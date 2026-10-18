package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// 对话区渲染缓存。
//
// 每个已完成的消息段（一轮助手回答或一条其他消息）渲染一次后按行缓存，连同每行宽度；
// 拼视口内容时只需按全局最大行宽补空格、加左边距，不再对整段对话重新测宽。
// 缓存键包含段内每条消息影响渲染的字段，以及内容宽度、思考开关和主题，
// 所以消息被编辑、回退、压缩、清空或换会话时自然失效（键对不上就重渲染）。
// 未完成（流式中）的段每次都重渲染，不进缓存。

// segMsgKey 是一条消息里影响对话区渲染的全部字段。
// 字符串未变时 Go 比较的是同一块内存，开销很小。
type segMsgKey struct {
	role       role
	content    string
	reasoning  string
	done       bool
	toolName   string
	toolArgs   string
	toolResult string
	toolError  bool
	toolDur    time.Duration
}

func keyOf(msg *chatMessage) segMsgKey {
	return segMsgKey{
		role: msg.role, content: msg.content, reasoning: msg.reasoning, done: msg.done,
		toolName: msg.toolName, toolArgs: msg.toolArgs, toolResult: msg.toolResult,
		toolError: msg.toolError, toolDur: msg.toolDur,
	}
}

// renderParams 是消息之外影响渲染的状态。
type renderParams struct {
	width         int
	showReasoning bool
	theme         string
}

type cachedSeg struct {
	keys   []segMsgKey
	params renderParams
	raw    string   // 渲染结果，以 "\n\n" 结尾
	lines  []string // raw 去掉结尾 "\n\n" 后按行切开
	widths []int
	maxW   int

	padded       string // 补齐到 padW、加 padMargin 边距后的块，每行以 "\n" 结尾
	padW, margin int
	padOK        bool
}

type renderCache struct {
	segs []cachedSeg
}

func (c *cachedSeg) matches(m *Model, lo, hi int, p renderParams) bool {
	if c.params != p || len(c.keys) != hi-lo {
		return false
	}
	for i := range c.keys {
		if c.keys[i] != keyOf(&m.messages[lo+i]) {
			return false
		}
	}
	return true
}

func spanDone(m *Model, lo, hi int) bool {
	for i := lo; i < hi; i++ {
		if !m.messages[i].done {
			return false
		}
	}
	return true
}

func newCachedSeg(raw string) (cachedSeg, bool) {
	c := cachedSeg{raw: raw}
	if raw == "" {
		return c, true
	}
	core, ok := strings.CutSuffix(raw, "\n\n")
	if !ok || strings.HasSuffix(core, "\n") {
		return c, false
	}
	c.lines = strings.Split(core, "\n")
	c.widths = make([]int, len(c.lines))
	for i, l := range c.lines {
		w := lipgloss.Width(l)
		c.widths[i] = w
		c.maxW = max(c.maxW, w)
	}
	return c, true
}

// paddedBlock 返回补齐到 w、加 margin 左边距的块；last 为真时不带结尾空行。
func (c *cachedSeg) paddedBlock(w, margin int) string {
	if c.padOK && c.padW == w && c.margin == margin {
		return c.padded
	}
	var b strings.Builder
	left := strings.Repeat(" ", margin)
	blank := left + strings.Repeat(" ", w)
	b.Grow(len(c.raw) + (len(c.lines)+1)*(margin+8))
	for i, l := range c.lines {
		b.WriteString(left)
		b.WriteString(l)
		b.WriteString(strings.Repeat(" ", w-c.widths[i]))
		b.WriteByte('\n')
	}
	b.WriteString(blank) // 段间空行
	b.WriteByte('\n')
	c.padded, c.padW, c.margin, c.padOK = b.String(), w, margin, true
	return c.padded
}

// transcriptContent 返回视口内容：等价于
// lipgloss.NewStyle().MarginLeft(margin).Render(m.renderTranscript())，
// 但已完成的段走缓存，只重渲染变化的段（通常只有最后一组）。
func (m *Model) transcriptContent() string {
	margin := m.columnMargin()
	p := renderParams{width: m.contentWidth(), showReasoning: m.showReasoning, theme: currentTheme}
	spans := m.transcriptSpans()

	segs := make([]*cachedSeg, 0, len(spans)+1)
	logo, ok := newCachedSeg(m.transcriptLogo())
	if !ok {
		return m.slowTranscriptContent(margin)
	}
	segs = append(segs, &logo)

	old := m.rcache.segs
	next := make([]cachedSeg, len(spans))
	var live []cachedSeg // 未完成的段不进缓存
	for i, sp := range spans {
		lo, hi := sp[0], sp[1]
		if i < len(old) && old[i].matches(m, lo, hi, p) {
			next[i] = old[i]
			segs = append(segs, &next[i])
			continue
		}
		c, ok := newCachedSeg(m.renderSpan(lo, hi))
		if !ok {
			m.rcache.segs = nil
			return m.slowTranscriptContent(margin)
		}
		if spanDone(m, lo, hi) {
			c.params = p
			c.keys = make([]segMsgKey, hi-lo)
			for k := lo; k < hi; k++ {
				c.keys[k-lo] = keyOf(&m.messages[k])
			}
			next[i] = c
			segs = append(segs, &next[i])
		} else {
			live = append(live, c)
			segs = append(segs, &live[len(live)-1])
		}
	}
	m.rcache.segs = next

	// 去掉空段；找出全局最大行宽和总行数
	nonEmpty := segs[:0]
	maxW, lines := 0, 0
	for _, s := range segs {
		if s.raw == "" {
			continue
		}
		nonEmpty = append(nonEmpty, s)
		maxW = max(maxW, s.maxW)
		lines += len(s.lines) + 1
	}
	if len(nonEmpty) == 0 || lines-1 <= 1 {
		// 空内容或只有一行：lipgloss 不补齐，交给原实现
		return m.slowTranscriptContent(margin)
	}

	var b strings.Builder
	size := 0
	for _, s := range nonEmpty {
		size += len(s.paddedBlock(maxW, margin))
	}
	b.Grow(size)
	for _, s := range nonEmpty {
		b.WriteString(s.paddedBlock(maxW, margin))
	}
	// 整段对话结尾的空行与换行在原实现里被 TrimRight 掉
	out := b.String()
	out = out[:len(out)-(margin+maxW+1)-1]
	return out
}

func (m *Model) slowTranscriptContent(margin int) string {
	return lipgloss.NewStyle().MarginLeft(margin).Render(m.renderTranscript())
}
