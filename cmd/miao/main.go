// Command miao 是 miao 的程序入口。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/appdir"
	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/headless"
	"github.com/ainnhuomiao/miao/internal/hooks"
	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/mcp"
	"github.com/ainnhuomiao/miao/internal/repl"
	"github.com/ainnhuomiao/miao/internal/subagent"
	"github.com/ainnhuomiao/miao/internal/tools"
	"github.com/ainnhuomiao/miao/internal/tui"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	plain := flag.Bool("plain", false, "使用简单的逐行 REPL，而不是全屏 TUI")
	prompt := flag.String("p", "", "非交互模式：发送这段提示词，输出回答后退出（\"-\" 表示只用管道输入）")
	modeFlag := flag.String("mode", "", "启动权限模式：ask（默认）/ edits（自动同意编辑）/ plan（只读）；非交互时拒绝需审批的操作")
	parallelTools := flag.Int("parallel-tools", 0, "同批只读工具最大并发数，1 表示串行（默认 8）")
	verbose := flag.Bool("v", false, "非交互模式下把工具调用过程打印到 stderr")
	modelFlag := flag.String("model", "", "覆盖 MIAO_MODEL")
	thinkingFlag := flag.String("thinking", "", "思考强度：auto / none / minimal / low / medium / high / xhigh / max（覆盖 MIAO_THINKING）")
	baseURLFlag := flag.String("base-url", "", "覆盖 MIAO_BASE_URL")
	systemFlag := flag.String("system", "", "追加到会话基线说明（不改系统提示词）")
	debug := flag.Bool("debug", false, "把请求耗时、状态码写到 stderr")
	listModels := flag.Bool("list-models", false, "列出服务端可用模型后退出")
	protocolFlag := flag.String("protocol", "", "openai / anthropic / auto")
	timeoutFlag := flag.Int("timeout", 0, "单次 LLM 请求超时秒数（默认 600）")
	resume := flag.Bool("c", false, "启动时恢复最近一次会话")
	flag.BoolVar(resume, "resume", false, "启动时恢复最近一次会话")
	showVersion := flag.Bool("version", false, "打印版本并退出")
	outputFormat := flag.String("output-format", "text", "非交互输出：text / json / stream-json")
	maxTurns := flag.Int("max-turns", 0, "非交互模式最多工具循环轮数（覆盖 MIAO_MAX_TOOL_ROUNDS）")
	allowedTools := flag.String("allowed-tools", "", "只启用这些工具，逗号分隔；空字符串禁用全部工具")
	disabledTools := flag.String("disabled-tools", "", "禁用这些工具，逗号分隔；优先于允许列表")
	retriesFlag := flag.Int("retries", -1, "请求失败后额外重试次数，0 表示不重试（默认 3）")
	maxOutputTokens := flag.Int("max-output-tokens", -1, "单次请求默认输出 token 上限，0 沿用协议默认值")
	mcpEnabled := flag.Bool("mcp", true, "启用 MCP 服务器（-mcp=false 关闭）")
	hooksEnabled := flag.Bool("hooks", true, "启用 hooks（-hooks=false 关闭）")
	autoCompact := flag.Int("auto-compact", -1, "TUI 自动压缩阈值百分比，0 关闭（默认 80）")
	compactKeepTurns := flag.Int("compact-keep-turns", 0, "压缩时保留原文的最近轮数（默认 2）")
	features := registerFeatureFlags()
	flag.Parse()
	headlessMode := isFlagSet("p")

	if *showVersion {
		fmt.Println("miao", version)
		return nil
	}

	if *debug {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	notes := appdir.Migrate()

	if flag.NArg() > 0 && flag.Arg(0) == "init" {
		for _, n := range notes {
			fmt.Fprintf(os.Stderr, "\x1b[90m%s\x1b[0m\n", n)
		}
		return runInit()
	}
	if flag.NArg() > 0 && flag.Arg(0) == "config" {
		if handled, err := runConfigAction(flag.Args()[1:], os.Stdout); handled {
			return err
		}
	}

	cfg, cfgNotes, err := config.LoadWithNotes()
	notes = append(notes, cfgNotes...)
	configCommand := flag.NArg() > 0 && flag.Arg(0) == "config"
	if configCommand && err != nil && !errors.Is(err, config.ErrMissingAPIKey) {
		return err
	}
	if flag.NArg() > 0 && flag.Arg(0) == "doctor" {
		return runDoctor(cfg, notes, err)
	}
	plainMode := cfg.Plain
	if isFlagSet("plain") {
		plainMode = *plain
	}
	if !configCommand && errors.Is(err, config.ErrMissingAPIKey) &&
		wantWizard(headlessMode, plainMode || *listModels, isTerminal(os.Stdin), isTerminal(os.Stdout)) {
		if _, werr := tui.RunSetup(); werr != nil {
			return werr
		}
		cfg, cfgNotes, err = config.LoadWithNotes()
		notes = append(notes, cfgNotes...)
	}
	if err != nil && !configCommand {
		return err
	}
	if *modelFlag != "" {
		cfg.Model = *modelFlag
	}
	if *baseURLFlag != "" {
		cfg.BaseURL = *baseURLFlag
	}
	if *protocolFlag != "" {
		cfg.Protocol = *protocolFlag
	}
	if *timeoutFlag > 0 {
		cfg.Timeout = time.Duration(*timeoutFlag) * time.Second
	}
	if isFlagSet("thinking") {
		cfg.Thinking = *thinkingFlag
	}
	if isFlagSet("system") {
		cfg.System = *systemFlag
	}
	if isFlagSet("plain") {
		cfg.Plain = *plain
	}
	if isFlagSet("retries") {
		if *retriesFlag < 0 {
			return fmt.Errorf("-retries 必须是非负整数")
		}
		cfg.MaxRetries = *retriesFlag
	}
	if isFlagSet("max-output-tokens") {
		if *maxOutputTokens < 0 {
			return fmt.Errorf("-max-output-tokens 必须是非负整数")
		}
		cfg.MaxOutputTokens = *maxOutputTokens
	}
	if isFlagSet("mcp") {
		cfg.MCPEnabled = *mcpEnabled
	}
	if isFlagSet("hooks") {
		cfg.HooksEnabled = *hooksEnabled
	}
	if isFlagSet("auto-compact") {
		if *autoCompact < 0 || *autoCompact > 100 {
			return fmt.Errorf("-auto-compact 必须在 0..100 之间")
		}
		cfg.AutoCompactPercent = *autoCompact
	}
	if isFlagSet("compact-keep-turns") {
		if *compactKeepTurns < 1 {
			return fmt.Errorf("-compact-keep-turns 必须大于 0")
		}
		cfg.CompactKeepTurns = *compactKeepTurns
	}
	if isFlagSet("allowed-tools") {
		cfg.AllowedTools = splitCSV(*allowedTools)
		if cfg.AllowedTools == nil {
			cfg.AllowedTools = []string{}
		}
	}
	if isFlagSet("disabled-tools") {
		cfg.DisabledTools = splitCSV(*disabledTools)
	}
	features.apply(&cfg)
	if isFlagSet("mode") {
		cfg.Mode, err = config.NormalizeMode(*modeFlag)
		if err != nil {
			return err
		}
	}
	if isFlagSet("parallel-tools") {
		if *parallelTools < 1 {
			return fmt.Errorf("-parallel-tools 必须大于 0")
		}
		cfg.ParallelTools = *parallelTools
	}
	if headlessMode && *maxTurns > 0 {
		cfg.MaxToolRounds = *maxTurns
	}
	if configCommand {
		for _, n := range notes {
			fmt.Fprintln(os.Stderr, n)
		}
		return printConfig(os.Stdout, cfg)
	}

	// 先校验模型配置，再启动可能执行外部程序的 MCP 和 hooks。
	client := llm.NewSwappable(llm.NewClient(cfg.Protocol, cfg.APIKey, cfg.BaseURL, cfg.Model))
	llm.ApplySettings(client, cfg.Timeout, cfg.MaxRetries, *debug)
	client.SetMaxOutputTokens(cfg.MaxOutputTokens)
	if err := client.SetThinking(cfg.Thinking); err != nil {
		return err
	}
	if *listModels {
		return printModels(client)
	}

	reg, err := tools.NewRegistry(cfg.Workspace)
	if err != nil {
		return err
	}
	tools.RegisterBuiltins(reg)
	reg.RestrictTo(cfg.AllowedTools)
	reg.SetDisabledTools(cfg.DisabledTools)
	reg.SetDiagnosticsEnabled(cfg.DiagnosticsEnabled)
	reg.SetSnapshotsEnabled(cfg.SnapshotsEnabled)
	var mcpMgr *mcp.Manager
	if cfg.MCPEnabled {
		mcpMgr = mcp.Start(reg)
		defer mcpMgr.Close()
	}

	md, truncated, err := loadProjectMemory(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 %s 失败：%v\n", agent.AgentsMDName, err)
	}
	ag := agent.New(client, agent.DefaultSystemPrompt, reg)
	ag.SetImagesEnabled(cfg.ImagesEnabled)
	ag.SetProjectContext(md, truncated, strings.TrimSpace(cfg.System))
	ag.SetMaxToolRounds(cfg.MaxToolRounds)
	ag.SetParallelTools(cfg.ParallelTools)
	mode, err := headless.ParseMode(cfg.Mode)
	if err != nil {
		return err
	}
	ag.SetMode(mode)
	ag.SetAllowedCommands(agent.AllowedCommandsFromEnv())
	ag.SetFallbackModel(cfg.FallbackModel)
	ag.SetPrepareTools(func() {
		mcpMgr.WaitReady(1500 * time.Millisecond)
	})
	if cfg.HooksEnabled {
		hookRunner, err := hooks.Load(cfg.Workspace, ag.HookApprover())
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取 hook 配置失败（出错的部分已忽略）：%v\n", err)
		}
		ag.SetHooks(hookRunner)
	}
	if _, err := subagent.Setup(reg, client, ag.Model); err != nil {
		fmt.Fprintf(os.Stderr, "读取子代理配置失败：%v\n", err)
	}

	if headlessMode || cfg.Plain || !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		for _, n := range notes {
			fmt.Fprintf(os.Stderr, "\x1b[90m%s\x1b[0m\n", n)
		}
	}

	if headlessMode {
		format, err := headless.ParseFormat(*outputFormat)
		if err != nil {
			return err
		}
		var stdin io.Reader
		if !isTerminal(os.Stdin) {
			stdin = os.Stdin
		}
		text, err := headless.BuildPrompt(*prompt, stdin)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		defer ag.KillJobs()
		return headless.Run(ctx, ag, text, os.Stdout, os.Stderr, headless.Options{Verbose: *verbose, Format: format})
	}

	if cfg.Plain || !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return repl.New(ag, cfg.Model).Run(context.Background())
	}
	tui.SetStartupNotices(notes)
	tui.SetLoginHandler(func(c config.Credentials) (string, error) {
		model := cfg.Model
		if c.Model != "" {
			model = c.Model
		}
		next := llm.NewClient(c.Protocol, c.APIKey, c.BaseURL, model)
		llm.ApplySettings(next, cfg.Timeout, cfg.MaxRetries, *debug)
		client.Swap(next)
		return model, nil
	})
	if *resume {
		tui.SetResumeOnStart(true)
	}
	return tui.Run(ag, cfg.Model, tui.Options{
		AutoCompactPercent: cfg.AutoCompactPercent, CompactKeepTurns: cfg.CompactKeepTurns,
		DisableSessionStorage: !cfg.SessionsEnabled, DisableProjectMemory: !cfg.ProjectMemoryEnabled,
		DisableCustomCommands: !cfg.CustomCommandsEnabled, DisableShell: !cfg.ShellEnabled,
		DisableBell:      !cfg.BellEnabled,
		DisableSnapshots: !cfg.SnapshotsEnabled,
	})
}

func loadProjectMemory(cfg config.Config) (string, bool, error) {
	if !cfg.ProjectMemoryEnabled {
		return "", false, nil
	}
	return agent.LoadAgentsMD(cfg.Workspace)
}

// wantWizard 判断缺 key 时是否打开首次启动向导：只在交互 TUI 里开；
// -p 非交互、-plain、-list-models、没有 TTY 时直接报错（错误里提示 miao init 和 MIAO_API_KEY）。
func wantWizard(headless, plainOrList, ttyIn, ttyOut bool) bool {
	return !headless && !plainOrList && ttyIn && ttyOut
}

func printModels(client llm.Client) error {
	lister, ok := client.(llm.ModelLister)
	if !ok {
		return fmt.Errorf("当前协议不支持列出模型")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	names, err := lister.ListModels(ctx)
	if err != nil {
		return err
	}
	for _, n := range names {
		fmt.Println(n)
	}
	return nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isFlagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
