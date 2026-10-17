package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

const (
	headerHeight     = 2 // 标题行 + 分隔线
	inputLines       = 2 // 输入框可见行数
	inputFrameHeight = 2 // 上下边框
	hintHeight       = 1 // 底部快捷键提示
)

// 颜色同时适配浅色和深色终端。
var (
	accentColor = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#A78BFA"}
	userColor   = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#7DD3FC"}
	agentColor  = lipgloss.AdaptiveColor{Light: "#047857", Dark: "#6EE7B7"}
	mutedColor  = lipgloss.AdaptiveColor{Light: "#71717A", Dark: "#8B8B93"}
	errorColor  = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
)

var (
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	userLabelStyle  = lipgloss.NewStyle().Bold(true).Foreground(userColor)
	agentLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(agentColor)
	mutedStyle      = lipgloss.NewStyle().Foreground(mutedColor)
	errorStyle      = lipgloss.NewStyle().Foreground(errorColor)
	promptStyle     = lipgloss.NewStyle().Foreground(accentColor)

	inputBoxStyle = lipgloss.NewStyle().
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
  Enter 发送 · Ctrl+J 换行 · Ctrl+R 思考过程
  PgUp/PgDn 滚动 · Esc 取消生成 · Ctrl+C 退出`

func (m *Model) view() string {
	if !m.ready {
		return "正在启动…"
	}
	if m.width < 36 || m.height < 12 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			mutedStyle.Render("终端窗口太小\n请调整到至少 36×12"))
	}

	// 依次是：标题(2 行) + 对话区 + 输入框(2+2 行) + 提示(1 行)。
	// 严格的行数控制可以避免内容超过终端高度导致界面跳动。
	return strings.Join([]string{
		m.headerView(),
		m.viewport.View(),
		m.inputView(),
		m.hintView(),
	}, "\n")
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

func (m *Model) hintView() string {
	hint := "Enter 发送 · Ctrl+J 换行 · Ctrl+R 思考 · PgUp/PgDn 滚动 · Ctrl+C 退出"
	if m.streaming {
		hint = "生成中 · Esc 取消 · PgUp/PgDn 滚动 · Ctrl+C 取消"
	}
	if lipgloss.Width(hint)+1 > m.width {
		hint = truncateWidth(hint, m.width-2)
	}
	return " " + mutedStyle.Render(hint)
}

func (m *Model) renderTranscript() string {
	var b strings.Builder
	for _, msg := range m.messages {
		b.WriteString(m.renderMessage(msg))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) renderMessage(msg chatMessage) string {
	width := m.contentWidth()
	pad := lipgloss.NewStyle().Padding(0, 2)

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

	case roleNotice:
		body := "· " + strings.ReplaceAll(msg.content, "\n", "\n  ")
		return pad.Render(mutedStyle.Width(width).Render(wrapText(body, width))) + "\n"

	case roleError:
		body := "! " + strings.ReplaceAll(msg.content, "\n", "\n  ")
		return pad.Render(errorStyle.Width(width).Render(wrapText(body, width))) + "\n"
	}
	return ""
}

// wrapText 按显示宽度换行，并使用 Unicode UAX #14 的换行机会。
//
// 为什么不能只靠 lipgloss 的 Width？因为它是按空格断词的，
// 一长串没有空格的中文会被当成一个"单词"整体移到下一行，
// 导致上一行留下大片空白。UAX #14 允许在汉字之间换行。
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

		// 单个 segment 仍然超宽（例如没有空格的英文长串），按列硬切。
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

// splitByWidth 在显示宽度 width 处切开字符串，保证不会切坏 UTF-8。
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

// contentWidth 是消息正文的宽度。外层还有左右各 2 格留白。
func (m *Model) contentWidth() int {
	return max(10, m.width-4)
}

// truncateWidth 按终端显示宽度截断，中文等宽字符算 2 列。
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
