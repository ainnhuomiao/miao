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
	needArg bool   // 需要参数：Enter 等同补全
	swatch  string // 名字后的色块（/theme 菜单）
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
	return append(m.builtinPalette(), m.userPalette()...)
}

// builtinPalette 是内置命令；自定义命令见 userPalette。
func (m *Model) builtinPalette() []paletteCmd {
	reasoning := "收起"
	if m.showReasoning {
		reasoning = "展开"
	}
	return []paletteCmd{
		{name: "/help", desc: "显示帮助"},
		{name: "/new", desc: "新建会话"},
		{name: "/sessions", desc: "切换会话"},
		{name: "/clear", desc: "清空对话并换新会话"},
		{name: "/undo", desc: "撤销最近一次文件修改"},
		{name: "/compact", desc: "压缩上下文：把早期对话总结成摘要"},
		{name: "/diff", desc: "查看本次会话改过的文件和 diff"},
		{name: "/commit", desc: "让模型写提交信息，确认后提交本会话改过的文件"},
		{name: "/fork", desc: "把当前对话复制成新会话（原会话保留）"},
		{name: "/model", desc: "切换模型（当前：" + m.modelName + "）", needArg: true},
		{name: "/theme", desc: "切换主题（当前：" + m.savedTheme() + "）", needArg: true},
		{name: "/subagents", desc: "子代理设置：开关、并发上限、模型", needArg: true},
		{name: "/mcp", desc: "查看 MCP 服务器状态与工具"},
		{name: "/hooks", desc: "查看已配置的 hook"},
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
	if _, ok := m.atMode(); ok {
		return !(m.palette.esc && v == m.palette.hidden)
	}
	if m.subagentsMode() {
		return !(m.palette.esc && v == m.palette.hidden)
	}
	if _, ok := m.themeMode(); ok {
		return !(m.palette.esc && v == m.palette.hidden)
	}
	if _, ok := m.modelMode(); !ok && (!strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \t\n")) {
		return false
	}
	if m.palette.esc && v == m.palette.hidden {
		return false
	}
	return true
}

// paletteMatches 按前缀优先、子序列其次筛选命令。
func (m *Model) paletteMatches() []paletteCmd {
	if q, ok := m.atMode(); ok {
		return m.atMatches(q)
	}
	if m.subagentsMode() {
		return m.subagentsMatches()
	}
	if q, ok := m.modelMode(); ok {
		return m.modelMatches(q)
	}
	if q, ok := m.themeMode(); ok {
		return m.themeMatches(q)
	}
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
	if _, ok := m.themeMode(); !ok && m.themeOrig != "" {
		m.cancelThemePreview()
	}
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
	if ok, cmd := m.handleSubagentsKey(msg, matches); ok {
		return true, cmd
	}
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
		if _, ok := m.themeMode(); ok {
			m.previewTheme(matches[m.palette.sel].name)
		}
		return true, nil
	case "esc":
		if _, ok := m.themeMode(); ok {
			m.cancelThemePreview()
		}
		m.palette.esc = true
		m.palette.hidden = m.input.Value()
		m.syncPalette()
		return true, nil
	case "tab", "enter":
		if _, ok := m.atMode(); ok {
			if len(matches) == 0 {
				return msg.String() == "tab", nil // 没匹配时 Enter 照常发送
			}
			m.atComplete(matches[m.palette.sel])
			return true, nil
		}
		if msg.String() == "enter" {
			break
		}
		if len(matches) > 0 {
			c := matches[m.palette.sel]
			m.paletteComplete(c)
			if c.name == "/model" {
				return true, m.fetchModelsCmd()
			}
		}
		return true, nil
	}
	switch msg.String() {
	case "enter":
		if len(matches) == 0 {
			return false, nil // 交给原逻辑（提示未知命令）
		}
		if _, ok := m.themeMode(); ok {
			name := matches[m.palette.sel].name
			m.input.Reset()
			m.fitInput()
			m.applyTheme(name)
			m.syncPalette()
			return true, nil
		}
		_, inModel := m.modelMode()
		if m.streaming {
			if inModel {
				m.notice("正在生成回复，暂不能切换模型。")
			}
			return true, nil
		}
		c := matches[m.palette.sel]
		if inModel {
			m.input.Reset()
			m.fitInput()
			m.syncPalette()
			m.switchModel(c.name)
			return true, nil
		}
		if c.name == "/model" {
			m.paletteComplete(c)
			return true, m.fetchModelsCmd()
		}
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
	if _, ok := m.themeMode(); ok {
		m.input.SetValue("/theme " + c.name)
		m.input.CursorEnd()
		m.fitInput()
		m.syncPalette()
		m.previewTheme(c.name)
		return
	}
	if _, ok := m.modelMode(); ok {
		m.input.SetValue("/model " + c.name)
		m.input.CursorEnd()
		m.fitInput()
		m.syncPalette()
		return
	}
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
		empty := "  没有匹配的命令"
		if _, ok := m.atMode(); ok {
			empty = "  没有匹配的文件"
		} else if _, ok := m.modelMode(); ok {
			empty = "  没有匹配的模型（Enter 直接切换到输入的名字）"
		}
		return pad.Render(mutedStyle.Render(truncateWidth(empty, w)))
	}
	nameW := 0
	all := m.paletteCommands()
	if _, ok := m.modelMode(); ok {
		all = matches
	} else if _, ok := m.themeMode(); ok || m.subagentsMode() {
		all = matches
	} else if _, ok := m.atMode(); ok {
		all = matches[m.palette.top:min(len(matches), m.palette.top+m.paletteWindow())]
	}
	for _, c := range all {
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
		if c.swatch != "" {
			sw := lipgloss.Width(c.swatch) + 1
			desc = c.swatch + " " + mutedStyle.Render(truncateWidth(c.desc, max(0, descW-sw)))
		}
		var line string
		if i == m.palette.sel {
			line = selStyle.Render("› ") + selStyle.Render(name)
		} else {
			line = "  " + nameStyle.Render(name)
		}
		line += "  " + desc
		if i == end-1 && end < len(matches) { // 下面还有命令：最后一行右侧灰字 ↓
			line = truncateWidth(line, w-2)
			line += strings.Repeat(" ", max(1, w-1-lipgloss.Width(line))) + mutedStyle.Render("↓")
		}
		lines = append(lines, line)
	}
	return pad.Render(strings.Join(lines, "\n"))
}
