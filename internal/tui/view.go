package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"

	"github.com/ainnhuomiao/miao/internal/llm"
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

const helpText = `命令:
  /help       显示帮助
  /new        新建会话
  /sessions   切换会话（↑↓ 选择 · Enter 切换 · 打字筛选 · Ctrl+D 删除）
  /load <id>  按 id 加载会话
  /clear      清空对话并换新会话
  /undo       撤销最近一次文件修改（可连续撤销）
  /compact    压缩上下文（用量到 80% 时自动触发）
  /apply      确认规划方案，切到自动同意编辑并执行
  /cost       本会话估算花费
  /init       扫描项目生成 AGENTS.md
  /trust-project  信任当前仓库的项目配置
  /export     把当前会话导出为 Markdown（默认到数据目录，会脱敏）
  !<命令>     在工作区直通执行 shell（只读规划模式下不可用）
  /diff       查看本次会话改过的文件和 diff
  /commit     模型写提交信息，确认后只提交本会话改过的文件
  /fork       把当前对话复制成新会话
  Esc Esc     回到之前的某条消息重新编辑（文件改动一起还原）
  /model      切换模型（/model <名字>）
  /theme      切换主题（/theme <名字>，↑↓ 实时预览）
  /subagents  子代理设置（开关、并发上限、模型）
  /mcp        查看 MCP 服务器状态与工具数
  /hooks      查看已配置的 hook（工具调用前后、回合结束自动执行的命令）
  自定义命令  .miao/commands/*.md 或 ~/.config/miao/commands/*.md，模板里用 $ARGS 接参数
  /reasoning  展开或收起思考过程
  /exit       退出`

func (m *Model) view() string {
	if !m.ready {
		return "正在启动…"
	}
	if m.width < 36 || m.height < 12 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			mutedStyle.Render("终端窗口太小\n请调整到至少 36×12"))
	}

	main := m.viewport.View()
	if m.isEmptySession() && m.side.tree.preview == nil {
		main = m.welcomeView(m.viewport.Width, m.viewport.Height)
	}

	header := m.headerView()
	side := m.sidebarCols()
	if side > 0 { // 主体各部件按侧栏右边剩下的宽度渲染
		w := m.width
		m.width -= side
		defer func() { m.width = w }()
	}
	parts := []string{header, main}
	// 工具状态条常驻占一行（空闲显示待命文案），输入框不会上下跳
	if m.paletteOpen() {
		parts = append(parts, m.paletteView())
	} else {
		if p := m.todoPanelView(); p != "" {
			parts = append(parts, p)
		}
		parts = append(parts, m.activityView())
	}
	if m.picker.open {
		parts = append(parts, m.sessionPickerView())
	}
	if m.rewind.open {
		parts = append(parts, m.rewindView())
	}
	if m.approval != nil {
		parts = append(parts, m.approvalView())
	}
	if m.keysCard {
		parts = append(parts, m.keysCardView())
	}
	parts = append(parts, m.inputView(), m.hintView())
	return m.withSidebar(side, parts[0], strings.Join(parts[1:], "\n"))
}

func (m *Model) headerView() string {
	suffix := " · " + m.modelName
	// 先按 猫+miao → 只有猫 → 都不要 降级，再走吃豆人自己的降级。
	left := m.catLogo(0)
	for lv := 2; lv >= 0; lv-- {
		left = m.catLogo(lv)
		if m.width-lipgloss.Width(left)-lipgloss.Width(suffix)-lipgloss.Width(m.contextViewCells(16))-5 >= 0 {
			break
		}
	}
	// 豆子轨道铺满标题与数字之间的整段空白。
	right := m.contextViewCells(16) + " "
	if extra := m.width - lipgloss.Width(left) - lipgloss.Width(suffix) - lipgloss.Width(right) - 4; extra > 0 {
		right = "  " + m.contextViewCells(16+extra) + " "
	} else if extra < 0 {
		// 窄屏（按定稿）：先去掉 54k/128k，轨道缩到最少 8 格，再不够只留 ᗧ 42%。
		right = m.narrowContext(m.width - lipgloss.Width(left) - lipgloss.Width(suffix) - 3)
		if right == "" {
			right = m.narrowContext(m.width - lipgloss.Width(left) - 3)
		}
		if lipgloss.Width(left)+lipgloss.Width(right)+2 > m.width {
			right = ""
		}
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
		if isFileEdit(m.approval.toolName) {
			style = diffLineStyle(l)
		}
		lines = append(lines, style.Render(truncateWidth(l, innerW)))
	}
	cmd := strings.Join(lines, "\n")
	hint := "Y 允许 · A 本会话总是允许 · N 拒绝 · Esc 取消"
	if m.approval.toolName == "web_search" {
		hint = "Y 允许 · A 本会话再次搜索这条完全相同的内容时放行 · N 拒绝 · Esc 取消"
	}
	if isFileEdit(m.approval.toolName) {
		hint = "Y 写入 · B 本轮全部写入 · A 本会话总是允许 · N 拒绝 · Esc 取消"
	}
	body := titleStyle.Render(title) + "\n" + cmd + "\n" + mutedStyle.Render(hint)
	return approvalBoxStyle.Width(max(24, m.width-2)).Render(body)
}

// barKeys 是底栏最右的灰字提示：平时只有「? 帮助」，生成和审批时换成当下能用的键。
func (m *Model) barKeys() string {
	switch {
	case m.approval != nil:
		return "Y 允许  A 总是  N 拒绝  Esc 取消"
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
	if hit, ok := formatCacheHit(m.lastReq); ok {
		t += " · 缓存" + hit
	} else if hit, ok := formatCacheHit(u); ok {
		t += " · 缓存" + hit
	}
	if c := m.costText(); c != "" {
		t += " · " + c
	}
	return t
}

func (m *Model) costText() string {
	if m.usage == nil {
		return ""
	}
	usd, ok := llm.EstimateCostUSD(m.modelName, *m.usage, priceOverride("PRICE_IN"), priceOverride("PRICE_OUT"), priceOverride("PRICE_CACHE"))
	if !ok {
		return ""
	}
	return llm.FormatCost(usd)
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
//	~/miao             ● 就绪 · deepseek-flash · ↑703 ↓78 · 缓存72% · ? 帮助
func (m *Model) barView(showPath, showUsage bool) (string, bool) {
	w := m.columnWidth()
	sep := mutedStyle.Render(" · ")
	left := ""
	if showPath {
		left = mutedStyle.Render(strings.TrimPrefix(m.workspaceLabel(), "⌂ "))
	}
	parts := []string{m.statusDot() + " " + mutedStyle.Render(m.ag.Mode().String())}
	parts = append(parts, lipgloss.NewStyle().Foreground(accentColor).Render(m.modelName))
	if n := m.ag.RunningJobs(); n > 0 {
		parts = append(parts, mutedStyle.Render(fmt.Sprintf("后台 %d", n)))
	}
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

// hintView 是极简底栏。窄屏时先藏路径，再藏用量，尽量保留缓存命中率。
func (m *Model) hintView() string {
	for _, t := range [][2]bool{{true, true}, {false, true}, {false, false}} {
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
		cu := m.lastReq
		if cu == nil {
			cu = u
		}
		if hit, ok := formatCacheHit(cu); ok {
			out += muted.Render(" · ") + bg.Foreground(agentColor).Render("缓存 "+hit)
		}
	}
	return out + bg.Render(" ")
}

// formatCacheHit 返回本次请求的缓存命中率；高命中（≥99%）显示一位小数。
func formatCacheHit(u *llm.Usage) (string, bool) {
	if u == nil || u.PromptCacheHitTokens <= 0 || u.PromptTokens <= 0 {
		return "", false
	}
	pct := float64(u.PromptCacheHitTokens) * 100 / float64(u.PromptTokens)
	if pct >= 99 {
		return fmt.Sprintf("%.1f%%", pct), true
	}
	return fmt.Sprintf("%.0f%%", pct), true
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

// renderTranscript 是对话区的全部内容（不含左侧居中边距），按段拼接。
func (m *Model) renderTranscript() string {
	var b strings.Builder
	for _, s := range m.transcriptSegments(nil) {
		b.WriteString(s)
	}
	return strings.TrimRight(b.String(), "\n")
}

// transcriptLogo 是还没有用户消息时顶部的大 logo，没有时返回空串。
func (m *Model) transcriptLogo() string {
	if m.hasUserMessage() || m.viewport.Height < 6 {
		return ""
	}
	width := m.contentWidth()
	logo := gradientLogo()
	sub := gradientText("你的 Go 终端 agent")
	hint := mutedStyle.Render("Enter 开始对话 · /help 查看命令")
	return lipgloss.PlaceHorizontal(max(width, lipgloss.Width(logo)), lipgloss.Center,
		logo+"\n\n"+sub+"\n"+hint) + "\n\n"
}

// transcriptSpans 把消息切成渲染段：一轮回答（连续的助手气泡与工具卡）合成一段，
// 其余消息各自一段。返回每段的 [起, 止) 下标。
func (m *Model) transcriptSpans() [][2]int {
	var spans [][2]int
	for i := 0; i < len(m.messages); {
		j := i + 1
		if isGroupRole(m.messages[i].role) {
			for j < len(m.messages) && isGroupRole(m.messages[j].role) {
				j++
			}
		}
		spans = append(spans, [2]int{i, j})
		i = j
	}
	return spans
}

func isGroupRole(r role) bool { return r == roleAssistant || r == roleTool }

// transcriptSegments 依次返回 logo 段和每个消息段的渲染结果（各自以 "\n\n" 结尾，空段为空串）。
// render 非空时用它渲染消息段（缓存层），否则直接渲染。
func (m *Model) transcriptSegments(render func(lo, hi int) string) []string {
	if render == nil {
		render = m.renderSpan
	}
	spans := m.transcriptSpans()
	out := make([]string, 0, len(spans)+1)
	out = append(out, m.transcriptLogo())
	for _, sp := range spans {
		out = append(out, render(sp[0], sp[1]))
	}
	return out
}

// renderSpan 渲染 messages[lo:hi]：一组助手/工具消息，或单条其他消息。
func (m *Model) renderSpan(lo, hi int) string {
	if !isGroupRole(m.messages[lo].role) {
		return m.renderMessage(m.messages[lo])
	}
	width := m.contentWidth()
	pad := lipgloss.NewStyle().Padding(0, 1)

	// 一轮回答只显示一次 miao 标签。
	var group strings.Builder
	reasoning := 0
	var reasoningText []string
	pendingStream := false
	for idx := lo; idx < hi; idx++ {
		msg := &m.messages[idx]
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
	}

	label := agentTagStyle.Render("miao")
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
	return pad.Render(strings.TrimRight(body, "\n")) + "\n\n"
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
	if isTaskTool(msg.toolName) {
		return m.taskCardLine(msg, width)
	}
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

	case roleDiff:
		return pad.Render(diffBubble(msg.content, width)) + "\n\n"

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
	// 统一用 ANSI 感知、与 lipgloss.Width 同口径的截断：不会切断颜色码，
	// 也不受 LC_ALL=zh_CN 等语言环境影响（runewidth 会把模糊宽度字符算成 2）。
	return xansi.Truncate(s, width, "…")
}

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

	if v, ok := m.imageChipsView(w); ok {
		return padL.Render(v)
	}
	if v, ok := m.liveActivityView(w); ok {
		return padL.Render(v)
	}
	if v, ok := m.subagentActivityView(w); ok {
		return padL.Render(v)
	}
	if v, ok := m.taskActivityView(w); ok {
		return padL.Render(v)
	}
	if v, ok := m.todoActivityView(w); ok {
		return padL.Render(v)
	}
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
	if !t.done && t.toolLive != "" {
		detail += " · " + t.toolLive
	}
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
	if m.compacting {
		verb = "压缩上下文"
	}
	if m.shelling {
		verb = "执行命令"
	}
	dots := strings.Repeat(".", int(el/(400*time.Millisecond))%3+1)
	return lipgloss.NewStyle().Foreground(accentColor).Render(glyph+" ") +
		gradientText(verb) + mutedStyle.Render(fmt.Sprintf("%-3s %.1fs · esc 中断", dots, el.Seconds()))
}
