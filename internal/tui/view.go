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
	headerHeight     = 2
	inputLines       = 2
	inputFrameHeight = 2
	hintHeight       = 1
	approvalCardBase = 4 // 边框2 + 标题1 + 提示1，另加 display 行数
	approvalMaxLines = 6
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

	drawerBorderStyle = lipgloss.NewStyle().Foreground(toolColor)
	drawerBoxStyle    = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(toolColor).
				Padding(0, 1)

	userTagStyle  = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("#0B1020")).Background(userColor)
	agentTagStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("#0B1020")).Background(agentColor)

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
		main = lipgloss.JoinHorizontal(lipgloss.Top, main, m.drawerBoxView())
	}

	parts := []string{m.headerView(), main}
	if m.approval != nil {
		parts = append(parts, m.approvalView())
	}
	parts = append(parts, m.inputView(), m.hintView())
	return strings.Join(parts, "\n")
}

func (m *Model) drawerBoxView() string {
	w := m.drawerViewport.Width + 2
	h := m.drawerViewport.Height
	box := drawerBoxStyle.Width(w).Height(h).Render(m.drawerViewport.View())
	// 把标题嵌进上边框：╭ 工具 · 1 ───╮
	lines := strings.Split(box, "\n")
	if len(lines) > 0 {
		title := m.drawerTitle()
		fill := w - lipgloss.Width(title)
		if fill >= 1 {
			lines[0] = drawerBorderStyle.Render("╭─") + titleStyle.Render(title) +
				drawerBorderStyle.Render(strings.Repeat("─", fill-1)+"╮")
		}
	}
	return strings.Join(lines, "\n")
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
	title := truncateWidth(m.approval.title(), innerW)
	var lines []string
	for _, l := range m.approval.displayLines() {
		style := lipgloss.NewStyle()
		if m.approval.toolName == "write_file" {
			style = diffLineStyle(l)
		}
		lines = append(lines, style.Render(truncateWidth(l, innerW)))
	}
	cmd := strings.Join(lines, "\n")
	hint := "Y 允许 · N 拒绝 · Esc 取消 · Tab 详情"
	if m.approval.toolName == "write_file" {
		hint = "Y 写入 · N 拒绝 · Esc 取消 · Tab 全部改动"
	}
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
	width := m.contentWidth()
	pad := lipgloss.NewStyle().Padding(0, 1)

	// 一轮回答（用户消息之后的若干助手气泡与工具卡）合成一组，只显示一次 GoAgent 标签。
	inGroup := false
	var group strings.Builder
	reasoning := 0
	var reasoningText []string
	pendingStream := false

	flush := func() {
		if !inGroup {
			return
		}
		label := agentTagStyle.Render("GoAgent")
		if reasoning > 0 && !m.showReasoning {
			label += " " + mutedStyle.Render(fmt.Sprintf("思考 %d 字 · Ctrl+R", reasoning))
		}
		body := label + "\n"
		if m.showReasoning && len(reasoningText) > 0 {
			body += mutedStyle.Render(wrapText(strings.Join(reasoningText, "\n"), width)) + "\n"
		}
		inner := strings.TrimRight(group.String(), "\n")
		if inner == "" && pendingStream {
			inner = mutedStyle.Render("…")
		}
		body += inner
		b.WriteString(pad.Render(strings.TrimRight(body, "\n")) + "\n\n")
		inGroup, reasoning, reasoningText, pendingStream = false, 0, nil, false
		group.Reset()
	}

	for idx := range m.messages {
		msg := &m.messages[idx]
		switch msg.role {
		case roleAssistant, roleTool:
			inGroup = true
			if msg.role == roleTool {
				group.WriteString(m.toolCardLine(*msg, width) + "\n")
				continue
			}
			if msg.reasoning != "" {
				reasoning += utf8.RuneCountInString(msg.reasoning)
				reasoningText = append(reasoningText, msg.reasoning)
			}
			if msg.content == "" {
				if !msg.done {
					pendingStream = true
				}
				continue
			}
			group.WriteString(m.assistantBody(msg, width) + "\n")
		default:
			flush()
			b.WriteString(m.renderMessage(*msg))
		}
	}
	flush()
	return strings.TrimRight(b.String(), "\n")
}

// assistantBody 渲染助手正文：流式时纯文本，完成后 Glamour（带缓存）。
func (m *Model) assistantBody(msg *chatMessage, width int) string {
	if !msg.done {
		return wrapText(msg.content, width)
	}
	if msg.mdCache == "" || msg.mdW != width {
		msg.mdCache = renderMarkdown(msg.content, width)
		msg.mdW = width
	}
	return msg.mdCache
}

// toolCardLine 是主区的一行工具卡：⚙ name · ✓ 12ms · 摘要。
func (m *Model) toolCardLine(msg chatMessage, width int) string {
	name := "⚙ " + msg.toolName
	var summary string
	switch {
	case !msg.done:
		summary = "运行中…"
	case msg.toolError:
		summary = "✗ · " + firstLine(msg.toolResult)
	default:
		summary = "✓ " + msg.toolDur.Round(time.Millisecond).String()
		if s := toolSummary(msg.toolName, msg.toolResult); s != "" {
			summary += " · " + s
		}
	}
	plain := truncateWidth(name+" · "+summary, width)
	if i := strings.Index(plain, " · "); i >= 0 {
		name, summary = plain[:i], plain[i+len(" · "):]
	} else {
		name, summary = plain, ""
	}
	suffix := ""
	if summary != "" {
		st := mutedStyle
		if msg.done && msg.toolError {
			st = errorStyle
		}
		suffix = st.Render(" · " + summary)
	}
	return toolLabelStyle.Render(name) + suffix
}

// toolSummary 只写结果数量或第一行，不把多行内容挤成一行。
func toolSummary(tool, result string) string {
	result = strings.TrimRight(result, "\n")
	if result == "" {
		return ""
	}
	n := strings.Count(result, "\n") + 1
	switch tool {
	case "list_dir":
		if result == "(空目录)" {
			return "空目录"
		}
		return fmt.Sprintf("%d 项", n)
	case "read_file":
		return fmt.Sprintf("%d 行", n)
	}
	if n > 1 {
		return fmt.Sprintf("%s（共 %d 行）", firstLine(result), n)
	}
	return result
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

func (m *Model) renderMessage(msg chatMessage) string {
	width := m.contentWidth()
	pad := lipgloss.NewStyle().Padding(0, 1)

	switch msg.role {
	case roleUser:
		body := userTagStyle.Render("你") + "\n" +
			lipgloss.NewStyle().Width(width).Render(wrapText(msg.content, width))
		return pad.Render(body) + "\n\n"

	case roleNotice:
		body := "· " + strings.ReplaceAll(msg.content, "\n", "\n  ")
		return pad.Render(mutedStyle.Width(width).Render(wrapText(body, width))) + "\n\n"

	case roleError:
		body := "! " + strings.ReplaceAll(msg.content, "\n", "\n  ")
		return pad.Render(errorStyle.Width(width).Render(wrapText(body, width))) + "\n\n"
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

var (
	diffAddStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	diffDelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// diffLineStyle 按 "+ "/"- " 前缀给 diff 行上色，其余行用弱化色。
func diffLineStyle(l string) lipgloss.Style {
	switch {
	case strings.HasPrefix(l, "+ "):
		return diffAddStyle
	case strings.HasPrefix(l, "- "):
		return diffDelStyle
	default:
		return mutedStyle
	}
}
