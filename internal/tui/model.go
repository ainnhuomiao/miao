package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"agnet/internal/agent"
	"agnet/internal/llm"
)

type role int

const (
	roleUser role = iota
	roleAssistant
	roleNotice
	roleError
)

// chatMessage 是界面上显示的一条消息。
// reasoning 单独保存，便于折叠显示；content 才是最终回答。
type chatMessage struct {
	role      role
	content   string
	reasoning string
	done      bool
}

// streamEventMsg 是后台流式 goroutine 发给 Update 的事件。
type streamEventMsg struct {
	content   string
	reasoning string
	done      bool
	err       error
}

// Model 是 TUI 的全部状态。
type Model struct {
	ag        *agent.Agent
	modelName string

	width  int
	height int
	ready  bool

	viewport viewport.Model
	input    textarea.Model
	spinner  spinner.Model

	messages []chatMessage

	streaming bool
	cancel    context.CancelFunc
	events    chan streamEventMsg

	showReasoning bool
}

// New 创建 TUI 模型。此时还没有终端尺寸，收到 WindowSizeMsg 后才开始渲染。
func New(ag *agent.Agent, modelName string) *Model {
	ta := textarea.New()
	ta.Placeholder = "输入消息，Enter 发送…"
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(inputLines)
	ta.SetWidth(60)
	// 默认 Enter 是换行；这里改成 Ctrl+J，Enter 留给"发送"。
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

	return &Model{
		ag:        ag,
		modelName: modelName,
		input:     ta,
		spinner:   sp,
		viewport:  vp,
		messages: []chatMessage{{
			role:    roleNotice,
			content: "已就绪。输入消息开始对话，输入 /help 查看命令。",
			done:    true,
		}},
	}
}

// Init 实现 tea.Model。启动输入框的光标闪烁。
func (m *Model) Init() tea.Cmd {
	return textarea.Blink
}

// Update 实现 tea.Model，是整个 TUI 的核心状态机。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.layout()
		m.refreshViewport(true)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case spinner.TickMsg:
		if !m.streaming {
			return m, nil // 流结束后让 tick 链自然停下
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case streamEventMsg:
		return m.handleStreamEvent(msg)
	}

	// 其余消息（光标闪烁等）交给输入框。
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View 实现 tea.Model。
func (m *Model) View() string {
	return m.view()
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.streaming {
			m.cancelStream()
			return m, nil
		}
		return m, tea.Quit

	case "esc":
		if m.streaming {
			m.cancelStream()
		}
		return m, nil

	case "ctrl+r":
		m.showReasoning = !m.showReasoning
		m.refreshViewport(false)
		return m, nil

	case "pgup":
		m.viewport.PageUp()
		return m, nil

	case "pgdown":
		m.viewport.PageDown()
		return m, nil

	case "enter":
		if m.streaming {
			return m, nil // 生成中不允许发送新消息
		}
		return m, m.submit()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// submit 处理一次输入：斜杠命令直接执行，普通文本发起对话。
func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	m.input.Reset()

	if strings.HasPrefix(text, "/") {
		return m.runCommand(text)
	}

	// 用带缓冲的 channel 把流式事件送进 Bubble Tea 的事件循环。
	// 这样网络 goroutine 和 UI 完全解耦。
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
		err := ag.Send(ctx, text, func(d llm.Delta) error {
			select {
			case events <- streamEventMsg{content: d.Content, reasoning: d.Reasoning}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		events <- streamEventMsg{done: true, err: err}
	}()

	return tea.Batch(waitForEvent(events), m.spinner.Tick)
}

// waitForEvent 是一个 tea.Cmd：它阻塞等待一个流式事件，然后交给 Update。
// 每处理完一个事件，Update 会再返回一个新的 waitForEvent，形成读取链。
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

		last := &m.messages[len(m.messages)-1]
		last.done = true

		switch {
		case errors.Is(msg.err, context.Canceled):
			m.messages = append(m.messages, chatMessage{
				role: roleNotice, content: "已取消生成（本轮未加入上下文）。", done: true,
			})
		case msg.err != nil:
			m.messages = append(m.messages, chatMessage{
				role: roleError, content: "请求失败: " + msg.err.Error(), done: true,
			})
		case last.content == "":
			m.messages = append(m.messages, chatMessage{
				role: roleNotice, content: "模型没有返回文本内容。", done: true,
			})
		}

		m.refreshViewport(false)
		return m, nil
	}

	last := &m.messages[len(m.messages)-1]
	last.content += msg.content
	last.reasoning += msg.reasoning
	m.refreshViewport(false)

	return m, waitForEvent(m.events)
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

func (m *Model) cancelStream() {
	if m.cancel != nil {
		m.cancel()
	}
}

// Close 在程序退出后调用，确保后台请求不会继续占用资源。
func (m *Model) Close() {
	m.cancelStream()
}

// layout 根据终端尺寸计算各区域大小。
//
// 垂直方向固定分配：
//
//	header(2) + viewport(vh) + input(inputLines+2) + hint(1) = height
func (m *Model) layout() {
	vh := m.height - headerHeight - (inputLines + inputFrameHeight) - hintHeight
	if vh < 3 {
		vh = 3
	}
	m.viewport.Width = m.width
	m.viewport.Height = vh
	m.input.SetWidth(max(10, m.width-4))
	m.input.SetHeight(inputLines)
}

func (m *Model) refreshViewport(forceBottom bool) {
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderTranscript())
	if forceBottom || atBottom {
		m.viewport.GotoBottom()
	}
}
