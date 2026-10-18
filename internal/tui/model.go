package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/session"
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
	toolLive   string // 运行中的最新一行输出

	mdCache string // 完成后 Glamour 渲染结果缓存
	mdW     int
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
	usage      *llm.Usage
	done       bool
	err        error
}

// approvalRequestMsg 由工具 goroutine 经 Program.Send 投递，请求用户确认。
type approvalRequestMsg struct {
	toolName string
	display  string
	args     json.RawMessage
	resp     chan bool
}

type approvalClearMsg struct{}

type approvalState struct {
	toolName string
	display  string
	args     json.RawMessage
	resp     chan bool
}

// Model 是 TUI 的全部状态。
type Model struct {
	side      sidebarState // 左侧会话侧栏
	keysCard  bool         // ? 快捷键速查卡，见 keys.go
	picker    pickerState  // /sessions 会话选择列表
	git       gitState     // /commit 进行中
	rewind    rewindState  // Esc Esc 回退
	img       imageState   // Ctrl+V / @ 图片，见 images.go
	sub       subagentState
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

	streaming  bool
	compacting bool // 正在压缩上下文（streaming 同时为 true）
	shelling   bool // 正在执行 ! 命令（streaming 同时为 true）
	cancel     context.CancelFunc
	events     chan streamEventMsg

	showReasoning bool
	streamStart   time.Time
	drawerOpen    bool
	drawerFocus   bool // 焦点在抽屉时滚轮/Pg 作用于抽屉
	// 左侧会话侧栏：一期接口预留，按 [ ] 暂为 noop
	sidebarReserved bool

	approval  *approvalState
	mention   mentionState // @ 引用文件补全缓存
	userCmds  userCmdState // 自定义斜杠命令缓存
	themeOrig string       // /theme 预览前的主题，空表示没在预览
	palette   paletteState
	models    modelListState

	todoPanel bool // Ctrl+T 展开任务清单

	// usage 是最近一轮（含工具循环里的多次请求）累计用量，底栏显示 ↑↓ 与缓存命中。
	usage      *llm.Usage
	ctxTokens  int     // 最近一次请求的 prompt+completion，即当前上下文长度（顶栏吃豆人）
	usageFresh bool    // 新一轮开始后的第一份用量覆盖旧值，之后累加
	costUSD    float64 // 本会话按模型单价累计的估算花费

	store *session.Store
	sess  *session.Session
}

// New 创建 TUI 模型。
func New(ag *agent.Agent, modelName string) *Model {
	ta := textarea.New()
	ta.Placeholder = "输入消息，Enter 发送…"
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(1)
	ta.SetPromptFunc(2, func(i int) string {
		if i == 0 {
			return "› "
		}
		return "  "
	})
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

	m := &Model{
		ag:             ag,
		modelName:      modelName,
		input:          ta,
		spinner:        sp,
		viewport:       vp,
		drawerViewport: dvp,
		messages: []chatMessage{{
			role:    roleNotice,
			content: "已就绪。输入消息开始对话，输入 /help 查看命令。",
			done:    true,
		}},
	}
	if ag.AgentsMDLoaded() {
		m.messages = append(m.messages, chatMessage{role: roleNotice, content: "已加载 AGENTS.md", done: true})
	}
	for _, n := range startupNotices {
		m.messages = append(m.messages, chatMessage{role: roleNotice, content: n, done: true})
	}
	startupNotices = nil // 只显示一次
	m.initSessionStore()
	if resumeOnStart {
		resumeOnStart = false
		m.resumeLastSession()
	}
	return m
}

// resumeOnStart 由 -c / --resume 设置，启动时恢复最近一次会话。
var resumeOnStart bool

// SetResumeOnStart 让下一次 New 恢复最近会话。
func SetResumeOnStart(v bool) { resumeOnStart = v }

func (m *Model) resumeLastSession() {
	if m.store == nil {
		m.notice("会话存储不可用，无法恢复。")
		return
	}
	list, err := m.store.List()
	if err != nil || len(list) == 0 {
		m.notice("没有可恢复的会话。")
		return
	}
	m.loadSession(list[0].ID)
	m.notice("已恢复最近会话。")
}

// startupNotices 是启动时要显示一次的灰色提示（例如目录迁移）。
var startupNotices []string

// SetStartupNotices 设置启动时显示一次的提示。
func SetStartupNotices(notes []string) { startupNotices = notes }

// initSessionStore 打开默认会话目录并创建空会话；失败时静默降级（不持久化）。
// 默认不加载最近会话；-c/--resume 时恢复最近一次。
func (m *Model) initSessionStore() {
	dir, err := session.DefaultDir()
	if err != nil {
		return
	}
	store, err := session.NewStore(dir)
	if err != nil {
		return
	}
	m.store = store
	m.sess = store.New(m.modelName, m.ag.Workspace())
	m.syncSnapshotDir()
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.fetchModelsCmd())
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

	case gitMsg:
		return m.handleGitMsg(msg)

	case clipImageMsg:
		return m, m.onClipImage(msg)

	case tea.KeyMsg:
		if m.keysCard { // 速查卡打开时任意键关闭
			m.keysCard = false
			m.layout()
			return m, nil
		}
		if m.approval == nil {
			if msg.String() == "ctrl+b" {
				m.toggleSidebar()
				return m, nil
			}
			if ok, cmd := m.sidebarKey(msg); ok {
				return m, cmd
			}
			if ok, cmd := m.sessionPickerKey(msg); ok {
				return m, cmd
			}
			if ok, cmd := m.rewindKey(msg); ok {
				return m, cmd
			}
		}
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
			args:     msg.args,
			resp:     msg.resp,
		}
		m.layout()
		return m, nil

	case modelsMsg:
		m.handleModelsMsg(msg)
		return m, nil

	case compactDoneMsg:
		return m.handleCompactDone(msg)

	case bangDoneMsg:
		return m.handleBangDone(msg)

	case mcpNoticeMsg:
		m.notice(string(msg))
		return m, nil

	case approvalClearMsg:
		m.clearApproval(false)
		m.layout()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.fitInput()
	m.syncPalette()
	return m, cmd
}

func (m *Model) View() string {
	return m.view()
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 审批叠层优先吃键。
	if m.approval != nil {
		switch msg.String() {
		case "tab":
			return m, nil
		case "pgup", "pgdown":
			if m.drawerOpen {
				if msg.String() == "pgup" {
					m.drawerViewport.PageUp()
				} else {
					m.drawerViewport.PageDown()
				}
			}
			return m, nil
		case "y", "Y":
			m.resolveApproval(true)
			return m, nil
		case "a", "A": // 本会话总是允许
			m.ag.AllowForSession(m.approval.toolName, m.approval.args)
			m.resolveApproval(true)
			return m, nil
		case "b", "B": // 本轮其余文件编辑都允许
			if isFileEdit(m.approval.toolName) {
				m.ag.AllowEditsThisRound()
				m.resolveApproval(true)
				return m, nil
			}
		case "esc":
			if m.drawerOpen { // Esc 顺序：先关抽屉，再拒绝
				m.drawerOpen = false
				m.drawerFocus = false
				m.layout()
				m.refreshViewport(false)
				return m, nil
			}
			m.resolveApproval(false)
			return m, nil
		case "n", "N":
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

	if ok, cmd := m.handleImageKey(msg); ok {
		return m, cmd
	}
	if ok, cmd := m.handlePaletteKey(msg); ok {
		return m, cmd
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
		return m, nil // 工具状态条常驻，不再有抽屉

	case "shift+tab":
		mode := m.ag.CycleMode()
		m.notice("权限模式：" + mode.String())
		return m, nil

	case "?":
		// 输入框为空时 ? 弹出键位表；有内容时照常输入问号。
		if m.input.Value() == "" {
			m.keysCard = true
			m.layout()
			return m, nil
		}

	case "[", "]":
		// 一期：左侧会话侧栏接口预留，暂 noop
		_ = m.sidebarReserved
		return m, nil

	case "ctrl+t":
		m.toggleTodoPanel()
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
	m.fitInput()
	m.syncPalette()
	return m, cmd
}

func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" && len(m.img.pending) > 0 {
		text = "请看图片"
	}
	if text == "" {
		return nil
	}
	m.input.Reset()
	m.fitInput()
	m.syncPalette()

	if strings.HasPrefix(text, "/") {
		return m.runCommand(text)
	}
	if strings.HasPrefix(text, "!") {
		return m.runBang(strings.TrimPrefix(text, "!"))
	}
	return m.sendMessage(text, text)
}

// sendMessage 把一条消息发给模型：界面显示 display，实际发送 prompt（自定义命令时两者不同）。
func (m *Model) sendMessage(display, prompt string) tea.Cmd {
	text := display
	m.markRewindPoint(text)
	events := make(chan streamEventMsg, 128)
	ctx, cancel := context.WithCancel(context.Background())

	m.streaming = true
	m.streamStart = time.Now()
	m.usageFresh = true
	m.cancel = cancel
	m.events = events
	m.messages = append(m.messages, chatMessage{role: roleUser, content: text, done: true})
	sendText := m.expandMentions(prompt) // 「已附加 …」灰字放在用户消息下面
	m.collectImages(prompt)
	m.messages = append(m.messages, chatMessage{role: roleAssistant})
	m.refreshViewport(true)

	ag := m.ag
	go func() {
		defer close(events)
		err := ag.Send(ctx, sendText, func(e agent.Event) error {
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
				usage:      e.Usage,
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
		case errors.Is(msg.err, agent.ErrMaxToolRounds):
			m.messages = append(m.messages, chatMessage{
				role: roleNotice, content: msg.err.Error(), done: true,
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

		m.saveCurrentSession()

		m.refreshViewport(false)
		m.refreshDrawer()
		if msg.err == nil {
			if n := m.ag.TrimOldToolResults(compactKeepTurns, 1500); n > 0 {
				m.ctxTokens = m.ag.EstimateContext()
				m.notice(fmt.Sprintf("已折叠 %d 条早期工具输出。", n))
				m.saveCurrentSession()
			}
			if m.needAutoCompact() {
				return m, m.startCompact(true)
			}
		}
		return m, nil
	}

	switch msg.kind {
	case agent.EventUsage:
		if msg.usage != nil {
			m.ctxTokens = msg.usage.PromptTokens + msg.usage.CompletionTokens
			if usd, ok := llm.EstimateCostUSD(m.modelName, *msg.usage, priceOverride("PRICE_IN"), priceOverride("PRICE_OUT")); ok {
				m.costUSD += usd
			}
			if m.usageFresh || m.usage == nil {
				u := *msg.usage
				m.usage = &u
				m.usageFresh = false
			} else {
				m.usage.PromptTokens += msg.usage.PromptTokens
				m.usage.CompletionTokens += msg.usage.CompletionTokens
				m.usage.TotalTokens += msg.usage.TotalTokens
				m.usage.PromptCacheHitTokens += msg.usage.PromptCacheHitTokens
			}
		}
		return m, waitForEvent(m.events)
	case agent.EventSubagentUsage:
		m.addSubagentUsage(msg.usage)
		return m, waitForEvent(m.events)
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
	case agent.EventToolProgress:
		for i := len(m.messages) - 1; i >= 0; i-- {
			if cm := &m.messages[i]; cm.role == roleTool && !cm.done && cm.toolCallID == msg.toolCallID {
				cm.toolLive = msg.toolResult
				break
			}
		}
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
		m.beginNewSession("对话已清空，开始新会话。")

	case "/new":
		m.beginNewSession("已新建会话。")

	case "/sessions":
		m.openSessionPicker()

	case "/load":
		if len(fields) < 2 {
			m.notice("用法：/load <会话 id>（或用 /sessions 直接选）")
		} else {
			m.loadSession(fields[1])
		}

	case "/model":
		if len(fields) < 2 {
			m.notice("当前模型：" + m.modelName + "。用法：/model <模型名>")
			return m.fetchModelsCmd()
		}
		m.switchModel(fields[1])

	case "/undo":
		m.undo()

	case "/compact":
		return m.startCompact(false)

	case "/diff":
		m.showDiff()

	case "/commit":
		return m.startCommit()

	case "/fork":
		m.forkSession()

	case "/help":
		m.messages = append(m.messages, chatMessage{
			role: roleNotice, content: helpText + "\n\n" + keymapText(), done: true,
		})
		m.refreshViewport(true)

	case "/theme":
		if len(fields) < 2 {
			m.notice("当前主题：" + m.savedTheme() + "。输入 /theme 加空格选择，或 /theme <名字>")
		} else {
			m.applyTheme(fields[1])
		}

	case "/subagents":
		m.openSubagentsMenu()

	case "/mcp":
		m.notice(mcpSummary())

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

	case "/export":
		path := ""
		if len(fields) > 1 {
			path = fields[1]
		}
		m.exportSession(path)

	case "/hooks":
		m.notice(m.ag.Hooks().Summary())

	case "/apply":
		return m.applyPlan()

	case "/cost":
		m.showCost()

	case "/init":
		m.initAgentsMD()

	case "/trust-project":
		m.trustProject()

	default:
		if cmd, ok := m.runUserCommand(text); ok {
			return cmd
		}
		m.messages = append(m.messages, chatMessage{
			role:    roleNotice,
			content: "未知命令 " + fields[0] + "，输入 /help 查看帮助。",
			done:    true,
		})
		m.refreshViewport(false)
	}
	return nil
}

func (m *Model) notice(content string) {
	m.messages = append(m.messages, chatMessage{role: roleNotice, content: content, done: true})
	m.refreshViewport(true)
}

// saveCurrentSession 把 Agent 历史写入当前会话文件；空会话不落盘。
func (m *Model) saveCurrentSession() {
	if m.store == nil || m.sess == nil {
		return
	}
	m.sess.Messages = m.ag.History()
	m.sess.Model = m.modelName
	if ws := m.ag.Workspace(); ws != "" {
		m.sess.Workspace = ws
	}
	if err := m.store.Save(m.sess); err != nil {
		m.messages = append(m.messages, chatMessage{
			role: roleError, content: "保存会话失败: " + err.Error(), done: true,
		})
	}
	m.refreshSidebar()
}

// beginNewSession 清空 Agent 历史与界面，并换新会话 id（旧文件保留）。
func (m *Model) beginNewSession(notice string) {
	m.ag.Reset()
	m.usage = nil
	m.ctxTokens = 0
	m.usageFresh = false
	m.costUSD = 0
	if m.store != nil {
		m.sess = m.store.New(m.modelName, m.ag.Workspace())
		m.syncSnapshotDir()
	} else {
		m.sess = nil
	}
	m.messages = []chatMessage{{role: roleNotice, content: notice, done: true}}
	m.refreshViewport(true)
	m.refreshDrawer()
	m.refreshSidebar()
}

func (m *Model) listSessions() {
	if m.store == nil {
		m.notice("会话存储不可用。")
		return
	}
	list, err := m.store.List()
	if err != nil {
		m.notice("列出会话失败: " + err.Error())
		return
	}
	if len(list) == 0 {
		m.notice("暂无已保存会话。")
		return
	}
	var b strings.Builder
	b.WriteString("已保存会话（最近在前）：\n")
	for _, meta := range list {
		title := meta.Title
		if title == "" {
			title = "（无标题）"
		}
		b.WriteString(fmt.Sprintf("  %s  %s  %s  (%d 条)\n",
			meta.ID,
			meta.Updated.Local().Format("2006-01-02 15:04"),
			title,
			meta.MsgCount,
		))
	}
	b.WriteString("加载：/load <id>")
	m.notice(strings.TrimRight(b.String(), "\n"))
}

func (m *Model) loadSession(id string) {
	if m.store == nil {
		m.notice("会话存储不可用。")
		return
	}
	sess, err := m.store.Load(id)
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			m.notice("会话不存在: " + id)
			return
		}
		m.notice("加载会话失败: " + err.Error())
		return
	}
	m.sess = sess
	m.syncSnapshotDir()
	m.ag.ClearSessionAllows()
	m.ag.SetHistory(sess.Messages)
	if sess.Model != "" && sess.Model != m.modelName {
		m.applyModel(sess.Model)
	}
	m.usage = nil
	m.ctxTokens = 0
	m.usageFresh = false
	m.costUSD = 0
	m.messages = chatMessagesFromHistory(sess)
	m.refreshViewport(true)
	m.refreshDrawer()
	m.refreshSidebar()
}

// chatMessagesFromHistory 把持久化的 llm 消息还原为界面气泡（含工具卡）。
func chatMessagesFromHistory(sess *session.Session) []chatMessage {
	title := sess.Title
	if title == "" {
		title = session.TitleFrom(sess.Messages)
	}
	out := []chatMessage{{
		role:    roleNotice,
		content: fmt.Sprintf("已加载会话 %s · %s", sess.ID, title),
		done:    true,
	}}
	for _, msg := range sess.Messages {
		switch msg.Role {
		case llm.RoleUser:
			out = append(out, chatMessage{role: roleUser, content: msg.Content, done: true})
			out = append(out, imageNotices(msg.Images)...)
		case llm.RoleAssistant:
			out = append(out, chatMessage{role: roleAssistant, content: msg.Content, done: true})
			for _, tc := range msg.ToolCalls {
				out = append(out, chatMessage{
					role:       roleTool,
					toolCallID: tc.ID,
					toolName:   tc.Function.Name,
					toolArgs:   tc.Function.Arguments,
					done:       false,
				})
			}
		case llm.RoleTool:
			updated := false
			for i := len(out) - 1; i >= 0; i-- {
				cm := &out[i]
				if cm.role == roleTool && !cm.done &&
					(cm.toolCallID == msg.ToolCallID || (msg.ToolCallID == "" && cm.toolName == msg.Name)) {
					cm.toolResult = msg.Content
					cm.toolName = firstNonEmpty(cm.toolName, msg.Name)
					cm.done = true
					updated = true
					break
				}
			}
			if !updated {
				out = append(out, chatMessage{
					role:       roleTool,
					toolCallID: msg.ToolCallID,
					toolName:   msg.Name,
					toolResult: msg.Content,
					done:       true,
				})
			}
		}
	}
	// 未匹配到结果的工具卡仍标为完成，避免界面一直显示运行中。
	for i := range out {
		if out[i].role == roleTool {
			out[i].done = true
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
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
	vh := m.height - headerHeight - (m.inputHeight() + inputFrameHeight) - hintHeight
	if m.approval != nil {
		vh -= approvalCardBase + len(m.approval.displayLines())
	}
	if vh < 3 {
		vh = 3
	}
	m.viewport.Width = m.width - m.sidebarCols()
	if h := m.paletteHeight(); h > 0 {
		vh -= h // 命令菜单替代工具状态条
	} else {
		vh -= activityMaxRows // 工具状态条常驻占位
	}
	vh -= m.todoPanelHeight()
	vh -= m.rewindHeight()
	if m.keysCard {
		vh -= keysCardHeight()
	}
	vh -= m.pickerHeight()
	m.viewport.Height = max(3, vh)
	m.input.SetWidth(max(10, m.columnWidth()-4))
	m.input.SetHeight(m.inputHeight())
}

func (m *Model) refreshViewport(forceBottom bool) {
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(lipgloss.NewStyle().MarginLeft(m.columnMargin()).Render(m.renderTranscript()))
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
	width := max(8, m.drawerViewport.Width)
	var b strings.Builder
	if m.approval != nil {
		b.WriteString(titleStyle.Render(truncateWidth("待确认 · "+m.approval.title(), width)))
		b.WriteString("\n")
		for _, l := range m.approval.body() {
			b.WriteString(diffLineStyle(l).Render(truncateWidth(l, width)))
			b.WriteString("\n")
		}
		b.WriteString(mutedStyle.Render(strings.Repeat("─", width)))
		b.WriteString("\n")
	}

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
		b.WriteString(toolLabelStyle.Render(truncateWidth("⚙ "+t.toolName, width)))
		b.WriteString("\n")
		status := "运行中…"
		if t.done {
			if t.toolError {
				status = "✗ 失败"
			} else {
				status = "✓ " + t.toolDur.Round(time.Millisecond).String()
			}
		}
		meta := status
		if a := formatArgs(t.toolName, t.toolArgs); a != "" {
			meta += " · " + a
		}
		b.WriteString(mutedStyle.Render(wrapText(meta, width)))
		b.WriteString("\n")
		if t.done && t.toolResult != "" {
			b.WriteString(mutedStyle.Render(strings.Repeat("─", min(width, 12))))
			b.WriteString("\n")
			style := lipgloss.NewStyle()
			if t.toolError {
				style = errorStyle
			}
			lines := strings.Split(strings.TrimRight(t.toolResult, "\n"), "\n")
			const maxLines = 40
			for k, l := range lines {
				if k == maxLines {
					b.WriteString(mutedStyle.Render(fmt.Sprintf("…还有 %d 行", len(lines)-maxLines)))
					b.WriteString("\n")
					break
				}
				b.WriteString(style.Render(truncateWidth(l, width)))
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// drawerTitle 是抽屉标题：工具 · 本轮调用次数。
func (m *Model) drawerTitle() string {
	n := 0
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == roleUser {
			break
		}
		if m.messages[i].role == roleTool {
			n++
		}
	}
	return fmt.Sprintf(" 工具 · %d ", n)
}

// formatArgs 把工具参数 JSON 写成 key: value；run_command 写成命令行。
func formatArgs(tool, raw string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || len(obj) == 0 {
		return strings.TrimSpace(raw)
	}
	if tool == "run_command" {
		parts := []string{fmt.Sprint(obj["command"])}
		if args, ok := obj["args"].([]any); ok {
			for _, a := range args {
				parts = append(parts, fmt.Sprint(a))
			}
		}
		return "$ " + strings.Join(parts, " ")
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		v := obj[k]
		str, ok := v.(string)
		if !ok {
			bs, _ := json.Marshal(v)
			str = string(bs)
		}
		if k == "content" {
			str = fmt.Sprintf("%d 字节", len(str))
		}
		out = append(out, k+": "+compact(str, 60))
	}
	return strings.Join(out, " · ")
}

func compact(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", "↵")
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes]) + "…"
}

// title 是审批卡标题。
func (a *approvalState) title() string {
	if isFileEdit(a.toolName) {
		head, _, _ := strings.Cut(a.display, "\n")
		return "确认写入 " + head
	}
	return "确认执行 " + a.toolName
}

// body 是标题之下的全部内容行。
func (a *approvalState) body() []string {
	if isFileEdit(a.toolName) {
		_, rest, _ := strings.Cut(a.display, "\n")
		if rest == "" {
			return nil
		}
		return strings.Split(rest, "\n")
	}
	return strings.Split(strings.TrimRight(a.display, "\n"), "\n")
}

// displayLines 是卡片里显示的行：最多 approvalMaxLines 行，超出部分折叠成一行提示。
func (a *approvalState) displayLines() []string {
	lines := a.body()
	if len(lines) > approvalMaxLines {
		more := len(lines) - (approvalMaxLines - 1)
		lines = append(append([]string{}, lines[:approvalMaxLines-1]...),
			fmt.Sprintf("…还有 %d 行", more))
	}
	return lines
}

// inputHeight 按内容算输入框行数（含软换行），1 到 inputMaxLines 行。
func (m *Model) inputHeight() int {
	w := max(1, m.input.Width())
	n := 0
	for _, l := range strings.Split(m.input.Value(), "\n") {
		n += max(1, (lipgloss.Width(l)+w-1)/w)
	}
	return min(max(1, n), inputMaxLines)
}

// fitInput 在输入行数变化时重新布局。
func (m *Model) fitInput() {
	if h := m.inputHeight(); h != m.input.Height() {
		m.input.SetHeight(h)
		m.layout()
		m.refreshViewport(false)
	}
}

// columnWidth 是对话区和输入框共用的列宽：最多 columnMax，宽屏居中。
func (m *Model) columnWidth() int {
	w := m.viewport.Width
	if w <= 0 {
		w = m.width
	}
	return min(w, columnMax)
}

func (m *Model) columnMargin() int {
	return max(0, (m.viewport.Width-m.columnWidth())/2)
}

// isFileEdit 判断审批卡是否为文件写入类（标题 + diff 行）。
func isFileEdit(tool string) bool { return tool == "write_file" || tool == "edit_file" }
