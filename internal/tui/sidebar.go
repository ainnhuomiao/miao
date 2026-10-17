package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"miao/internal/session"
)

const (
	sidebarWidth    = 28 // 含右侧分隔线
	sidebarMinTotal = 110
)

// sidebarState 是左侧会话侧栏的状态。
type sidebarState struct {
	open    bool
	focus   bool
	sel     int // 选中的会话下标
	confirm bool
	list    []session.Meta
	now     func() time.Time
}

func (m *Model) sidebarVisible() bool {
	return m.side.open && m.width >= sidebarMinTotal
}

// sidebarCols 是侧栏占用的列数（隐藏时为 0）。
func (m *Model) sidebarCols() int {
	if m.sidebarVisible() {
		return sidebarWidth
	}
	return 0
}

func (m *Model) refreshSidebar() {
	if m.store == nil {
		m.side.list = nil
		return
	}
	list, err := m.store.List()
	if err != nil {
		return
	}
	// 当前会话还没落盘时也放在最上面
	if m.sess != nil {
		found := false
		for _, s := range list {
			if s.ID == m.sess.ID {
				found = true
				break
			}
		}
		if !found {
			list = append([]session.Meta{{ID: m.sess.ID, Title: "新会话", Updated: m.sidebarNow()}}, list...)
		}
	}
	m.side.list = list
	m.side.sel = min(max(0, m.side.sel), max(0, len(list)-1))
}

func (m *Model) sidebarNow() time.Time {
	if m.side.now != nil {
		return m.side.now()
	}
	return time.Now()
}

// toggleSidebar：Ctrl+B 打开并聚焦，再按关闭。
func (m *Model) toggleSidebar() {
	if m.side.open {
		m.side.open, m.side.focus, m.side.confirm = false, false, false
	} else {
		m.side.open, m.side.focus = true, true
		m.refreshSidebar()
		m.side.sel = 0
		for i, s := range m.side.list {
			if m.sess != nil && s.ID == m.sess.ID {
				m.side.sel = i
			}
		}
	}
	m.layout()
	m.refreshViewport(false)
}

// sidebarKey 处理侧栏聚焦时的按键；返回 false 表示交给输入框。
func (m *Model) sidebarKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if !m.side.focus || !m.sidebarVisible() {
		return false, nil
	}
	k := msg.String()
	if m.side.confirm {
		m.side.confirm = false
		if k == "y" || k == "Y" {
			m.deleteSelectedSession()
		}
		return true, nil
	}
	switch k {
	case "up", "k":
		m.side.sel = max(0, m.side.sel-1)
	case "down", "j":
		m.side.sel = min(len(m.side.list)-1, m.side.sel+1)
	case "enter":
		if s, ok := m.selectedSession(); ok && (m.sess == nil || s.ID != m.sess.ID) && !m.streaming {
			m.saveCurrentSession()
			m.loadSession(s.ID)
			m.refreshSidebar()
		}
	case "d":
		if _, ok := m.selectedSession(); ok {
			m.side.confirm = true
		}
	case "esc", "tab":
		m.side.focus = false
	case "ctrl+c":
		return false, nil
	}
	return true, nil
}

func (m *Model) selectedSession() (session.Meta, bool) {
	if m.side.sel < 0 || m.side.sel >= len(m.side.list) {
		return session.Meta{}, false
	}
	return m.side.list[m.side.sel], true
}

func (m *Model) deleteSelectedSession() {
	s, ok := m.selectedSession()
	if !ok || m.store == nil {
		return
	}
	current := m.sess != nil && s.ID == m.sess.ID
	if current && m.streaming {
		return
	}
	if err := m.store.Delete(s.ID); err != nil && err != session.ErrNotFound {
		m.notice("删除会话失败: " + err.Error())
		return
	}
	if current {
		m.beginNewSession("已删除当前会话，开始新会话。")
	}
	m.refreshSidebar()
}

// sidebarView 渲染 height 行的侧栏（含右侧 │）。
func (m *Model) sidebarView(height int) string {
	inner := sidebarWidth - 1
	sep := lipgloss.NewStyle().Foreground(surface0Color).Render("│")
	mauve := lipgloss.NewStyle().Foreground(accentColor)
	text := lipgloss.NewStyle().Foreground(textColor)

	type row struct {
		s   string
		idx int // 会话下标，组名/标题为 -1
	}
	rows := []row{{" " + titleStyle.Render("会话"), -1}}
	now := m.sidebarNow()
	lastGroup := ""
	for i, s := range m.side.list {
		g := dayGroup(s.Updated, now)
		if g != lastGroup && (g != "今天" || i > 0) {
			rows = append(rows, row{" " + mutedStyle.Render("┈ "+g), -1})
		}
		lastGroup = g

		current := m.sess != nil && s.ID == m.sess.ID
		mark := "  "
		switch {
		case m.side.focus && i == m.side.sel:
			mark = mauve.Render("›") + " "
		case current:
			mark = mauve.Render("●") + " "
		}
		stamp := sessionStamp(s.Updated, now)
		titleW := inner - 1 - 2 - lipgloss.Width(stamp) - 2
		title := s.Title
		if title == "" {
			title = "(无标题)"
		}
		var body string
		if m.side.confirm && i == m.side.sel {
			body = text.Render(truncateWidth("删除？y/n", titleW))
			stamp = ""
		} else {
			title = truncateWidth(title, titleW)
			if current {
				body = text.Render(title)
			} else {
				body = mutedStyle.Render(title)
			}
		}
		gap := max(1, inner-1-2-lipgloss.Width(body)-lipgloss.Width(stamp)-1)
		rows = append(rows, row{" " + mark + body + strings.Repeat(" ", gap) + mutedStyle.Render(stamp), i})
	}
	if len(m.side.list) == 0 {
		rows = append(rows, row{" " + mutedStyle.Render("还没有会话"), -1})
	}

	// 选中行保持在可视范围内
	start := 0
	for i, r := range rows {
		if r.idx == m.side.sel && i >= height {
			start = i - height + 1
		}
	}
	out := make([]string, 0, height)
	for i := start; i < len(rows) && len(out) < height; i++ {
		out = append(out, padRight(rows[i].s, inner)+sep)
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", inner)+sep)
	}
	return strings.Join(out, "\n")
}

func padRight(s string, w int) string {
	s = truncateWidth(s, w)
	if d := w - lipgloss.Width(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

func dayGroup(t, now time.Time) string {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	today := time.Date(y2, m2, d2, 0, 0, 0, 0, now.Location())
	day := time.Date(y1, m1, d1, 0, 0, 0, 0, now.Location())
	switch {
	case !day.Before(today):
		return "今天"
	case today.Sub(day) <= 24*time.Hour:
		return "昨天"
	}
	return "更早"
}

func sessionStamp(t, now time.Time) string {
	if dayGroup(t, now) == "更早" {
		return fmt.Sprintf("%02d-%02d", t.Month(), t.Day())
	}
	return t.Format("15:04")
}

// withSidebar 把侧栏拼到顶栏以下的主体左边。
// side 是 view() 开头算好的侧栏列数，此时 m.width 已是剩余宽度。
func (m *Model) withSidebar(side int, header, body string) string {
	if side == 0 {
		return header + "\n" + body
	}
	h := lipgloss.Height(body)
	body = lipgloss.NewStyle().MaxWidth(m.width).Render(body)
	return header + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, m.sidebarView(h), body)
}
