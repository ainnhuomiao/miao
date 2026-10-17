package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// paletteMaxRows 是斜杠命令菜单最多显示的行数，超出滚动。
const paletteMaxRows = 8

// paletteCmd 是菜单里的一条命令。
type paletteCmd struct {
	name    string
	desc    string
	needArg bool // 需要参数：Enter 等同补全
}

// paletteState 是斜杠命令菜单状态。
type paletteState struct {
	sel    int
	top    int
	query  string // 上次筛选用的内容，变化时重置选中
	hidden string // Esc 关闭时的输入内容，内容不变则保持隐藏
	esc    bool
	rows   int // 上次布局时菜单占的行数（0 表示未显示）
}

func (m *Model) paletteCommands() []paletteCmd {
	reasoning := "收起"
	if m.showReasoning {
		reasoning = "展开"
	}
	return []paletteCmd{
		{name: "/help", desc: "显示帮助"},
		{name: "/new", desc: "新建会话"},
		{name: "/sessions", desc: "列出已保存会话"},
		{name: "/load", desc: "加载会话 <id>", needArg: true},
		{name: "/clear", desc: "清空对话并换新会话"},
		{name: "/reasoning", desc: "思考过程：展开/收起（当前：" + reasoning + "）"},
		{name: "/exit", desc: "退出"},
	}
}

// paletteOpen 判断菜单是否显示：内容以 / 开头且仍在命令名部分。
func (m *Model) paletteOpen() bool {
	if m.approval != nil {
		return false
	}
	v := m.input.Value()
	if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \t\n") {
		return false
	}
	if m.palette.esc && v == m.palette.hidden {
		return false
	}
	return true
}

// paletteMatches 按前缀优先、子序列其次筛选命令。
func (m *Model) paletteMatches() []paletteCmd {
	q := strings.ToLower(strings.TrimPrefix(m.input.Value(), "/"))
	var prefix, fuzzy []paletteCmd
	for _, c := range m.paletteCommands() {
		name := strings.TrimPrefix(c.name, "/")
		switch {
		case strings.HasPrefix(name, q):
			prefix = append(prefix, c)
		case isSubsequence(q, name):
			fuzzy = append(fuzzy, c)
		}
	}
	return append(prefix, fuzzy...)
}

func isSubsequence(q, s string) bool {
	qr := []rune(q)
	i := 0
	for _, r := range s {
		if i < len(qr) && qr[i] == r {
			i++
		}
	}
	return i == len(qr)
}

// paletteHeight 是菜单显示时占的行数；未显示返回 0。
func (m *Model) paletteHeight() int {
	if !m.paletteOpen() {
		return 0
	}
	return max(1, min(m.paletteWindow(), len(m.paletteMatches())))
}

// paletteWindow 是菜单可见行数上限：不超过 paletteMaxRows，且给对话区留最少 3 行。
func (m *Model) paletteWindow() int {
	avail := m.height - headerHeight - (m.inputHeight() + inputFrameHeight) - hintHeight - 3
	if m.approval != nil {
		avail -= approvalCardBase + len(m.approval.displayLines())
	}
	return max(1, min(paletteMaxRows, avail))
}

// syncPalette 在输入变化后更新选中项，并在菜单行数变化时重新布局。
func (m *Model) syncPalette() {
	v := m.input.Value()
	if m.palette.esc && v != m.palette.hidden {
		m.palette.esc = false
	}
	if v != m.palette.query {
		m.palette.query = v
		m.palette.sel, m.palette.top = 0, 0
	}
	n := len(m.paletteMatches())
	if m.palette.sel >= n {
		m.palette.sel = max(0, n-1)
	}
	m.paletteScroll()
	if h := m.paletteHeight(); h != m.palette.rows {
		m.palette.rows = h
		m.layout()
		m.refreshViewport(false)
	}
}

func (m *Model) paletteScroll() {
	if m.palette.sel < m.palette.top {
		m.palette.top = m.palette.sel
	}
	if win := m.paletteWindow(); m.palette.sel >= m.palette.top+win {
		m.palette.top = m.palette.sel - win + 1
	}
}

// handlePaletteKey 在菜单打开时处理按键；返回 false 表示不处理。
func (m *Model) handlePaletteKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if !m.paletteOpen() {
		return false, nil
	}
	matches := m.paletteMatches()
	switch msg.String() {
	case "up", "down":
		if len(matches) == 0 {
			return true, nil
		}
		d := 1
		if msg.String() == "up" {
			d = -1
		}
		m.palette.sel = (m.palette.sel + d + len(matches)) % len(matches)
		m.paletteScroll()
		return true, nil
	case "esc":
		m.palette.esc = true
		m.palette.hidden = m.input.Value()
		m.syncPalette()
		return true, nil
	case "tab":
		if len(matches) > 0 {
			m.paletteComplete(matches[m.palette.sel])
		}
		return true, nil
	case "enter":
		if len(matches) == 0 {
			return false, nil // 交给原逻辑（提示未知命令）
		}
		if m.streaming {
			return true, nil
		}
		c := matches[m.palette.sel]
		if c.needArg {
			m.paletteComplete(c)
			return true, nil
		}
		m.input.Reset()
		m.fitInput()
		m.syncPalette()
		return true, m.runCommand(c.name)
	}
	return false, nil
}

func (m *Model) paletteComplete(c paletteCmd) {
	m.input.SetValue(c.name + " ")
	m.input.CursorEnd()
	m.fitInput()
	m.syncPalette()
}

// paletteView 渲染输入框正上方的命令菜单，替代工具状态行。
func (m *Model) paletteView() string {
	w := max(10, m.columnWidth())
	pad := lipgloss.NewStyle().MarginLeft(m.columnMargin())
	matches := m.paletteMatches()
	if len(matches) == 0 {
		return pad.Render(mutedStyle.Render(truncateWidth("  没有匹配的命令", w)))
	}
	nameW := 0
	for _, c := range m.paletteCommands() {
		nameW = max(nameW, lipgloss.Width(c.name))
	}
	selStyle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(textColor)
	m.paletteScroll()
	end := min(len(matches), m.palette.top+m.paletteWindow())
	lines := make([]string, 0, end-m.palette.top)
	for i := m.palette.top; i < end; i++ {
		c := matches[i]
		name := c.name + strings.Repeat(" ", nameW-lipgloss.Width(c.name))
		descW := max(0, w-2-nameW-2)
		desc := mutedStyle.Render(truncateWidth(c.desc, descW))
		var line string
		if i == m.palette.sel {
			line = selStyle.Render("› ") + selStyle.Render(name)
		} else {
			line = "  " + nameStyle.Render(name)
		}
		lines = append(lines, line+"  "+desc)
	}
	return pad.Render(strings.Join(lines, "\n"))
}
