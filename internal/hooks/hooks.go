// Package hooks 在工具调用前后、回合结束时执行用户配置的 shell 命令。
//
// 配置来源：
//   - 用户级：~/.miao/config.json 的 "hooks" 字段，直接生效；
//   - 项目级：<工作区>/.miao/hooks.json（没有 .miao 时读 .agnet），第一次运行前要用户确认，确认过的按文件哈希记住，
//     文件内容一变就重新确认（防止克隆陌生仓库后自动执行别人的命令）。
//
// 格式：{"pre_tool": [{"match": "edit_file|write_file", "command": "...", "timeout": 60}],
//
//	"post_tool": [...], "stop": [...]}
//
// match 是工具名正则（留空匹配全部，stop 忽略）。命令用 sh -c 在工作区里执行，
// 环境变量 MIAO_EVENT、MIAO_TOOL、MIAO_TOOL_ARGS 描述触发点（同时提供旧名 AGNET_* 以兼容），stdin 是同样内容的 JSON。
// 退出码非 0 视为失败：pre_tool 失败会阻止这次工具调用，post_tool 失败把输出附在工具结果后，
// stop 失败把输出作为新消息交给模型继续处理。
package hooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ainnhuomiao/miao/internal/appdir"
)

const (
	EventPreTool  = "pre_tool"
	EventPostTool = "post_tool"
	EventStop     = "stop"

	defaultTimeout = 60 * time.Second
	maxTimeout     = 10 * time.Minute
	maxOutput      = 16 << 10
	maxEnvArgs     = 32 << 10
)

// Hook 是一条配置。
type Hook struct {
	Match   string `json:"match,omitempty"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"` // 秒

	re     *regexp.Regexp
	source string // "用户" 或 "项目"
}

// Set 是一组按时机分类的 hook。
type Set struct {
	PreTool  []Hook `json:"pre_tool,omitempty"`
	PostTool []Hook `json:"post_tool,omitempty"`
	Stop     []Hook `json:"stop,omitempty"`
}

func (s Set) empty() bool { return len(s.PreTool)+len(s.PostTool)+len(s.Stop) == 0 }

func (s *Set) compile(source string) error {
	for _, list := range []*[]Hook{&s.PreTool, &s.PostTool, &s.Stop} {
		kept := (*list)[:0]
		for _, h := range *list {
			if strings.TrimSpace(h.Command) == "" {
				continue
			}
			if h.Match != "" {
				re, err := regexp.Compile("^(?:" + h.Match + ")$")
				if err != nil {
					return fmt.Errorf("hook match %q 不是合法正则: %w", h.Match, err)
				}
				h.re = re
			}
			h.source = source
			kept = append(kept, h)
		}
		*list = kept
	}
	return nil
}

// ApproveFunc 询问用户是否信任项目级 hook；display 是要展示的说明。
type ApproveFunc func(ctx context.Context, display string) (bool, error)

// Runner 持有已加载的 hook 并负责执行。并发安全。
type Runner struct {
	workspace   string
	user        Set
	project     Set
	projectPath string
	projectHash string
	approve     func() ApproveFunc

	mu      sync.Mutex
	decided bool // 本次运行已决定是否信任项目 hook
	trusted bool
}

// Result 是一次 hook 执行的汇总。
type Result struct {
	Failed bool
	Output string // 失败 hook 的输出（含命令说明）；成功时为空
	Ran    int
}

// Load 读取用户级和项目级配置。approve 在需要确认时被调用（可返回 nil 表示无法确认 → 跳过项目 hook）。
// 配置有错时返回错误，但仍返回可用的 Runner（出错的那一份被忽略）。
func Load(workspace string, approve func() ApproveFunc) (*Runner, error) {
	r := &Runner{workspace: workspace, approve: approve}
	var errs []error
	if p, err := userConfigPath(); err == nil {
		if data, err := os.ReadFile(p); err == nil {
			var cfg struct {
				Hooks Set `json:"hooks"`
			}
			if err := json.Unmarshal(data, &cfg); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", p, err))
			} else if err := cfg.Hooks.compile("用户"); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", p, err))
			} else {
				r.user = cfg.Hooks
			}
		}
	}
	if pd := appdir.ProjectDir(workspace); workspace != "" && pd != "" {
		p := filepath.Join(pd, "hooks.json")
		if data, err := os.ReadFile(p); err == nil {
			var s Set
			if err := json.Unmarshal(data, &s); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", p, err))
			} else if err := s.compile("项目"); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", p, err))
			} else if !s.empty() {
				sum := sha256.Sum256(data)
				r.project, r.projectPath, r.projectHash = s, p, hex.EncodeToString(sum[:])
			}
		}
	}
	return r, errors.Join(errs...)
}

// Empty 报告是否没有任何 hook。
func (r *Runner) Empty() bool { return r == nil || (r.user.empty() && r.project.empty()) }

// Summary 用于 /hooks 显示。
func (r *Runner) Summary() string {
	if r.Empty() {
		return "没有配置 hook。用户级写在 ~/.miao/config.json 的 \"hooks\" 里，项目级写在 .miao/hooks.json。"
	}
	var b strings.Builder
	b.WriteString("Hooks：")
	add := func(src string, s Set, note string) {
		for _, e := range []struct {
			name string
			list []Hook
		}{{"工具调用前", s.PreTool}, {"工具调用后", s.PostTool}, {"回合结束", s.Stop}} {
			for _, h := range e.list {
				m := h.Match
				if m == "" || e.name == "回合结束" {
					m = "全部"
				}
				fmt.Fprintf(&b, "\n  [%s%s] %s（%s）：%s", src, note, e.name, m, h.Command)
			}
		}
	}
	add("用户", r.user, "")
	note := ""
	if !r.project.empty() {
		r.mu.Lock()
		switch {
		case r.decided && r.trusted:
			note = "·已信任"
		case r.decided:
			note = "·未信任，已跳过"
		case isTrusted(r.projectPath, r.projectHash):
			note = "·已信任"
		default:
			note = "·首次运行前确认"
		}
		r.mu.Unlock()
	}
	add("项目", r.project, note)
	return b.String()
}

// PreTool 在工具执行前运行；Failed 为真时应阻止这次调用。
func (r *Runner) PreTool(ctx context.Context, tool, args string) Result {
	return r.run(ctx, EventPreTool, tool, args, "")
}

// PostTool 在工具执行后运行。
func (r *Runner) PostTool(ctx context.Context, tool, args, result string) Result {
	return r.run(ctx, EventPostTool, tool, args, result)
}

// Stop 在模型给出最终回复（本回合不再调用工具）时运行。
func (r *Runner) Stop(ctx context.Context) Result {
	return r.run(ctx, EventStop, "", "", "")
}

func (r *Runner) run(ctx context.Context, event, tool, args, result string) Result {
	if r.Empty() {
		return Result{}
	}
	pick := func(s Set) []Hook {
		var list []Hook
		switch event {
		case EventPreTool:
			list = s.PreTool
		case EventPostTool:
			list = s.PostTool
		default:
			list = s.Stop
		}
		var out []Hook
		for _, h := range list {
			if event == EventStop || h.re == nil || h.re.MatchString(tool) {
				out = append(out, h)
			}
		}
		return out
	}
	hs := pick(r.user)
	if proj := pick(r.project); len(proj) > 0 && r.projectAllowed(ctx) {
		hs = append(hs, proj...)
	}
	var res Result
	var outs []string
	for _, h := range hs {
		if ctx.Err() != nil {
			break
		}
		res.Ran++
		out, err := r.exec(ctx, h, event, tool, args, result)
		if err != nil {
			res.Failed = true
			msg := fmt.Sprintf("hook `%s` 失败（%v）", h.Command, err)
			if out != "" {
				msg += "：\n" + out
			}
			outs = append(outs, msg)
			if event == EventPreTool {
				break // 已经要阻止了，后面的不必再跑
			}
		}
	}
	res.Output = strings.Join(outs, "\n\n")
	return res
}

// projectAllowed 决定是否运行项目级 hook：已按哈希信任则直接运行，否则每次运行只问一次。
func (r *Runner) projectAllowed(ctx context.Context) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.decided {
		return r.trusted
	}
	if isTrusted(r.projectPath, r.projectHash) {
		r.decided, r.trusted = true, true
		return true
	}
	var approve ApproveFunc
	if r.approve != nil {
		approve = r.approve()
	}
	if approve == nil {
		return false // 暂时无法询问（例如还没接上界面），下次再问
	}
	ok, err := approve(ctx, r.trustPrompt())
	if err != nil {
		return false // 被取消：下次再问
	}
	r.decided, r.trusted = true, ok
	if ok {
		_ = saveTrust(r.projectPath, r.projectHash)
	}
	return ok
}

func (r *Runner) trustPrompt() string {
	var b strings.Builder
	b.WriteString("项目里的 hooks.json 想在你的电脑上自动执行以下命令：")
	for _, e := range []struct {
		name string
		list []Hook
	}{{"工具调用前", r.project.PreTool}, {"工具调用后", r.project.PostTool}, {"回合结束", r.project.Stop}} {
		for _, h := range e.list {
			fmt.Fprintf(&b, "\n  %s：%s", e.name, h.Command)
		}
	}
	b.WriteString("\n允许后会按文件内容记住，文件改动后会再次询问。")
	return b.String()
}

func (r *Runner) exec(ctx context.Context, h Hook, event, tool, args, result string) (string, error) {
	timeout := defaultTimeout
	if h.Timeout > 0 {
		timeout = min(time.Duration(h.Timeout)*time.Second, maxTimeout)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload, _ := json.Marshal(map[string]string{
		"event": event, "tool": tool, "args": args, "result": truncate(result, maxEnvArgs), "workspace": r.workspace,
	})
	cmd := exec.Command("sh", "-c", h.Command)
	cmd.Dir = r.workspace
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(),
		"MIAO_EVENT="+event,
		"MIAO_TOOL="+tool,
		"MIAO_TOOL_ARGS="+truncate(args, maxEnvArgs),
		"MIAO_WORKSPACE="+r.workspace,
		// 旧名保留，兼容改名前写的 hook 脚本
		"AGNET_EVENT="+event,
		"AGNET_TOOL="+tool,
		"AGNET_TOOL_ARGS="+truncate(args, maxEnvArgs),
		"AGNET_WORKSPACE="+r.workspace,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // 连同子进程一起结束
		<-done                                              //nolint:errcheck // 进程已被强杀，Wait 的错误无意义，以超时/取消原因为准
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("超时 %s", timeout)
		} else {
			err = ctx.Err()
		}
	}
	return strings.TrimSpace(truncate(out.String(), maxOutput)), err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…（已截断）"
}

// ---- 信任记录：~/.miao/trusted_hooks.json，{"<hooks.json 绝对路径>": "<sha256>"} ----

func userConfigPath() (string, error) {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func trustPath() (string, error) {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "trusted_hooks.json"), nil
}

var trustMu sync.Mutex

func readTrust() map[string]string {
	m := map[string]string{}
	if p, err := trustPath(); err == nil {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &m)
		}
	}
	return m
}

func isTrusted(path, hash string) bool {
	trustMu.Lock()
	defer trustMu.Unlock()
	if path == "" {
		return false
	}
	m := readTrust()
	if m[path] == hash {
		return true
	}
	// 改名兼容：项目从 .agnet/hooks.json 挪到 .miao/hooks.json 后，内容没变就仍算已信任
	if alt, ok := oldProjectPath(path); ok && m[alt] == hash {
		return true
	}
	return false
}

// oldProjectPath 把 <ws>/.miao/hooks.json 换成对应的旧路径 <ws>/.agnet/hooks.json。
func oldProjectPath(path string) (string, bool) {
	dir := filepath.Dir(path)
	if filepath.Base(dir) != appdir.ProjectDirName {
		return "", false
	}
	return filepath.Join(filepath.Dir(dir), appdir.OldProjectDirName, filepath.Base(path)), true
}

func saveTrust(path, hash string) error {
	trustMu.Lock()
	defer trustMu.Unlock()
	p, err := trustPath()
	if err != nil {
		return err
	}
	m := readTrust()
	m[path] = hash
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
