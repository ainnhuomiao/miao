package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"agnet/internal/agent"
)

type role int

const (
	roleUser role = iota
	roleAssistant
	roleTool
	roleNotice
	roleError
)

// chatMessage 是界面上显示的一条消息。
type chatMessage struct {
	role      role
	content   string
	reasoning string
	done      bool

	// 工具卡字段（roleTool）
	toolName   string
	toolArgs   string
	toolResult string
	toolError  bool
	toolDur    time.Duration
	toolCallID string
}

// streamEventMsg 是后台流式 goroutine 发给 Update 的事件。
type streamEventMsg struct {
	kind       agent.EventKind
	content    string
	reasoning  string
	toolCallID string
	toolName   string
	toolArgs   string
	toolResult string
	toolError  bool
	toolDur    time.Duration
	done       bool
	err        error
}

// approvalRequestMsg 由工具 goroutine 经 Program.Send 投递，请求用户确认。
type approvalRequestMsg struct {
	toolName string
	display  string
	resp     chan bool
}

type approvalClearMsg struct{}

type approvalState struct {
	toolName string
	display  string
	resp     chan bool
}

// Model 是 TUI 的全部状态。
type Model struct {
	ag        *agent.Agent
	modelName string

	width  int
	height int
	ready  bool

	viewport       viewport.Model
	drawerViewport viewport.Model
	input          textarea.Model
	spinner        spinner.Model

	messages []chatMessage

	streaming bool
	cancel    context.CancelFunc
	events    chan streamEventMsg

	showReasoning bool
	drawerOpen    bool
	drawerFocus   bool // 焦点在抽屉时滚轮/Pg 作用于抽屉
	// 左侧会话侧栏：一期接口预留，按 [ ] 暂为 noop
	sidebarReserved bool

	approval *approvalState
}

// New 创建 TUI 模型。
func New(ag *agent.Agent, modelName string) *Model {
	ta := textarea.New()
	ta.Placeholder = "输入消息，Enter 发送…"
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(inputLines)
	ta.SetWidth(60)
	ta.KeyMap.InsertNewline.SetKeys("ctrl+j")

	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = promptStyle
	ta.FocusedStyle.Placeholder = mutedStyle
	ta.Cursor.Style = lipgloss.NewStyle().Foreground(accentColor)
	_ = ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accentColor)

	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	dvp := viewport.New(0, 0)
	dvp.MouseWheelEnabled = true

	return &Model{
		ag:             ag,
		modelName:      modelName,
		input:          ta,
		spinner:        sp,
		viewport:       vp,
		drawerViewport: dvp,
		messages: []chatMessage{{
			role:    roleNotice,
			content: "已就绪。输入消息开始对话，输入 /help 查看命令。Tab 开/关工具抽屉。",
			done:    true,
		}},
	}
}

func (m *Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.layout()
		m.refreshViewport(true)
		m.refreshDrawer()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		if m.drawerOpen && m.drawerFocus {
			var cmd tea.Cmd
			m.drawerViewport, cmd = m.drawerViewport.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case spinner.TickMsg:
		if !m.streaming {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case streamEventMsg:
		return m.handleStreamEvent(msg)

	case approvalRequestMsg:
		m.approval = &approvalState{
			toolName: msg.toolName,
			display:  msg.display,
			resp:     msg.resp,
		}
		m.layout()
		return m, nil

	case approvalClearMsg:
		m.clearApproval(false)
		m.layout()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	return m.view()
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 审批叠层优先吃键。
	if m.approval != nil {
		switch msg.String() {
		case "y", "Y":
			m.resolveApproval(true)
			return m, nil
		case "n", "N", "esc":
			m.resolveApproval(false)
			return m, nil
		case "ctrl+c":
			m.resolveApproval(false)
			if m.streaming {
				m.cancelStream()
				return m, nil
			}
			return m, tea.Quit
		default:
			return m, nil // 审批中忽略其它输入
		}
	}

	switch msg.String() {
	case "ctrl+c":
		if m.streaming {
			m.cancelStream()
			return m, nil
		}
		return m, tea.Quit

	case "esc":
		// 定稿：关抽屉 → 取消审批（上面已处理）→ 取消生成
		if m.drawerOpen {
			m.drawerOpen = false
			m.drawerFocus = false
			m.layout()
			m.refreshViewport(false)
			return m, nil
		}
		if m.streaming {
			m.cancelStream()
		}
		return m, nil

	case "tab":
		m.drawerOpen = !m.drawerOpen
		m.drawerFocus = m.drawerOpen
		m.layout()
		m.refreshViewport(false)
		m.refreshDrawer()
		return m, nil

	case "[", "]":
		// 一期：左侧会话侧栏接口预留，暂 noop
		_ = m.sidebarReserved
		return m, nil

	case "ctrl+r":
		m.showReasoning = !m.showReasoning
		m.refreshViewport(false)
		return m, nil

	case "pgup":
		if m.drawerOpen && m.drawerFocus {
			m.drawerViewport.PageUp()
		} else {
			m.viewport.PageUp()
		}
		return m, nil

	case "pgdown":
		if m.drawerOpen && m.drawerFocus {
			m.drawerViewport.PageDown()
		} else {
			m.viewport.PageDown()
		}
		return m, nil

	case "enter":
		if m.streaming {
			return m, nil
		}
		return m, m.submit()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	m.input.Reset()

	if strings.HasPrefix(text, "/") {
		return m.runCommand(text)
	}

	events := make(chan streamEventMsg, 128)
	ctx, cancel := context.WithCancel(context.Background())

	m.streaming = true
	m.cancel = cancel
	m.events = events
	m.messages = append(m.messages,
		chatMessage{role: roleUser, content: text, done: true},
		chatMessage{role: roleAssistant},
	)
	m.refreshViewport(true)

	ag := m.ag
	go func() {
		defer close(events)
		err := ag.Send(ctx, text, func(e agent.Event) error {
			msg := streamEventMsg{
				kind:       e.Kind,
				content:    e.Delta.Content,
				reasoning:  e.Delta.Reasoning,
				toolCallID: e.ToolCallID,
				toolName:   e.ToolName,
				toolArgs:   e.ToolArgs,
				toolResult: e.ToolResult,
				toolError:  e.ToolError,
				toolDur:    e.Duration,
			}
			select {
			case events <- msg:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		events <- streamEventMsg{done: true, err: err}
	}()

	return tea.Batch(waitForEvent(events), m.spinner.Tick)
}

func waitForEvent(ch <-chan streamEventMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *Model) handleStreamEvent(msg streamEventMsg) (tea.Model, tea.Cmd) {
	if msg.done {
		m.streaming = false
		m.cancel = nil
		m.events = nil

		last := m.lastAssistant()
		if last != nil {
			last.done = true
		}

		switch {
		case errors.Is(msg.err, context.Canceled):
			m.messages = append(m.messages, chatMessage{
				role: roleNotice, content: "已取消生成（本轮未加入上下文）。", done: true,
			})
		case msg.err != nil:
			m.messages = append(m.messages, chatMessage{
				role: roleError, content: "请求失败: " + msg.err.Error(), done: true,
			})
		case last != nil && last.content == "" && !m.hasToolAfterAssistant(last):
			m.messages = append(m.messages, chatMessage{
				role: roleNotice, content: "模型没有返回文本内容。", done: true,
			})
		}

		m.refreshViewport(false)
		m.refreshDrawer()
		return m, nil
	}

	switch msg.kind {
	case agent.EventDelta:
		last := m.lastAssistant()
		if last == nil || last.done {
			m.messages = append(m.messages, chatMessage{role: roleAssistant})
			last = &m.messages[len(m.messages)-1]
		}
		last.content += msg.content
		last.reasoning += msg.reasoning
	case agent.EventToolStart:
		// 当前助手气泡先收尾（可能尚无文本），再插入工具卡
		if last := m.lastAssistant(); last != nil && !last.done {
			last.done = true
		}
		m.messages = append(m.messages, chatMessage{
			role:       roleTool,
			toolCallID: msg.toolCallID,
			toolName:   msg.toolName,
			toolArgs:   msg.toolArgs,
			done:       false,
		})
	case agent.EventToolEnd:
		updated := false
		for i := len(m.messages) - 1; i >= 0; i-- {
			cm := &m.messages[i]
			if cm.role == roleTool && !cm.done &&
				(cm.toolCallID == msg.toolCallID || (msg.toolCallID == "" && cm.toolName == msg.toolName)) {
				cm.toolResult = msg.toolResult
				cm.toolError = msg.toolError
				cm.toolDur = msg.toolDur
				cm.done = true
				updated = true
				break
			}
		}
		if !updated {
			m.messages = append(m.messages, chatMessage{
				role:       roleTool,
				toolCallID: msg.toolCallID,
				toolName:   msg.toolName,
				toolArgs:   msg.toolArgs,
				toolResult: msg.toolResult,
				toolError:  msg.toolError,
				toolDur:    msg.toolDur,
				done:       true,
			})
		}
		// 工具之后可能还有最终助手回复：预留新气泡
		m.messages = append(m.messages, chatMessage{role: roleAssistant})
	}

	m.refreshViewport(false)
	m.refreshDrawer()
	return m, waitForEvent(m.events)
}

func (m *Model) lastAssistant() *chatMessage {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == roleAssistant {
			return &m.messages[i]
		}
	}
	return nil
}

func (m *Model) hasToolAfterAssistant(asst *chatMessage) bool {
	foundAsst := false
	for i := range m.messages {
		if &m.messages[i] == asst {
			foundAsst = true
			continue
		}
		if foundAsst && m.messages[i].role == roleTool {
			return true
		}
	}
	return false
}

func (m *Model) runCommand(text string) tea.Cmd {
	fields := strings.Fields(text)

	switch fields[0] {
	case "/exit", "/quit":
		return tea.Quit

	case "/clear":
		m.ag.Reset()
		m.messages = []chatMessage{{
			role: roleNotice, content: "对话已清空，开始新会话。", done: true,
		}}
		m.refreshViewport(true)
		m.refreshDrawer()

	case "/help":
		m.messages = append(m.messages, chatMessage{
			role: roleNotice, content: helpText, done: true,
		})
		m.refreshViewport(true)

	case "/reasoning":
		m.showReasoning = !m.showReasoning
		state := "已收起思考过程。"
		if m.showReasoning {
			state = "已展开思考过程。"
		}
		m.messages = append(m.messages, chatMessage{
			role: roleNotice, content: state, done: true,
		})
		m.refreshViewport(false)

	default:
		m.messages = append(m.messages, chatMessage{
			role:    roleNotice,
			content: "未知命令 " + fields[0] + "，输入 /help 查看帮助。",
			done:    true,
		})
		m.refreshViewport(false)
	}
	return nil
}

func (m *Model) resolveApproval(allow bool) {
	if m.approval == nil {
		return
	}
	select {
	case m.approval.resp <- allow:
	default:
	}
	m.approval = nil
	m.layout()
}

func (m *Model) clearApproval(sendReject bool) {
	if m.approval == nil {
		return
	}
	if sendReject {
		select {
		case m.approval.resp <- false:
		default:
		}
	}
	m.approval = nil
}

func (m *Model) cancelStream() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Model) Close() {
	m.clearApproval(true)
	m.cancelStream()
}

func (m *Model) layout() {
	vh := m.height - headerHeight - (inputLines + inputFrameHeight) - hintHeight
	if m.approval != nil {
		vh -= approvalCardBase + len(m.approval.displayLines())
	}
	if vh < 3 {
		vh = 3
	}
	mainW := m.width
	if m.drawerOpen {
		dw := drawerWidth(m.width)
		mainW = m.width - dw
		if mainW < 24 {
			mainW = 24
			dw = m.width - mainW
		}
		m.drawerViewport.Width = dw
		m.drawerViewport.Height = vh
	}
	m.viewport.Width = mainW
	m.viewport.Height = vh
	m.input.SetWidth(max(10, m.width-4))
	m.input.SetHeight(inputLines)
}

func drawerWidth(total int) int {
	w := total / 3
	if w < 28 {
		w = 28
	}
	if w > 48 {
		w = 48
	}
	if w >= total-20 {
		w = max(12, total/3)
	}
	return w
}

func (m *Model) refreshViewport(forceBottom bool) {
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderTranscript())
	if forceBottom || atBottom {
		m.viewport.GotoBottom()
	}
}

func (m *Model) refreshDrawer() {
	if !m.drawerOpen {
		return
	}
	atBottom := m.drawerViewport.AtBottom()
	m.drawerViewport.SetContent(m.renderDrawer())
	if atBottom {
		m.drawerViewport.GotoBottom()
	}
}

func (m *Model) renderDrawer() string {
	width := max(8, m.drawerViewport.Width-2)
	var b strings.Builder
	b.WriteString(titleStyle.Render("工具"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(strings.Repeat("─", max(0, width))))
	b.WriteString("\n")

	var tools []chatMessage
	for _, msg := range m.messages {
		if msg.role == roleTool {
			tools = append(tools, msg)
		}
	}
	if len(tools) == 0 {
		b.WriteString(mutedStyle.Render("本会话还没有工具调用。"))
		return b.String()
	}
	for i, t := range tools {
		if i > 0 {
			b.WriteString("\n")
		}
		status := "运行中…"
		if t.done {
			if t.toolError {
				status = "失败"
			} else {
				status = fmt.Sprintf("完成 · %s", t.toolDur.Round(time.Millisecond))
			}
		}
		b.WriteString(toolLabelStyle.Render("⚙ " + t.toolName))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(status))
		b.WriteString("\n")
		if t.toolArgs != "" {
			b.WriteString(mutedStyle.Width(width).Render(wrapText("参数: "+compact(t.toolArgs, 200), width)))
			b.WriteString("\n")
		}
		if t.done && t.toolResult != "" {
			preview := compact(t.toolResult, 400)
			style := mutedStyle
			if t.toolError {
				style = errorStyle
			}
			b.WriteString(style.Width(width).Render(wrapText(preview, width)))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func compact(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", "↵")
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes]) + "…"
}

// displayLines 把审批内容切成行，最多 approvalMaxLines 行。
func (a *approvalState) displayLines() []string {
	lines := strings.Split(strings.TrimRight(a.display, "\n"), "\n")
	if len(lines) > approvalMaxLines {
		lines = append(lines[:approvalMaxLines-1], "…")
	}
	return lines
}
