package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// activityRight 是工具行右侧固定区：思考动画（含 esc 中断）· 工具 N；宽度不够时只留工具 N。
func (m *Model) activityRight(w, leftMin int) string {
	right := mutedStyle.Render(fmt.Sprintf("工具 %d", m.toolCount()))
	if th := m.thinkingView(); th != "" {
		full := th + mutedStyle.Render(" · ") + right
		if leftMin+lipgloss.Width(full)+1 <= w {
			return full
		}
	}
	return right
}

// joinActivity 把左侧内容（会被截断）与右侧固定区拼成恰好一行。
func joinActivity(w int, prefix, text string, textStyle lipgloss.Style, right string) string {
	room := w - lipgloss.Width(prefix) - lipgloss.Width(right) - 1
	if room < 1 {
		right, room = "", w-lipgloss.Width(prefix)
	}
	text = truncateWidth(text, max(1, room))
	gap := max(1, w-lipgloss.Width(prefix)-lipgloss.Width(text)-lipgloss.Width(right))
	if right == "" {
		gap = 0
	}
	return prefix + textStyle.Render(text) + strings.Repeat(" ", gap) + right
}

// liveActivityView：命令运行中且有实时输出时，工具行显示 peach 竖线 + 最新一行输出。
func (m *Model) liveActivityView(w int) (string, bool) {
	rows := m.activityRows()
	if len(rows) == 0 || rows[0].done || rows[0].toolLive == "" || rows[0].toolName == "task" { // task 的进度由子代理状态行显示
		return "", false
	}
	live := strings.TrimSpace(firstLine(strings.ReplaceAll(rows[0].toolLive, "\t", " ")))
	prefix := lipgloss.NewStyle().Foreground(toolColor).Render("▎") + " "
	right := m.activityRight(w, lipgloss.Width(prefix)+8)
	return joinActivity(w, prefix, live, lipgloss.NewStyle().Foreground(textColor), right), true
}
