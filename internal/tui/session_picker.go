package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ainnhuomiao/miao/internal/session"
)

const pickerMaxRows = 8

// pickerState 是 /sessions 弹出的会话选择列表。
type pickerState struct {
	open       bool
	sel, top   int
	query      string
	all        []session.Meta
	confirmDel bool
	confirmInt bool // 正在生成时切换需要再确认
}

func (m *Model) openSessionPicker() {
	if m.store == nil {
		m.notice(m.sessionStorageNotice())
		return
	}
	list, err := m.store.List()
	if err != nil {
		m.notice("列出会话失败: " + err.Error())
		return
	}
	m.picker = pickerState{open: true, all: list}
	if len(m.picker.all) == 0 {
		m.picker.open = false
		m.notice("暂无已保存会话。")
	}
	m.layout()
}

func (m *Model) closeSessionPicker() {
	m.picker = pickerState{}
	m.layout()
}

func (m *Model) pickerMatches() []session.Meta {
	q := strings.ToLower(strings.TrimSpace(m.picker.query))
	if q == "" {
		return m.picker.all
	}
	var out []session.Meta
	for _, s := range m.picker.all {
		if strings.Contains(strings.ToLower(pickerTitle(s)), q) {
			out = append(out, s)
		}
	}
	return out
}

func pickerTitle(s session.Meta) string {
	t, _, _ := strings.Cut(strings.TrimSpace(s.Title), "\n")
	if t == "" {
		return "（无标题）"
	}
	return t
}

// sessionPickerKey 处理列表打开时的按键；返回 false 表示不归它管。
func (m *Model) sessionPickerKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if !m.picker.open {
		return false, nil
	}
	defer func() {
		if m.picker.open {
			m.layout()
		}
	}()
	k := msg.String()
	list := m.pickerMatches()
	if m.picker.confirmDel {
		m.picker.confirmDel = false
		if k == "ctrl+d" && m.picker.sel < len(list) {
			m.deleteSession(list[m.picker.sel])
			if list, err := m.store.List(); err == nil {
				m.picker.all = list
			}
			m.picker.sel = min(m.picker.sel, max(0, len(m.pickerMatches())-1))
		}
		return true, nil
	}
	if m.picker.confirmInt && k != "enter" {
		m.picker.confirmInt = false
	}
	switch k {
	case "ctrl+c":
		return false, nil
	case "esc":
		m.closeSessionPicker()
	case "up":
		m.picker.sel = max(0, m.picker.sel-1)
	case "down":
		m.picker.sel = min(len(list)-1, m.picker.sel+1)
	case "enter":
		if m.picker.sel >= len(list) {
			return true, nil
		}
		s := list[m.picker.sel]
		if m.sess != nil && s.ID == m.sess.ID {
			m.closeSessionPicker()
			return true, nil
		}
		if m.streaming {
			if !m.picker.confirmInt {
				m.picker.confirmInt = true
				return true, nil
			}
			m.cancelStream()
			m.picker.confirmInt = false
			m.notice("已中断当前回复，再按 Enter 切换。")
			return true, nil
		}
		m.saveCurrentSession()
		m.loadSession(s.ID)
		m.refreshSidebar()
		m.closeSessionPicker()
	case "backspace":
		if r := []rune(m.picker.query); len(r) > 0 {
			m.picker.query = string(r[:len(r)-1])
			m.picker.sel, m.picker.top = 0, 0
		}
	case "ctrl+d":
		if len(list) > 0 {
			m.picker.confirmDel = true
		}
	default:
		if msg.Text != "" {
			m.picker.query += msg.Text
			m.picker.sel, m.picker.top = 0, 0
		}
	}
	return true, nil
}

func relTime(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}

func (m *Model) sessionPickerView() string {
	w := max(10, m.columnWidth())
	pad := lipgloss.NewStyle().MarginLeft(m.columnMargin())
	sel := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	text := lipgloss.NewStyle().Foreground(textColor)
	dot := lipgloss.NewStyle().Foreground(accentColor)
	list := m.pickerMatches()

	head := "切换会话 · ↑↓ 选择 · Enter 切换 · 打字筛选 · Ctrl+D 删除 · Esc 关闭"
	switch {
	case m.picker.confirmDel:
		head = "再按一次 Ctrl+D 删除这个会话，其他键取消"
	case m.picker.confirmInt:
		head = "当前回复会中断，再按 Enter 确认"
	case m.picker.query != "":
		head = "筛选：" + m.picker.query
	}
	lines := []string{" " + mutedStyle.Render(truncateWidth(head, w-2))}
	if len(list) == 0 {
		lines = append(lines, "   "+mutedStyle.Render("没有匹配的会话"))
		return pad.Render(strings.Join(lines, "\n"))
	}
	if m.picker.sel < m.picker.top {
		m.picker.top = m.picker.sel
	}
	if m.picker.sel >= m.picker.top+pickerMaxRows {
		m.picker.top = m.picker.sel - pickerMaxRows + 1
	}
	now := m.sidebarNow()
	end := min(len(list), m.picker.top+pickerMaxRows)
	for i := m.picker.top; i < end; i++ {
		s := list[i]
		meta := relTime(s.Updated, now)
		if s.MsgCount > 0 {
			meta += fmt.Sprintf(" · %d 条", s.MsgCount)
		}
		cur := ""
		if m.sess != nil && s.ID == m.sess.ID {
			cur = " " + dot.Render("●")
		}
		title := truncateWidth(pickerTitle(s), max(4, w-6-lipgloss.Width(meta)-lipgloss.Width(cur)))
		var line string
		if i == m.picker.sel {
			line = " " + sel.Render("›") + " " + sel.Render(title)
		} else {
			line = "   " + text.Render(title)
		}
		line += cur + "  " + mutedStyle.Render(meta)
		if i == end-1 && end < len(list) {
			line += strings.Repeat(" ", max(1, w-1-lipgloss.Width(line))) + mutedStyle.Render("↓")
		}
		lines = append(lines, line)
	}
	lines = append(lines, " "+mutedStyle.Render("Ctrl+D 删除 · Esc 关闭"))
	return pad.Render(strings.Join(lines, "\n"))
}

// pickerHeight 是会话列表占的行数（标题行 + 最多 8 行）。
func (m *Model) pickerHeight() int {
	if !m.picker.open {
		return 0
	}
	n := len(m.pickerMatches())
	if n == 0 {
		return 2
	}
	return 2 + min(pickerMaxRows, n) // 标题行 + 列表 + 底部提示
}
