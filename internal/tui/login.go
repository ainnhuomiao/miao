package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/setup"
)

// ---- 首次启动向导 / /login：选服务商 → （OpenAI 兼容填地址）→ 粘贴 key → 自动测试 ----

type wizardStep int

const (
	wizProvider wizardStep = iota
	wizBaseURL
	wizKey
	wizTesting
	wizResult
)

const (
	wizBlinkEvery = 400 * time.Millisecond
	wizDoneDelay  = 700 * time.Millisecond
)

// wizard 是向导的状态；首次启动时单独跑一个程序，/login 时嵌在主界面里。
type wizard struct {
	firstRun bool
	step     wizardStep
	cursor   int
	baseURL  textinput.Model
	key      textinput.Model
	inputErr string

	tester setup.Tester
	saver  func(config.Credentials) (string, error)
	gen    int // 每次测试递增，丢弃过期的测试结果

	blink    bool
	ok       bool
	failure  string
	saved    config.Credentials
	savedMsg string

	done      bool // 成功并已保存，外层可以收起向导
	cancelled bool // 用户放弃（/login 时 Esc，首次启动时 Ctrl+C）
}

type wizardTestMsg struct {
	gen   int
	creds config.Credentials
	err   error
}

type wizardBlinkMsg struct{ gen int }

type wizardDoneMsg struct{}

func newWizard(firstRun bool, tester setup.Tester, saver func(config.Credentials) (string, error)) *wizard {
	if tester == nil {
		tester = setup.DefaultTester
	}
	if saver == nil {
		saver = setup.Save
	}
	key := textinput.New()
	key.Prompt = ""
	key.Placeholder = "粘贴 key"
	key.EchoMode = textinput.EchoPassword
	key.EchoCharacter = '•'
	key.CharLimit = 512
	base := textinput.New()
	base.Prompt = ""
	base.Placeholder = "https://example.com/v1"
	base.CharLimit = 512
	w := &wizard{firstRun: firstRun, key: key, baseURL: base, tester: tester, saver: saver}
	w.applyStyles()
	return w
}

func (w *wizard) applyStyles() {
	for _, in := range []*textinput.Model{&w.key, &w.baseURL} {
		s := in.Styles()
		s.Focused.Text = lipgloss.NewStyle().Foreground(textColor)
		s.Blurred.Text = s.Focused.Text
		s.Focused.Placeholder = mutedStyle
		s.Blurred.Placeholder = mutedStyle
		in.SetStyles(s)
	}
}

func (w *wizard) provider() setup.Provider { return setup.Providers[w.cursor] }

// Update 处理一条消息，返回要执行的命令。
func (w *wizard) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case wizardTestMsg:
		if msg.gen != w.gen || w.step != wizTesting {
			return nil
		}
		return w.onTested(msg)
	case wizardBlinkMsg:
		if msg.gen != w.gen || w.step != wizTesting {
			return nil
		}
		w.blink = !w.blink
		return w.blinkCmd()
	case wizardDoneMsg:
		w.done = true
		return nil
	case tea.KeyPressMsg:
		return w.handleKey(msg)
	}
	// 粘贴等其它消息交给当前输入框
	return w.updateInput(msg)
}

func (w *wizard) updateInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch w.step {
	case wizKey:
		w.key, cmd = w.key.Update(msg)
	case wizBaseURL:
		w.baseURL, cmd = w.baseURL.Update(msg)
	}
	return cmd
}

func (w *wizard) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		w.cancelled = true
		return nil
	}
	switch w.step {
	case wizProvider:
		switch k {
		case "up", "k":
			w.cursor = (w.cursor + len(setup.Providers) - 1) % len(setup.Providers)
		case "down", "j":
			w.cursor = (w.cursor + 1) % len(setup.Providers)
		case "enter":
			if w.provider().NeedsBaseURL() {
				return w.goTo(wizBaseURL)
			}
			return w.goTo(wizKey)
		case "esc":
			if !w.firstRun {
				w.cancelled = true
			}
		}
		return nil
	case wizBaseURL:
		switch k {
		case "enter":
			if err := setup.ValidateBaseURL(w.baseURL.Value()); err != nil {
				w.inputErr = err.Error()
				return nil
			}
			return w.goTo(wizKey)
		case "esc":
			return w.goTo(wizProvider)
		}
		w.inputErr = ""
		return w.updateInput(msg)
	case wizKey:
		switch k {
		case "enter":
			if strings.TrimSpace(w.key.Value()) == "" {
				w.inputErr = "key 不能为空"
				return nil
			}
			return w.startTest()
		case "esc":
			if w.provider().NeedsBaseURL() {
				return w.goTo(wizBaseURL)
			}
			return w.goTo(wizProvider)
		}
		w.inputErr = ""
		return w.updateInput(msg)
	case wizResult:
		if w.ok {
			return nil
		}
		switch k {
		case "enter", "r":
			return w.startTest()
		case "esc":
			return w.goTo(wizKey)
		}
	}
	return nil
}

func (w *wizard) goTo(s wizardStep) tea.Cmd {
	w.step = s
	w.inputErr = ""
	w.key.Blur()
	w.baseURL.Blur()
	switch s {
	case wizKey:
		return w.key.Focus()
	case wizBaseURL:
		return w.baseURL.Focus()
	}
	return nil
}

func (w *wizard) startTest() tea.Cmd {
	w.step = wizTesting
	w.key.Blur()
	w.gen++
	w.blink = false
	w.failure = ""
	gen := w.gen
	creds := setup.Credentials(w.provider(), w.key.Value(), w.baseURL.Value())
	tester := w.tester
	test := func() tea.Msg {
		c, err := tester(context.Background(), creds)
		return wizardTestMsg{gen: gen, creds: c, err: err}
	}
	return tea.Batch(test, w.blinkCmd())
}

func (w *wizard) blinkCmd() tea.Cmd {
	gen := w.gen
	return tea.Tick(wizBlinkEvery, func(time.Time) tea.Msg { return wizardBlinkMsg{gen: gen} })
}

func (w *wizard) onTested(msg wizardTestMsg) tea.Cmd {
	w.step = wizResult
	w.blink = false
	if msg.err != nil {
		w.ok = false
		w.failure = setup.Classify(msg.err).Error()
		return nil
	}
	note, err := w.saver(msg.creds)
	if err != nil {
		w.ok = false
		w.failure = err.Error()
		return nil
	}
	w.ok = true
	w.saved = msg.creds
	w.savedMsg = note
	// 清掉输入框里的明文，避免之后被渲染或残留
	w.key.SetValue("")
	return tea.Tick(wizDoneDelay, func(time.Time) tea.Msg { return wizardDoneMsg{} })
}

// View 渲染向导，水平垂直居中。
func (w *wizard) View(width, height int) string {
	face := catFace
	if w.step == wizTesting && w.blink {
		face = catBlink
	}
	accent := lipgloss.NewStyle().Foreground(accentColor)
	text := lipgloss.NewStyle().Foreground(textColor)
	title := "第一次见面，先配一下 key"
	if !w.firstRun {
		title = "换个 key 或服务商"
	}
	lines := []string{accent.Render(face), mutedStyle.Render(title), ""}

	var hint []string
	switch w.step {
	case wizProvider:
		lines = append(lines, text.Render("选一个服务商"), "")
		for i, p := range setup.Providers {
			if i == w.cursor {
				lines = append(lines, accent.Render("› "+p.Name))
			} else {
				lines = append(lines, mutedStyle.Render("  "+p.Name))
			}
		}
		hint = []string{"↑↓ 选择", "Enter 确认"}
		if !w.firstRun {
			hint = append(hint, "Esc 取消")
		}
	case wizBaseURL:
		lines = append(lines, text.Render(w.provider().Name+" 的接口地址"), "", "› "+w.baseURL.View())
		hint = []string{"Enter 确认", "Esc 返回"}
	case wizKey:
		lines = append(lines, text.Render(w.provider().Name+" 的 API Key"), "", "› "+w.key.View())
		hint = []string{"Enter 确认", "Esc 返回"}
	case wizTesting:
		lines = append(lines, mutedStyle.Render("正在测试连接…"))
		hint = []string{"Ctrl+C 退出"}
	case wizResult:
		if w.ok {
			lines = append(lines, lipgloss.NewStyle().Foreground(agentColor).Render("●")+" "+text.Render("连上了"))
			if w.savedMsg != "" {
				lines = append(lines, mutedStyle.Render(w.savedMsg))
			}
		} else {
			lines = append(lines, lipgloss.NewStyle().Foreground(errorColor).Render("●")+" "+text.Render(w.failure))
			hint = []string{"Enter 重试", "Esc 返回改 key"}
		}
	}
	if w.inputErr != "" {
		lines = append(lines, "", errorStyle.Render(w.inputErr))
	}
	block := lipgloss.JoinVertical(lipgloss.Center, lines...)
	body := lipgloss.Place(width, max(1, height-1), lipgloss.Center, lipgloss.Center, block)
	foot := mutedStyle.Render(strings.Join(hint, " · "))
	return body + "\n" + lipgloss.PlaceHorizontal(width, lipgloss.Center, foot)
}

// ---- 首次启动：单独的小程序 ----

type setupProgram struct {
	w             *wizard
	width, height int
}

func (p *setupProgram) Init() tea.Cmd { return nil }

func (p *setupProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		p.width, p.height = ws.Width, ws.Height
		return p, nil
	}
	cmd := p.w.Update(msg)
	if p.w.done || p.w.cancelled {
		return p, tea.Quit
	}
	return p, cmd
}

func (p *setupProgram) View() tea.View {
	v := tea.NewView(p.w.View(max(p.width, 20), max(p.height, 10)))
	v.AltScreen = true
	return v
}

// ErrSetupCancelled 表示用户在首次启动向导里退出了。
var ErrSetupCancelled = errSetupCancelled{}

type errSetupCancelled struct{}

func (errSetupCancelled) Error() string {
	return "已取消配置。之后可以运行 `miao init`，或设置环境变量 MIAO_API_KEY"
}

// RunSetup 在没有 key 的交互终端里跑首次启动向导；成功返回保存好的连接信息。
func RunSetup() (config.Credentials, error) {
	initMarkdown()
	termDark = mdDark
	setTheme("auto")
	loadSavedTheme()
	w := newWizard(true, nil, nil)
	if _, err := tea.NewProgram(&setupProgram{w: w}).Run(); err != nil {
		return config.Credentials{}, err
	}
	if !w.done {
		return config.Credentials{}, ErrSetupCancelled
	}
	return w.saved, nil
}

// ---- /login：嵌在主界面里 ----

// LoginHandler 在 /login 成功保存后被调用，用新的连接信息替换客户端，返回新的模型名。
type LoginHandler func(c config.Credentials) (model string, err error)

var loginHandler LoginHandler

// SetLoginHandler 设置 /login 成功后切换客户端的回调。
func SetLoginHandler(h LoginHandler) { loginHandler = h }

// loginTester / loginSaver 供测试替换，默认走真实网络和磁盘。
var (
	loginTester setup.Tester
	loginSaver  func(config.Credentials) (string, error)
)

func (m *Model) openLogin() tea.Cmd {
	if m.streaming {
		m.notice("正在生成，等这一轮结束再 /login。")
		return nil
	}
	m.login = newWizard(false, loginTester, loginSaver)
	m.input.SetValue("")
	return nil
}

// loginUpdate 把属于向导的消息交给它；返回 false 表示交还主界面处理（窗口大小、流式事件等）。
func (m *Model) loginUpdate(msg tea.Msg) (bool, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg, wizardTestMsg, wizardBlinkMsg, wizardDoneMsg:
	default:
		return false, nil
	}
	w := m.login
	cmd := w.Update(msg)
	switch {
	case w.cancelled:
		m.login = nil
		m.layout()
		m.notice("已取消 /login，连接信息没有改动。")
		return true, nil
	case w.done:
		m.login = nil
		m.layout()
		m.applyLogin(w.saved, w.savedMsg)
		return true, nil
	}
	return true, cmd
}

func (m *Model) applyLogin(c config.Credentials, savedMsg string) {
	p, _ := setup.ProviderByID(c.Provider)
	if loginHandler == nil {
		m.notice("连上了 " + p.Name + "。" + savedMsg + "，重启后生效。")
		return
	}
	model, err := loginHandler(c)
	if err != nil {
		m.notice("已保存，但切换客户端失败：" + err.Error() + "。重启后生效。")
		return
	}
	if model != "" {
		m.modelName = model
	}
	m.invalidateThinking()
	m.notice("已切换到 " + p.Name + "（模型 " + m.modelName + "）。" + savedMsg)
}
