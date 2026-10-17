package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

const (
	headerHeight       = 2
	inputLines         = 2
	inputFrameHeight   = 2
	hintHeight         = 1
	approvalCardHeight = 5 // 边框2 + 标题/命令/提示 3
)

var (
	accentColor = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#A78BFA"}
	userColor   = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#7DD3FC"}
	agentColor  = lipgloss.AdaptiveColor{Light: "#047857", Dark: "#6EE7B7"}
	toolColor   = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	mutedColor  = lipgloss.AdaptiveColor{Light: "#71717A", Dark: "#8B8B93"}
	errorColor  = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
)

var (
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	userLabelStyle  = lipgloss.NewStyle().Bold(true).Foreground(userColor)
	agentLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(agentColor)
	toolLabelStyle  = lipgloss.NewStyle().Bold(true).Foreground(toolColor)
	mutedStyle      = lipgloss.NewStyle().Foreground(mutedColor)
	errorStyle      = lipgloss.NewStyle().Foreground(errorColor)
	promptStyle     = lipgloss.NewStyle().Foreground(accentColor)

	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Padding(0, 1)

	approvalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Padding(0, 1)

)

const helpText = `命令:
  /help       显示帮助
  /clear      清空对话历史
  /reasoning  展开或收起思考过程
  /exit       退出

快捷键:
  Enter 发送 · Ctrl+J 换行 · Tab 工具抽屉 · Ctrl+R 思考过程
  PgUp/PgDn 滚动 · Esc 关抽屉/取消审批/取消生成 · Ctrl+C 取消或退出
  名单外 run_command：Y 允许 · N/Esc 拒绝
  [ / ] 左侧会话（一期预留）`

func (m *Model) view() string {
	if !m.ready {
		return "正在启动…"
	}
	if m.width < 36 || m.height < 12 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			mutedStyle.Render("终端窗口太小\n请调整到至少 36×12"))
	}

	main := m.viewport.View()
	if m.drawerOpen {
		sep := mutedStyle.Render("│")
		main = lipgloss.JoinHorizontal(lipgloss.Top, main, sep, m.drawerViewport.View())
	}

	parts := []string{m.headerView(), main}
	if m.approval != nil {
		parts = append(parts, m.approvalView())
	}
	parts = append(parts, m.inputView(), m.hintView())
	return strings.Join(parts, "\n")
}

func (m *Model) headerView() string {
	right := m.statusView()
	left := " " + titleStyle.Render("GoAgent")

	suffix := " · " + m.modelName
	if lipgloss.Width(left)+lipgloss.Width(suffix)+lipgloss.Width(right)+2 <= m.width {
		left += mutedStyle.Render(suffix)
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	rule := " " + mutedStyle.Render(strings.Repeat("─", max(0, m.width-2)))

	return line + "\n" + rule
}

func (m *Model) statusView() string {
	if m.streaming {
		label := "生成中"
		if m.lastAssistantContent() == "" {
			label = "思考中"
		}
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].role == roleTool && !m.messages[i].done {
				label = "工具中"
				break
			}
		}
		return m.spinner.View() + mutedStyle.Render(label)
	}
	return agentLabelStyle.Render("●") + mutedStyle.Render(" 就绪")
}

func (m *Model) lastAssistantContent() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == roleAssistant {
			return m.messages[i].content
		}
	}
	return ""
}

func (m *Model) inputView() string {
	return inputBoxStyle.Render(m.input.View())
}

func (m *Model) approvalView() string {
	if m.approval == nil {
		return ""
	}
	innerW := max(20, m.width-6)
	title := "确认执行 " + m.approval.toolName
	cmd := truncateWidth(m.approval.display, innerW)
	hint := "Y 允许 · N 拒绝 · Esc 取消"
	body := titleStyle.Render(title) + "\n" + cmd + "\n" + mutedStyle.Render(hint)
	return approvalBoxStyle.Width(max(24, m.width-2)).Render(body)
}

func (m *Model) hintView() string {
	hint := "Enter 发送 · Ctrl+J 换行 · Tab 抽屉 · Ctrl+R 思考 · Ctrl+C 退出"
	if m.drawerOpen {
		hint = "Tab 关抽屉 · Esc 关抽屉 · PgUp/PgDn 滚动 · Ctrl+C 退出"
	}
	if m.streaming {
		hint = "生成中 · Esc 取消 · Tab 抽屉 · PgUp/PgDn · Ctrl+C 取消"
	}
	if m.approval != nil {
		hint = "确认命令 · Y 允许 · N 拒绝 · Esc 取消"
	}
	if lipgloss.Width(hint)+1 > m.width {
		hint = truncateWidth(hint, m.width-2)
	}
	return " " + mutedStyle.Render(hint)
}

func (m *Model) renderTranscript() string {
	var b strings.Builder
	for _, msg := range m.messages {
		// 跳过尚未有任何内容的空助手气泡（工具后预留的）
		if msg.role == roleAssistant && msg.content == "" && msg.reasoning == "" && msg.done {
			continue
		}
		if msg.role == roleAssistant && msg.content == "" && msg.reasoning == "" && !msg.done {
			// 流式中的空气泡仍显示占位
			if m.streaming {
				b.WriteString(m.renderMessage(msg))
			}
			continue
		}
		b.WriteString(m.renderMessage(msg))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) renderMessage(msg chatMessage) string {
	width := m.contentWidth()
	pad := lipgloss.NewStyle().Padding(0, 1)

	switch msg.role {
	case roleUser:
		body := userLabelStyle.Render("▌ 你") + "\n" +
			lipgloss.NewStyle().Width(width).Render(wrapText(msg.content, width))
		return pad.Render(body) + "\n\n"

	case roleAssistant:
		var b strings.Builder
		b.WriteString(agentLabelStyle.Render("▌ GoAgent"))
		b.WriteString("\n")

		if msg.reasoning != "" {
			if m.showReasoning {
				b.WriteString(mutedStyle.Width(width).Render(wrapText(msg.reasoning, width)))
			} else {
				b.WriteString(mutedStyle.Render(fmt.Sprintf(
					"思考 %d 字 · Ctrl+R 展开", utf8.RuneCountInString(msg.reasoning))))
			}
			b.WriteString("\n")
		}

		switch {
		case msg.content != "":
			b.WriteString(lipgloss.NewStyle().Width(width).Render(wrapText(msg.content, width)))
		case !msg.done:
			b.WriteString(mutedStyle.Render("…"))
		default:
			b.WriteString(mutedStyle.Render("(无内容)"))
		}
		return pad.Render(b.String()) + "\n\n"

	case roleTool:
		// 默认一行：⚙ name · ✓ 12ms · 摘要；详情留给抽屉。
		name := "⚙ " + msg.toolName
		var summary string
		switch {
		case !msg.done:
			summary = "运行中…"
		case msg.toolError:
			summary = "✗"
			if preview := compact(msg.toolResult, 40); preview != "" {
				summary += " · " + preview
			}
		default:
			summary = fmt.Sprintf("✓ %s", msg.toolDur.Round(time.Millisecond))
			if preview := compact(msg.toolResult, 48); preview != "" {
				summary += " · " + preview
			}
		}
		// 先按纯文本截断，再上色，避免 ANSI 干扰换行宽度。
		plain := name + " · " + summary
		if lipgloss.Width(plain) > width {
			plain = truncateWidth(plain, width)
			// 截断后重新拆：尽量保留工具名着色
			if i := strings.Index(plain, " · "); i >= 0 {
				name, summary = plain[:i], plain[i+len(" · "):]
			} else {
				name, summary = plain, ""
			}
		}
		suffix := ""
		if summary != "" {
			if msg.done && msg.toolError {
				suffix = errorStyle.Render(" · " + summary)
			} else {
				suffix = mutedStyle.Render(" · " + summary)
			}
		}
		return pad.Render(toolLabelStyle.Render(name)+suffix) + "\n\n"

	case roleNotice:
		body := "· " + strings.ReplaceAll(msg.content, "\n", "\n  ")
		return pad.Render(mutedStyle.Width(width).Render(wrapText(body, width))) + "\n"

	case roleError:
		body := "! " + strings.ReplaceAll(msg.content, "\n", "\n  ")
		return pad.Render(errorStyle.Width(width).Render(wrapText(body, width))) + "\n"
	}
	return ""
}

func wrapText(s string, width int) string {
	if width <= 0 || s == "" {
		return s
	}
	var out strings.Builder
	paragraphs := strings.Split(s, "\n")
	for i, p := range paragraphs {
		if i > 0 {
			out.WriteByte('\n')
		}
		wrapParagraph(&out, p, width)
	}
	return out.String()
}

func wrapParagraph(out *strings.Builder, p string, width int) {
	rest := p
	state := -1
	col := 0

	for len(rest) > 0 {
		seg, next, mustBreak, newState := uniseg.FirstLineSegmentInString(rest, state)
		state = newState
		segWidth := uniseg.StringWidth(seg)

		if col > 0 && col+segWidth > width {
			out.WriteByte('\n')
			col = 0
		}

		for segWidth > width {
			head, tail := splitByWidth(seg, width)
			out.WriteString(head)
			out.WriteByte('\n')
			seg, segWidth = tail, uniseg.StringWidth(tail)
			col = 0
		}

		out.WriteString(seg)
		col += segWidth

		if mustBreak && len(next) > 0 {
			out.WriteByte('\n')
			col = 0
			state = -1
		}
		rest = next
	}
}

func splitByWidth(s string, width int) (head, tail string) {
	used := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if i > 0 && used+rw > width {
			return s[:i], s[i:]
		}
		used += rw
	}
	return s, ""
}

func (m *Model) contentWidth() int {
	w := m.viewport.Width
	if w <= 0 {
		w = m.width
	}
	return max(10, w-4)
}

func truncateWidth(s string, width int) string {
	if width <= 1 || lipgloss.Width(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if used+rw > width-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString("…")
	return b.String()
}
