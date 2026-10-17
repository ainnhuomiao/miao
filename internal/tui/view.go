package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"

	"agnet/internal/llm"
)

const (
	headerHeight     = 2
	inputMaxLines    = 6
	columnMax        = 100
	activityMaxRows  = 1 // 输入框上方工具状态条只占一行
	inputFrameHeight = 2
	hintHeight       = 1
	approvalCardBase = 4 // 边框2 + 标题1 + 提示1，另加 display 行数
	approvalMaxLines = 12
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

	userTagStyle  = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(userColor).Background(surface0Color)
	agentTagStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(agentColor).Background(surface0Color)

	segAccentStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(crustColor).Background(accentColor)
	segStyle       = lipgloss.NewStyle().Padding(0, 1).Foreground(textColor).Background(surface0Color)

	approvalBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(accentColor).
				Padding(0, 1)
)

const helpText = `命令:
  /help       显示帮助
  /new        新建会话
  /sessions   列出已保存会话
  /load <id>  加载会话
  /clear      清空对话并换新会话
  /reasoning  展开或收起思考过程
  /exit       退出

快捷键:
  Enter 发送 · Ctrl+J 换行 · Ctrl+R 思考过程
  PgUp/PgDn 滚动 · Esc 取消审批/取消生成 · Ctrl+C 取消或退出
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

	parts := []string{m.headerView(), main}
	// 工具状态条常驻占一行（空闲显示待命文案），输入框不会上下跳
	parts = append(parts, m.activityView())
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
	left := " " + titleStyle.Render("GoAgent")
	suffix := " · " + m.modelName
	right := m.contextView() + " "
	if lipgloss.Width(left)+lipgloss.Width(suffix)+lipgloss.Width(right)+2 > m.width {
		right = ""
	}
	if lipgloss.Width(left)+lipgloss.Width(suffix)+lipgloss.Width(right)+2 <= m.width {
		left += lipgloss.NewStyle().Foreground(subtext0Color).Render(suffix)
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	rule := " " + lipgloss.NewStyle().Foreground(surface0Color).Render(strings.Repeat("─", max(0, m.width-2)))

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
		sp := m.spinner
		color := accentColor
		if label == "工具中" {
			color = toolColor
		}
		sp.Style = lipgloss.NewStyle().Foreground(color).Background(surface0Color)
		elapsed := time.Since(m.streamStart).Truncate(100 * time.Millisecond)
		bg := lipgloss.NewStyle().Background(surface0Color)
		return sp.View() + bg.Foreground(color).Render(label) +
			bg.Foreground(mutedColor).Render(fmt.Sprintf(" %.1fs", elapsed.Seconds()))
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
	st := inputBoxStyle
	if strings.TrimSpace(m.input.Value()) == "" {
		st = st.BorderForeground(mutedColor) // 空着时边框淡一点
	}
	return lipgloss.NewStyle().MarginLeft(m.columnMargin()).Render(st.Render(m.input.View()))
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
	hint := "Y 允许 · N 拒绝 · Esc 取消"
	if m.approval.toolName == "write_file" {
		hint = "Y 写入 · N 拒绝 · Esc 取消"
	}
	body := titleStyle.Render(title) + "\n" + cmd + "\n" + mutedStyle.Render(hint)
	return approvalBoxStyle.Width(max(24, m.width-2)).Render(body)
}

// keymapText 是按 ? 时列出的完整键位表。
const keymapText = `键位表
  Enter     发送
  Ctrl+J    换行
  Ctrl+R    显示/隐藏思考过程
  Esc       取消生成
  PgUp/PgDn 滚动对话
  ?         显示本表（输入框为空时）
  Ctrl+C    退出`

// barKeys 是底栏最右的灰字提示：平时只有「? 帮助」，生成和审批时换成当下能用的键。
func (m *Model) barKeys() string {
	switch {
	case m.approval != nil:
		return "Y 允许  N 拒绝  Esc 取消"
	case m.streaming:
		return "Esc 取消"
	default:
		return "? 帮助"
	}
}

// usageText 是 token 胶囊的文字：↑输入 ↓输出 · 缓存N%；没有用量时为空。
func (m *Model) usageText() string {
	u := m.usage
	if u == nil || (u.PromptTokens == 0 && u.CompletionTokens == 0) {
		return ""
	}
	t := "↑" + formatTokens(u.PromptTokens) + " ↓" + formatTokens(u.CompletionTokens)
	if pct, ok := cachePercent(u); ok {
		t += fmt.Sprintf(" · 缓存%d%%", pct)
	}
	return t
}

// statusDot 是底栏最左的状态：空闲 green ●，思考 mauve、跑工具 peach 的 spinner + 文字 + 计时。
func (m *Model) statusDot() string {
	if !m.streaming {
		return lipgloss.NewStyle().Foreground(agentColor).Render("●") +
			lipgloss.NewStyle().Foreground(subtext0Color).Render(" 就绪")
	}
	label, color := "思考中", accentColor
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == roleTool && !m.messages[i].done {
			label, color = "工具中", toolColor
			break
		}
	}
	if label == "思考中" && m.lastAssistantContent() != "" {
		label = "生成中"
	}
	return lipgloss.NewStyle().Foreground(color).Render("●") +
		lipgloss.NewStyle().Foreground(subtext0Color).Render(" "+label)
}

// barView 拼出极简底栏：不铺底色、没有色块，只有状态点带颜色，其余灰字，宽度对齐对话列。
//
//	~/agnet            ● 就绪 · deepseek-flash · ↑703 ↓78 · 缓存72% · ? 帮助
func (m *Model) barView(showPath, showUsage bool) (string, bool) {
	w := m.columnWidth()
	sep := mutedStyle.Render(" · ")
	left := ""
	if showPath {
		left = mutedStyle.Render(strings.TrimPrefix(m.workspaceLabel(), "⌂ "))
	}
	parts := []string{m.statusDot(), lipgloss.NewStyle().Foreground(accentColor).Render(m.modelName)}
	if showUsage {
		if u := m.usageText(); u != "" {
			parts = append(parts, mutedStyle.Render(u))
		}
	}
	parts = append(parts, mutedStyle.Render(m.barKeys()))
	right := strings.Join(parts, sep)
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		return "", false
	}
	line := " " + left + strings.Repeat(" ", gap) + right + " "
	return lipgloss.NewStyle().MarginLeft(m.columnMargin()).Render(line), true
}

// hintView 是极简底栏。窄屏时先藏 token，再藏路径。
func (m *Model) hintView() string {
	for _, t := range [][2]bool{{true, true}, {true, false}, {false, false}} {
		if out, ok := m.barView(t[0], t[1]); ok {
			return out
		}
	}
	return truncateWidth(m.statusDot()+" "+m.modelName, m.width)
}

// usageLabel 渲染用量段：↑输入 ↓输出，接口返回缓存数据时再加 green 色缓存率。
// 没有用量时返回空串。
func (m *Model) usageLabel(showCache bool) string {
	u := m.usage
	if u == nil || (u.PromptTokens == 0 && u.CompletionTokens == 0) {
		return ""
	}
	bg := lipgloss.NewStyle().Background(surface0Color)
	muted := bg.Foreground(mutedColor)
	out := muted.Render(" ↑" + formatTokens(u.PromptTokens) + " ↓" + formatTokens(u.CompletionTokens))
	if showCache {
		if pct, ok := cachePercent(u); ok {
			out += muted.Render(" · ") + bg.Foreground(agentColor).Render(fmt.Sprintf("缓存 %d%%", pct))
		}
	}
	return out + bg.Render(" ")
}

// cachePercent 返回缓存命中率（命中数 / prompt_tokens）；无缓存数据时 ok 为 false。
func cachePercent(u *llm.Usage) (int, bool) {
	if u.PromptCacheHitTokens <= 0 || u.PromptTokens <= 0 {
		return 0, false
	}
	return u.PromptCacheHitTokens * 100 / u.PromptTokens, true
}

// formatTokens 把 token 数缩写成 980 / 1.2k / 15k / 1.3M。
func formatTokens(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 10_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	case n < 1_000_000:
		return strconv.Itoa(n/1000) + "k"
	default:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
	}
}

// barStatus 是底栏状态段：就绪，或生成时 spinner + 状态 + 计时。
func (m *Model) barStatus() string {
	bg := lipgloss.NewStyle().Background(surface0Color)
	if !m.streaming {
		return bg.Foreground(agentColor).Render("● ") + bg.Foreground(textColor).Render("就绪")
	}
	return m.statusView()
}

func (m *Model) workspaceLabel() string {
	ws := m.ag.Workspace()
	if ws == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(ws, home) {
		ws = "~" + strings.TrimPrefix(ws, home)
	}
	return "⌂ " + ws
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

	if !m.hasUserMessage() && m.viewport.Height >= 6 {
		logo := gradientLogo()
		sub := gradientText("你的 Go 终端 agent")
		hint := mutedStyle.Render("Enter 开始对话 · /help 查看命令")
		b.WriteString(lipgloss.PlaceHorizontal(max(width, lipgloss.Width(logo)), lipgloss.Center,
			logo+"\n\n"+sub+"\n"+hint) + "\n\n")
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
		bodyW := max(1, width-2)
		bodyLines := strings.Split(wrapText(msg.content, bodyW), "\n")
		bar := lipgloss.NewStyle().Foreground(userColor).Render("▏")
		var body strings.Builder
		body.WriteString(userTagStyle.Render("你"))
		for _, ln := range bodyLines {
			body.WriteString("\n" + bar + " " + ln)
		}
		return pad.Render(body.String()) + "\n\n"

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
	return max(10, m.columnWidth()-4)
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
	diffAddStyle = lipgloss.NewStyle().Foreground(agentColor)
	diffDelStyle = lipgloss.NewStyle().Foreground(errorColor)
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

func (m *Model) hasUserMessage() bool {
	for _, msg := range m.messages {
		if msg.role == roleUser {
			return true
		}
	}
	return false
}

// activityRows 是最近几次工具调用（最新的在最下面），每次一行。
func (m *Model) activityRows() []chatMessage {
	var rows []chatMessage
	for i := len(m.messages) - 1; i >= 0 && len(rows) < activityMaxRows; i-- {
		if m.messages[i].role == roleTool {
			rows = append([]chatMessage{m.messages[i]}, rows...)
		}
	}
	return rows
}

// activityView 渲染输入框上方的常驻工具状态条：surface0 底 + peach 竖线；空闲显示「┈ 工具待命」。
func (m *Model) toolCount() int {
	n := 0
	for _, msg := range m.messages {
		if msg.role == roleTool {
			n++
		}
	}
	return n
}

func (m *Model) activityView() string {
	w := max(10, m.columnWidth())
	bg := lipgloss.NewStyle()
	bar := bg.Foreground(toolColor).Render("▏")
	padL := lipgloss.NewStyle().MarginLeft(m.columnMargin())

	rows := m.activityRows()
	if len(rows) == 0 {
		idle := " " + mutedStyle.Render("┈ 工具待命")
		if th := m.thinkingView(); th != "" {
			gap := max(1, w-lipgloss.Width(idle)-lipgloss.Width(th)-1)
			idle += strings.Repeat(" ", gap) + th
		}
		return padL.Render(idle)
	}
	t := rows[0]
	var icon string
	iconStyle := bg.Foreground(agentColor)
	dur := ""
	switch {
	case !t.done:
		sp := m.spinner
		sp.Style = bg.Foreground(toolColor)
		icon = sp.View()
		iconStyle = bg
	case t.toolError:
		icon, iconStyle = "✗", bg.Foreground(errorColor)
	default:
		icon = "✓"
	}
	if t.done {
		dur = t.toolDur.Round(time.Millisecond).String() + "  "
	}
	dur += fmt.Sprintf("工具 %d", m.toolCount())
	detail := formatArgs(t.toolName, t.toolArgs)
	if t.done {
		if t.toolError {
			detail += " · " + firstLine(t.toolResult)
		} else if s := toolSummary(t.toolName, t.toolResult); s != "" && t.toolName != "run_command" {
			detail += " · " + s
		}
	}
	name := bg.Foreground(toolColor).Bold(true).Render(t.toolName)
	left := bar + iconStyle.Render(icon) + bg.Render(" ") + name + bg.Render(" ")
	right := bg.Foreground(mutedColor).Render(dur)
	if th := m.thinkingView(); th != "" {
		right = th + mutedStyle.Render(" · ") + right
	}
	room := w - lipgloss.Width(left) - lipgloss.Width(right) - 1
	detail = truncateWidth(strings.TrimPrefix(detail, " · "), max(1, room))
	gap := max(0, room-lipgloss.Width(detail))
	line := left + bg.Foreground(mutedColor).Render(detail) + bg.Render(strings.Repeat(" ", gap+1)) + right
	return padL.Render(line)
}

// thinkingGlyphs 是 Claude Code 风格的思考星号，来回往返播放。
var thinkingGlyphs = []string{"·", "✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳", "✢"}

// thinkingVerbs 每隔几秒换一个动词，模仿 Claude Code 的趣味状态词。
var thinkingVerbs = []string{"思考中", "琢磨中", "推敲中", "酝酿中", "构思中", "盘算中"}

// thinkingView 渲染工具行右侧的思考动画：✻ 琢磨中… 3.2s；不在生成时返回空串。
func (m *Model) thinkingView() string {
	if !m.streaming {
		return ""
	}
	el := time.Since(m.streamStart)
	glyph := thinkingGlyphs[int(el/(120*time.Millisecond))%len(thinkingGlyphs)]
	verb := thinkingVerbs[int(el/(3*time.Second))%len(thinkingVerbs)]
	dots := strings.Repeat(".", int(el/(400*time.Millisecond))%3+1)
	return lipgloss.NewStyle().Foreground(accentColor).Render(glyph+" ") +
		gradientText(verb) + mutedStyle.Render(fmt.Sprintf("%-3s %.1fs · esc 中断", dots, el.Seconds()))
}
