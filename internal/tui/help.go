package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// helpState 使用独立视口，避免把帮助内容写入对话或改变对话滚动位置。
type helpState struct {
	open     bool
	viewport viewport.Model
}

func (m *Model) openHelp() {
	m.help = helpState{open: true, viewport: viewport.New()}
	m.help.viewport.MouseWheelEnabled = true
	m.layoutHelp()
	m.rewind.lastEsc = time.Time{}
}

func (m *Model) closeHelp() {
	m.help.open = false
	// 关闭帮助的 Esc 不参与 Esc Esc 回退。
	m.rewind.lastEsc = time.Time{}
}

func (m *Model) layoutHelp() {
	if !m.help.open {
		return
	}
	w := max(1, m.columnWidth()-2)
	m.help.viewport.SetWidth(w)
	m.help.viewport.SetHeight(max(1, m.height-headerHeight-2))
	m.help.viewport.SetContent(mutedStyle.Render(wrapText(helpText+"\n\n"+keymapText(), w)))
}

func (m *Model) helpKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		m.closeHelp()
		return nil
	case "ctrl+c":
		m.closeHelp()
		if m.streaming {
			m.cancelStream()
			return nil
		}
		return tea.Quit
	case "home":
		m.help.viewport.GotoTop()
		return nil
	case "end":
		m.help.viewport.GotoBottom()
		return nil
	}
	var cmd tea.Cmd
	m.help.viewport, cmd = m.help.viewport.Update(msg)
	return cmd
}

func (m *Model) helpView() string {
	pad := lipgloss.NewStyle().MarginLeft(m.columnMargin())
	title := titleStyle.Render("帮助 · /help")
	hint := mutedStyle.Render(truncateWidth("↑↓ / PgUp/PgDn 滚动 · Esc 关闭帮助", m.help.viewport.Width()))
	return strings.Join([]string{
		m.headerView(),
		pad.Render(title),
		pad.Render(m.help.viewport.View()),
		pad.Render(hint),
	}, "\n")
}
