package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"miao/internal/llm"
	"miao/internal/tools"
)

// 默认超时。
var (
	ConnectTimeout = 10 * time.Second // 单个服务器启动 + 握手 + tools/list
	CallTimeout    = 60 * time.Second // 单次 tools/call
)

// maxResultBytes 是工具结果的最大长度，超出截断并附说明。
const maxResultBytes = 100 << 10

// 服务器状态。
const (
	StateConnecting = "连接中"
	StateConnected  = "已连接"
	StateFailed     = "失败"
	StateDisabled   = "已禁用"
)

// Status 是一个服务器的状态快照（/mcp 用）。
type Status struct {
	Name  string
	State string
	Tools []string // 注册到 miao 的工具名
	Err   string
}

type server struct {
	cfg    ServerConfig
	client *Client
	status Status
}

// Manager 管理全部 MCP 服务器。
type Manager struct {
	mu      sync.Mutex
	servers []*server
	taken   map[string]bool
	notices chan string
	closed  bool
	wg      sync.WaitGroup
}

var (
	defMu  sync.Mutex
	defMgr *Manager
)

// Default 返回 Start 创建的管理器；尚未启动时返回 nil。
func Default() *Manager {
	defMu.Lock()
	defer defMu.Unlock()
	return defMgr
}

// Start 读取默认配置，后台并行连接全部服务器，连上后把工具注册进 reg。
// 不阻塞；失败只产生提示，不影响 miao 运行。
func Start(reg *tools.Registry) *Manager {
	m := StartWithConfig(reg, ConfigPath())
	defMu.Lock()
	defMgr = m
	defMu.Unlock()
	return m
}

// StartWithConfig 同 Start，但使用指定配置文件路径。
func StartWithConfig(reg *tools.Registry, path string) *Manager {
	m := &Manager{taken: map[string]bool{}, notices: make(chan string, 64)}
	for _, d := range reg.Definitions() {
		m.taken[d.Function.Name] = true
	}
	cfgs, err := LoadConfig(path)
	if err != nil {
		m.notify("MCP 配置读取失败：" + err.Error())
		return m
	}
	for _, c := range cfgs {
		s := &server{cfg: c, status: Status{Name: c.Name, State: StateConnecting}}
		m.servers = append(m.servers, s)
		switch {
		case c.Disabled:
			s.status.State = StateDisabled
		case c.Command == "" && c.URL != "":
			s.status.State = StateFailed
			s.status.Err = "暂不支持 url（streamable HTTP），请使用 stdio command"
			m.notify(fmt.Sprintf("MCP 服务器 %s 启动失败：%s", c.Name, s.status.Err))
		default:
			m.wg.Add(1)
			go func() {
				defer m.wg.Done()
				m.connect(reg, s)
			}()
		}
	}
	return m
}

// Wait 等待所有连接尝试结束（测试用）。
func (m *Manager) Wait() { m.wg.Wait() }

// Notices 返回提示消息通道（缓冲，满了丢弃）。
func (m *Manager) Notices() <-chan string { return m.notices }

func (m *Manager) notify(s string) {
	select {
	case m.notices <- s:
	default:
	}
}

func (m *Manager) fail(s *server, err error) {
	m.mu.Lock()
	s.status.State = StateFailed
	s.status.Err = err.Error()
	m.mu.Unlock()
	m.notify(fmt.Sprintf("MCP 服务器 %s 启动失败：%v", s.cfg.Name, err))
}

func (m *Manager) connect(reg *tools.Registry, s *server) {
	c, err := Spawn(s.cfg)
	if err != nil {
		m.fail(s, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), ConnectTimeout)
	defer cancel()
	var list []Tool
	err = c.Initialize(ctx)
	if err == nil {
		list, err = c.ListTools(ctx)
	}
	if err != nil {
		c.Close()
		if ctx.Err() != nil {
			err = fmt.Errorf("连接超时（%s）", ConnectTimeout)
		}
		m.fail(s, err)
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		c.Close()
		return
	}
	s.client = c
	var names []string
	var defs []tools.Tool
	for _, t := range list {
		name := toolName(s.cfg.Name, t.Name, m.taken)
		names = append(names, name)
		defs = append(defs, m.makeTool(reg, s.cfg.Name, name, t, c))
	}
	s.status.State = StateConnected
	s.status.Tools = names
	m.mu.Unlock()
	for _, d := range defs {
		reg.Register(d)
	}
	m.notify(fmt.Sprintf("已连接 MCP：%s（%d 个工具）", s.cfg.Name, len(names)))
}

func (m *Manager) makeTool(reg *tools.Registry, serverName, name string, t Tool, c *Client) tools.Tool {
	schema := t.InputSchema
	if len(bytes.TrimSpace(schema)) == 0 || string(bytes.TrimSpace(schema)) == "null" {
		schema = json.RawMessage(`{"type":"object"}`)
	}
	desc := strings.TrimSpace(t.Description)
	desc = fmt.Sprintf("[MCP %s] %s", serverName, desc)
	origName := t.Name
	return tools.Tool{
		Def: llm.ToolDef{Type: "function", Function: llm.FunctionDef{
			Name: name, Description: desc, Parameters: schema,
		}},
		Run: func(ctx context.Context, args json.RawMessage) tools.Result {
			approver := reg.Approver()
			if approver == nil {
				return tools.Result{Content: "MCP 工具需要用户确认，但未配置审批", IsError: true}
			}
			display := fmt.Sprintf("MCP %s · %s\n%s", serverName, origName, prettyJSON(args))
			allow, err := approver(ctx, name, display, args)
			if err != nil {
				return tools.Result{Content: "审批中断: " + err.Error(), IsError: true}
			}
			if !allow {
				return tools.Result{Content: "用户拒绝执行 MCP 工具: " + name, IsError: true}
			}
			cctx, cancel := context.WithTimeout(ctx, CallTimeout)
			defer cancel()
			raw, err := c.CallTool(cctx, origName, args)
			if err != nil {
				if cctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
					return tools.Result{Content: fmt.Sprintf("MCP 工具 %s 超时（%s）", name, CallTimeout), IsError: true}
				}
				return tools.Result{Content: "MCP 调用失败: " + err.Error(), IsError: true}
			}
			return FormatResult(raw)
		},
	}
}

func prettyJSON(raw json.RawMessage) string {
	var b bytes.Buffer
	if json.Indent(&b, raw, "", "  ") != nil {
		return string(raw)
	}
	return b.String()
}

// FormatResult 把 tools/call 结果转成文本：拼接 text，其他类型给摘要，isError 视为工具错误。
func FormatResult(raw json.RawMessage) tools.Result {
	var res struct {
		Content []struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			MimeType string          `json:"mimeType"`
			URI      string          `json:"uri"`
			Name     string          `json:"name"`
			Resource json.RawMessage `json:"resource"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return tools.Result{Content: "解析 MCP 结果失败: " + err.Error(), IsError: true}
	}
	var parts []string
	for _, c := range res.Content {
		switch c.Type {
		case "text":
			parts = append(parts, c.Text)
		case "image", "audio":
			parts = append(parts, fmt.Sprintf("[%s: %s]", c.Type, c.MimeType))
		case "resource_link":
			parts = append(parts, fmt.Sprintf("[resource_link: %s %s]", c.Name, c.URI))
		case "resource":
			var r struct {
				URI      string `json:"uri"`
				MimeType string `json:"mimeType"`
				Text     string `json:"text"`
			}
			_ = json.Unmarshal(c.Resource, &r)
			if r.Text != "" {
				parts = append(parts, r.Text)
			} else {
				parts = append(parts, fmt.Sprintf("[resource: %s %s]", r.URI, r.MimeType))
			}
		default:
			parts = append(parts, fmt.Sprintf("[%s]", c.Type))
		}
	}
	if len(parts) == 0 && len(res.StructuredContent) > 0 {
		parts = append(parts, string(res.StructuredContent))
	}
	out := strings.Join(parts, "\n")
	if len(out) > maxResultBytes {
		cut := maxResultBytes
		for cut > 0 && out[cut]&0xC0 == 0x80 { // 不切断 UTF-8 字符
			cut--
		}
		out = out[:cut] + fmt.Sprintf("\n…[结果过长，已截断：共 %d 字节，仅保留前 %d 字节]", len(out), cut)
	}
	if out == "" {
		out = "（无输出）"
	}
	return tools.Result{Content: out, IsError: res.IsError}
}

// Statuses 返回全部服务器状态快照。
func (m *Manager) Statuses() []Status {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.servers))
	for _, s := range m.servers {
		st := s.status
		st.Tools = append([]string(nil), st.Tools...)
		out = append(out, st)
	}
	return out
}

// Summary 是 /mcp 命令显示的文本。
func (m *Manager) Summary() string {
	sts := m.Statuses()
	if len(sts) == 0 {
		return "未配置 MCP 服务器。配置文件：" + ConfigPath()
	}
	var b strings.Builder
	b.WriteString("MCP 服务器（" + ConfigPath() + "）：")
	for _, s := range sts {
		fmt.Fprintf(&b, "\n  %s  %s", s.Name, s.State)
		if s.State == StateConnected {
			fmt.Fprintf(&b, "（%d 个工具）", len(s.Tools))
			if len(s.Tools) > 0 {
				b.WriteString("\n    " + strings.Join(s.Tools, ", "))
			}
		}
		if s.Err != "" {
			b.WriteString("：" + s.Err)
		}
	}
	return b.String()
}

// Close 关闭全部服务器（退出时调用）。可重复调用。
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	var cs []*Client
	for _, s := range m.servers {
		if s.client != nil {
			cs = append(cs, s.client)
			s.client = nil
		}
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, c := range cs {
		wg.Add(1)
		go func() { defer wg.Done(); c.Close() }()
	}
	wg.Wait()
}
