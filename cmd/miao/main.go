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
	modeFlag := flag.String("mode", "ask", "非交互模式的权限：ask（需审批的一律拒绝）/ edits（自动同意编辑）/ plan（只读）")
	verbose := flag.Bool("v", false, "非交互模式下把工具调用过程打印到 stderr")
	modelFlag := flag.String("model", "", "覆盖 MIAO_MODEL")
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
	allowedTools := flag.String("allowed-tools", "", "非交互模式只启用这些工具，逗号分隔")
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

	cfg, cfgNotes, err := config.LoadWithNotes()
	notes = append(notes, cfgNotes...)
	if flag.NArg() > 0 && flag.Arg(0) == "doctor" {
		return runDoctor(cfg, notes, err)
	}
	if errors.Is(err, config.ErrMissingAPIKey) &&
		wantWizard(headlessMode, *plain || *listModels, isTerminal(os.Stdin), isTerminal(os.Stdout)) {
		if _, werr := tui.RunSetup(); werr != nil {
			return werr
		}
		cfg, cfgNotes, err = config.LoadWithNotes()
		notes = append(notes, cfgNotes...)
	}
	if err != nil {
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

	reg, err := tools.NewRegistry(cfg.Workspace)
	if err != nil {
		return err
	}
	tools.RegisterBuiltins(reg)
	mcpMgr := mcp.Start(reg)
	defer mcpMgr.Close()

	// 包一层可替换的客户端：/login 换 key 时主代理和子代理直接用上新的
	client := llm.NewSwappable(llm.NewClient(cfg.Protocol, cfg.APIKey, cfg.BaseURL, cfg.Model))
	llm.ApplySettings(client, cfg.Timeout, 3, *debug)

	if *listModels {
		return printModels(client)
	}

	md, truncated, err := agent.LoadAgentsMD(cfg.Workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 %s 失败：%v\n", agent.AgentsMDName, err)
	}
	if names := splitCSV(*allowedTools); len(names) > 0 {
		reg.RestrictTo(names)
	}

	ag := agent.New(client, agent.DefaultSystemPrompt, reg)
	ag.SetProjectContext(md, truncated, strings.TrimSpace(*systemFlag))
	ag.SetMaxToolRounds(cfg.MaxToolRounds)
	ag.SetAllowedCommands(agent.AllowedCommandsFromEnv())
	ag.SetFallbackModel(cfg.FallbackModel)
	ag.SetPrepareTools(func() {
		mcpMgr.WaitReady(1500 * time.Millisecond)
	})
	hookRunner, err := hooks.Load(cfg.Workspace, ag.HookApprover())
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 hook 配置失败（出错的部分已忽略）：%v\n", err)
	}
	ag.SetHooks(hookRunner)
	if _, err := subagent.Setup(reg, client, ag.Model); err != nil {
		fmt.Fprintf(os.Stderr, "读取子代理配置失败：%v\n", err)
	}

	if headlessMode || *plain || !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		for _, n := range notes {
			fmt.Fprintf(os.Stderr, "\x1b[90m%s\x1b[0m\n", n)
		}
	}

	if headlessMode {
		mode, err := headless.ParseMode(*modeFlag)
		if err != nil {
			return err
		}
		ag.SetMode(mode)
		format, err := headless.ParseFormat(*outputFormat)
		if err != nil {
			return err
		}
		if *maxTurns > 0 {
			ag.SetMaxToolRounds(*maxTurns)
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

	if *plain || !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return repl.New(ag, cfg.Model).Run(context.Background())
	}
	tui.SetStartupNotices(notes)
	tui.SetLoginHandler(func(c config.Credentials) (string, error) {
		model := cfg.Model
		if c.Model != "" {
			model = c.Model
		}
		next := llm.NewClient(c.Protocol, c.APIKey, c.BaseURL, model)
		llm.ApplySettings(next, cfg.Timeout, 3, *debug)
		client.Swap(next)
		return model, nil
	})
	if *resume {
		tui.SetResumeOnStart(true)
	}
	return tui.Run(ag, cfg.Model)
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
