package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
)

// ToolName 是暴露给主代理的工具名。
const ToolName = "task"

// 单个子代理的默认限制。
const (
	DefaultTimeout = 5 * time.Minute
	maxAnswerBytes = 32 << 10
)

// 进度行：排队中的子代理不计入运行数。
const (
	ActivityQueued   = "排队中"
	ActivityStarting = "启动中"
)

// Manager 持有子代理配置，并按配置在主注册表里注册或移除 task 工具。
type Manager struct {
	reg       *tools.Registry
	sub       *tools.Registry // 本地只读工具
	subWeb    *tools.Registry // 本地只读工具 + web_fetch（allow_web 时）
	client    llm.Client
	mainModel func() string

	Timeout time.Duration
	// Save 持久化配置；默认 SaveConfig，测试可替换。
	Save func(Config) error

	mu      sync.Mutex
	cfg     Config
	running int
	wake    chan struct{}
}

// New 创建 Manager：子代理用独立的只读注册表（无审批回调，不含写工具/命令/MCP/task）。
func New(reg *tools.Registry, client llm.Client, mainModel func() string, cfg Config) *Manager {
	full, err := tools.NewRegistry(reg.Workspace())
	if err != nil {
		full = reg
	}
	if full != reg {
		tools.RegisterBuiltins(full)
	}
	// 子代理保留独立的审批与路径约束闭包，同时继承用户对工具的开关。
	allowed := make([]string, 0, len(AllowedTools))
	for _, name := range AllowedTools {
		if reg.ToolEnabled(name) {
			allowed = append(allowed, name)
		}
	}
	withWeb := append([]string{}, allowed...)
	if reg.ToolEnabled(WebTool) {
		withWeb = append(withWeb, WebTool)
	}
	m := &Manager{
		reg:       reg,
		sub:       full.Subset(allowed...),
		subWeb:    full.Subset(withWeb...),
		client:    client,
		mainModel: mainModel,
		Timeout:   DefaultTimeout,
		Save:      SaveConfig,
		cfg:       cfg.Normalize(),
		wake:      make(chan struct{}),
	}
	m.apply()
	return m
}

// SubTools 返回按当前配置子代理可用的工具注册表。
func (m *Manager) SubTools() *tools.Registry {
	if m.Config().AllowWeb {
		return m.subWeb
	}
	return m.sub
}

// Config 返回当前配置。
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// SetConfig 立即生效并保存；保存失败时配置仍生效并返回错误。
func (m *Manager) SetConfig(c Config) error {
	c = c.Normalize()
	if err := c.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	m.cfg = c
	m.broadcastLocked()
	m.mu.Unlock()
	m.apply()
	if m.Save != nil {
		return m.Save(c)
	}
	return nil
}

// Running 返回正在运行（已拿到并发名额）的子代理数。
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *Manager) apply() {
	// 始终注册，避免开关改变工具列表导致整段 prompt cache 失效。
	m.reg.Register(m.tool())
}

func (m *Manager) broadcastLocked() {
	close(m.wake)
	m.wake = make(chan struct{})
}

func (m *Manager) acquire(ctx context.Context) error {
	for {
		m.mu.Lock()
		if m.running < m.cfg.Max {
			m.running++
			m.mu.Unlock()
			return nil
		}
		ch := m.wake
		m.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Manager) release() {
	m.mu.Lock()
	m.running--
	m.broadcastLocked()
	m.mu.Unlock()
}

type usageKey struct{}

// WithUsageSink 让 task 工具把子代理用量报告给主会话（主代理在执行工具时注入）。
func WithUsageSink(ctx context.Context, fn func(llm.Usage)) context.Context {
	return context.WithValue(ctx, usageKey{}, fn)
}

func usageSink(ctx context.Context) func(llm.Usage) {
	fn, _ := ctx.Value(usageKey{}).(func(llm.Usage))
	return fn
}

func (m *Manager) tool() tools.Tool {
	params, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"description": map[string]any{"type": "string", "description": "任务的简短标题（3~8 个字）"},
			"prompt":      map[string]any{"type": "string", "description": "交给子代理的完整说明：要查什么、关注哪些文件、需要返回什么"},
		},
		"required": []string{"description", "prompt"},
	})
	return tools.Tool{
		Def: llm.ToolDef{Type: "function", Function: llm.FunctionDef{
			Name:        ToolName,
			Description: "派出只读子代理，在全新上下文里做调研并返回结论。适合大范围搜索、阅读；子代理不能改文件或执行命令。不要用它做写入、修复或实现。关闭时调用会报错。",
			Parameters:  params,
		}},
		Run: m.run,
	}
}

func (m *Manager) run(ctx context.Context, args json.RawMessage) tools.Result {
	if !m.Config().Enabled {
		return tools.Result{Content: "子代理已关闭", IsError: true}
	}
	var in struct {
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tools.Result{Content: "参数解析失败: " + err.Error(), IsError: true}
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return tools.Result{Content: "prompt 不能为空", IsError: true}
	}
	progress := func(s string) { tools.ReportProgress(ctx, s) }
	progress(ActivityQueued)
	if err := m.acquire(ctx); err != nil {
		return tools.Result{Content: "子代理已取消（排队中）", IsError: true}
	}
	defer m.release()
	progress(ActivityStarting)

	cfg := m.Config()
	model := cfg.Model
	if model == "" && m.mainModel != nil {
		model = m.mainModel()
	}
	timeout := m.Timeout
	if cfg.TimeoutSec > 0 {
		timeout = time.Duration(cfg.TimeoutSec) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r := &Runner{
		Client:        m.client,
		Model:         model,
		Thinking:      cfg.Thinking,
		Tools:         m.sub,
		MaxToolRounds: cfg.MaxToolRounds,
		System:        subSystemPrompt(cfg.AllowWeb),
		OnActivity:    progress,
		OnUsage:       usageSink(ctx),
	}
	if cfg.AllowWeb {
		r.Tools = m.subWeb
	}
	out, err := r.Run(runCtx, "任务："+in.Description+"\n\n"+in.Prompt)
	footer := fmt.Sprintf("\n\n—— 子代理 · %d 轮工具 · 用量 ↑%d ↓%d tokens", out.Steps, out.Usage.PromptTokens, out.Usage.CompletionTokens)
	if err != nil {
		msg := "子代理失败: " + err.Error()
		switch {
		case ctx.Err() != nil:
			msg = "子代理已取消"
		case errors.Is(err, context.DeadlineExceeded):
			msg = fmt.Sprintf("子代理超时（%s）", timeout)
		}
		return tools.Result{Content: msg + footer, IsError: true}
	}
	ans := strings.TrimSpace(out.Answer)
	if ans == "" {
		ans = "(子代理没有返回内容)"
	}
	if len(ans) > maxAnswerBytes {
		cut := maxAnswerBytes
		for cut > 0 && (ans[cut]&0xC0) == 0x80 {
			cut--
		}
		ans = ans[:cut] + "\n…(已截断，超过 32KiB)"
	}
	return tools.Result{Content: ans + footer}
}

var current atomic.Pointer[Manager]

// SetCurrent 设置全局 Manager（TUI 的 /subagents 菜单使用）。
func SetCurrent(m *Manager) { current.Store(m) }

// Current 返回全局 Manager，未设置时为 nil。
func Current() *Manager { return current.Load() }

// Setup 读取配置并创建、注册全局 Manager。
func Setup(reg *tools.Registry, client llm.Client, mainModel func() string) (*Manager, error) {
	cfg, err := LoadConfig()
	m := New(reg, client, mainModel, cfg)
	SetCurrent(m)
	return m, err
}

func subSystemPrompt(allowWeb bool) string {
	if allowWeb {
		return SystemPrompt + webPromptNote
	}
	return SystemPrompt
}
