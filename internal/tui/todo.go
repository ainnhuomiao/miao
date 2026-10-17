package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"agnet/internal/tools"
)

// todoPanelMaxLines 是 Ctrl+T 清单面板最多显示的行数（含溢出提示）。
const todoPanelMaxLines = 10

// currentTodos 从界面消息里取最近一次成功的 todo_write 清单。
// 清单随消息走：/new、/clear 清空消息即清空清单；/load 从会话历史还原。
func (m *Model) currentTodos() []tools.TodoItem {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.role != roleTool || msg.toolName != "todo_write" || !msg.done || msg.toolError {
			continue
		}
		items, err := tools.ParseTodos([]byte(msg.toolArgs))
		if err != nil {
			continue
		}
		return items
	}
	return nil
}

func todoCounts(items []tools.TodoItem) (done int, focus string) {
	for _, t := range items {
		if t.Status == tools.TodoCompleted {
			done++
		}
	}
	for _, t := range items {
		if t.Status == tools.TodoInProgress {
			return done, t.Content
		}
	}
	for _, t := range items {
		if t.Status == tools.TodoPending {
			return done, t.Content
		}
	}
	return done, ""
}

// todoActivityView 在工具行显示清单进度「☐ 3/5 写测试」。
// 有工具正在执行时让位给工具；全部完成仅在最近一次工具就是 todo_write 时短暂显示「☑ 5/5 完成」。
func (m *Model) todoActivityView(w int) (string, bool) {
	items := m.currentTodos()
	if len(items) == 0 {
		return "", false
	}
	rows := m.activityRows()
	if len(rows) > 0 && !rows[0].done {
		return "", false
	}
	done, focus := todoCounts(items)
	allDone := done == len(items)
	if allDone && (len(rows) == 0 || rows[0].toolName != "todo_write") {
		return "", false
	}
	bar := lipgloss.NewStyle().Foreground(toolColor).Render("▏")
	if allDone {
		prefix := bar + mutedStyle.Render(fmt.Sprintf("☑ %d/%d", done, len(items))) + " "
		return joinActivity(w, prefix, "完成", mutedStyle, m.activityRight(w, lipgloss.Width(prefix)+4)), true
	}
	prefix := bar + lipgloss.NewStyle().Foreground(accentColor).Render(fmt.Sprintf("☐ %d/%d", done, len(items))) + " "
	right := m.activityRight(w, lipgloss.Width(prefix)+8)
	return joinActivity(w, prefix, focus, lipgloss.NewStyle().Foreground(textColor), right), true
}

// toggleTodoPanel 响应 Ctrl+T；无清单时给出灰色提示。
func (m *Model) toggleTodoPanel() {
	if len(m.currentTodos()) == 0 && !m.subagentsRunning() && len(m.finishedTasks()) == 0 {
		m.todoPanel = false
		m.notice("当前没有任务清单。")
		m.layout()
		return
	}
	m.todoPanel = !m.todoPanel
	m.layout()
}

func (m *Model) todoPanelVisible() bool {
	return m.todoPanel && !m.paletteOpen() && m.approval == nil && (len(m.currentTodos()) > 0 || m.subagentsRunning() || len(m.finishedTasks()) > 0)
}

// todoPanelHeight 是面板占用的行数（未显示为 0）。
func (m *Model) todoPanelHeight() int {
	if !m.todoPanelVisible() {
		return 0
	}
	return len(m.todoPanelLines())
}

func (m *Model) todoPanelLines() []string {
	items := m.currentTodos()
	w := max(10, m.columnWidth())
	var lines []string
	for _, t := range items {
		var mark string
		var st lipgloss.Style
		switch t.Status {
		case tools.TodoCompleted:
			mark, st = "☑ ", mutedStyle.Strikethrough(true)
		case tools.TodoInProgress:
			mark, st = "▶ ", lipgloss.NewStyle().Foreground(accentColor).Bold(true)
		default:
			mark, st = "☐ ", lipgloss.NewStyle().Foreground(textColor)
		}
		lines = append(lines, " "+st.Render(truncateWidth(mark+t.Content, w-2)))
	}
	if len(lines) > todoPanelMaxLines {
		more := len(lines) - (todoPanelMaxLines - 1)
		lines = append(lines[:todoPanelMaxLines-1], mutedStyle.Render(fmt.Sprintf(" … 还有 %d 项", more)))
	}
	lines = append(lines, m.subagentPanelLines(w)...)
	lines = append(lines, m.taskDetailLines(w)...)
	return lines
}

// todoPanelView 渲染 Ctrl+T 展开的完整清单（输入框上方）。
func (m *Model) todoPanelView() string {
	if !m.todoPanelVisible() {
		return ""
	}
	return lipgloss.NewStyle().MarginLeft(m.columnMargin()).Render(strings.Join(m.todoPanelLines(), "\n"))
}
