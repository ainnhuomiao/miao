package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"miao/internal/subagent"
)

// isTaskTool 判断是否为子代理 task 调用。
func isTaskTool(name string) bool { return name == subagent.ToolName }

// taskLineCount 是结果的行数（空结果为 0）。
func taskLineCount(result string) int {
	result = strings.TrimRight(result, "\n")
	if result == "" {
		return 0
	}
	return strings.Count(result, "\n") + 1
}

// taskMeta 是 task 行名称后面的部分（不含图标）：15.1s · 32 行 / 3.2s · 错误。
func taskMeta(msg chatMessage) string {
	el := fmt.Sprintf("%.1fs", msg.toolDur.Seconds())
	if msg.toolError {
		if e := strings.TrimSpace(firstLine(msg.toolResult)); e != "" {
			return el + " · " + e
		}
		return el
	}
	return fmt.Sprintf("%s · %d 行", el, taskLineCount(msg.toolResult))
}

// taskRow 拼成「⑂ 名称 · 状态 · 耗时 · 行数」，按显示宽度截断名称，整行不超过 width。
// status 是已渲染好的状态图标（✓ / ✗ / spinner）。
func taskRow(msg chatMessage, status string, width int) string {
	name := subagentDesc(msg.toolArgs)
	sep := mutedStyle.Render(" · ")
	head := "⑂ "
	tail := ""
	if msg.done {
		st := mutedStyle
		if msg.toolError {
			st = errorStyle
		}
		tail = st.Render(taskMeta(msg))
	} else {
		tail = mutedStyle.Render("运行中")
	}
	fixed := lipgloss.Width(head) + lipgloss.Width(sep) + 1 + lipgloss.Width(status) + lipgloss.Width(tail)
	room := width - fixed
	if room < 2 { // 太窄：只留名称与状态，再截断
		line := truncateWidth(head+name, max(1, width-lipgloss.Width(status)-3))
		return toolLabelStyle.Render(line) + sep + status
	}
	name = truncateWidth(name, room)
	out := toolLabelStyle.Render(head+name) + sep + status + " " + tail
	if lipgloss.Width(out) > width { // 失败信息过长时截断尾部
		plainTail := truncateWidth(taskMeta(msg), max(1, width-lipgloss.Width(head+name)-lipgloss.Width(sep)-1-lipgloss.Width(status)))
		out = toolLabelStyle.Render(head+name) + sep + status + " " + errorStyle.Render(plainTail)
	}
	return out
}

// taskStatus 返回 task 的状态图标（完成 ✓ / 失败 ✗ / 运行中 spinner）。
func (m *Model) taskStatus(msg chatMessage) string {
	switch {
	case !msg.done:
		sp := m.spinner
		sp.Style = lipgloss.NewStyle().Foreground(toolColor)
		return sp.View()
	case msg.toolError:
		return lipgloss.NewStyle().Foreground(errorColor).Render("✗")
	}
	return lipgloss.NewStyle().Foreground(agentColor).Render("✓")
}

// taskCardLine 是主区里 task 调用的一行。
func (m *Model) taskCardLine(msg chatMessage, width int) string {
	return taskRow(msg, m.taskStatus(msg), width)
}

// taskActivityView：最新工具是已结束的 task 时，工具行用同样的格式替换原始参数。
func (m *Model) taskActivityView(w int) (string, bool) {
	rows := m.activityRows()
	if len(rows) == 0 || !isTaskTool(rows[0].toolName) || m.subagentsRunning() {
		return "", false
	}
	prefix := lipgloss.NewStyle().Foreground(toolColor).Render("▏")
	right := m.activityRight(w, lipgloss.Width(prefix)+12)
	room := max(1, w-lipgloss.Width(prefix)-lipgloss.Width(right)-1)
	row := taskRow(rows[0], m.taskStatus(rows[0]), room)
	gap := max(1, w-lipgloss.Width(prefix)-lipgloss.Width(row)-lipgloss.Width(right))
	return prefix + row + strings.Repeat(" ", gap) + right, true
}

// taskPrompt 解析 task 调用的 prompt。
func taskPrompt(args string) string {
	var in struct {
		Prompt string `json:"prompt"`
	}
	_ = json.Unmarshal([]byte(args), &in)
	return strings.TrimSpace(in.Prompt)
}

// finishedTasks 是本轮（最后一条用户消息之后）已结束的 task 调用。
func (m *Model) finishedTasks() []chatMessage {
	var out []chatMessage
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.role == roleUser {
			break
		}
		if msg.role == roleTool && isTaskTool(msg.toolName) && msg.done {
			out = append([]chatMessage{msg}, out...)
		}
	}
	return out
}

const (
	taskDetailPromptLines = 4
	taskDetailResultLines = 12
)

// taskDetailLines 是 Ctrl+T 面板里已完成子代理的详情：名称行 + prompt + 结果。
func (m *Model) taskDetailLines(w int) []string {
	tasks := m.finishedTasks()
	if len(tasks) == 0 {
		return nil
	}
	head := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	text := lipgloss.NewStyle().Foreground(textColor)
	lines := []string{" " + head.Render(fmt.Sprintf("⑂ 子代理结果（%d 个）", len(tasks)))}
	clip := func(s string, n int, st lipgloss.Style, label string) {
		ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
		for k, l := range ls {
			if k == n {
				lines = append(lines, "     "+mutedStyle.Render(fmt.Sprintf("…还有 %d 行", len(ls)-n)))
				break
			}
			p := "     "
			if k == 0 {
				p = "   " + label
			}
			lines = append(lines, p+st.Render(truncateWidth(strings.ReplaceAll(l, "\t", " "), max(1, w-6))))
		}
	}
	for _, t := range tasks {
		lines = append(lines, "  "+taskRow(t, m.taskStatus(t), max(4, w-3)))
		if p := taskPrompt(t.toolArgs); p != "" {
			clip(p, taskDetailPromptLines, mutedStyle, mutedStyle.Render("问 "))
		}
		if t.toolResult != "" {
			st := text
			if t.toolError {
				st = errorStyle
			}
			clip(t.toolResult, taskDetailResultLines, st, mutedStyle.Render("答 "))
		}
	}
	return lines
}
