package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ainnhuomiao/miao/internal/session"
)

const (
	sidebarWidth    = 28 // 含右侧分隔线
	sidebarMinTotal = 110
)

// sidebarState 是左侧文件树侧栏的状态；默认隐藏。
type sidebarState struct {
	open  bool
	focus bool
	tree  treeState
	now   func() time.Time
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

// refreshSidebar 在文件可能变化后（回合结束、切换会话、回退）刷新文件树。
func (m *Model) refreshSidebar() {
	if m.side.open {
		m.refreshTree()
	}
}

func (m *Model) sidebarNow() time.Time {
	if m.side.now != nil {
		return m.side.now()
	}
	return time.Now()
}

// toggleSidebar：Ctrl+B 打开文件树并聚焦，再按关闭。
func (m *Model) toggleSidebar() {
	if m.side.open {
		m.side.open, m.side.focus, m.side.tree.confirm = false, false, ""
		m.side.tree.preview = nil
	} else {
		m.side.open, m.side.focus = true, true
		m.side.tree.sel = 0
		m.refreshTree()
	}
	m.layout()
	m.refreshViewport(false)
}

// sidebarKey 处理侧栏聚焦时的按键；返回 false 表示交给输入框。
func (m *Model) sidebarKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if !m.side.focus || !m.sidebarVisible() {
		return false, nil
	}
	switch k := msg.String(); k {
	case "esc", "tab":
		if m.side.tree.confirm != "" {
			m.side.tree.confirm = ""
			return true, nil
		}
		if k == "esc" && m.side.tree.preview != nil {
			m.closeTreePreview()
			return true, nil
		}
		m.side.focus = false
		return true, nil
	case "ctrl+c", "ctrl+t", "pgup", "pgdown":
		return false, nil
	default:
		return true, m.treeKey(k)
	}
}

// deleteSession 删除一个会话；删的是当前会话时开新会话。
func (m *Model) deleteSession(s session.Meta) {
	if m.store == nil {
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
}

// sidebarView 渲染 height 行的侧栏（含右侧 │）。
func (m *Model) sidebarView(height int) string {
	inner := sidebarWidth - 1
	sep := lipgloss.NewStyle().Foreground(surface0Color).Render("│")
	lines := m.treeView(inner, height)
	for i := range lines {
		lines[i] += sep
	}
	return strings.Join(lines, "\n")
}

func padRight(s string, w int) string {
	s = truncateWidth(s, w)
	if d := w - lipgloss.Width(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
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
