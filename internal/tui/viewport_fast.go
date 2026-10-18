package tui

import (
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// styleGen 在每次重建样式（换主题）时递增，用来让按样式缓存的画面失效。
var styleGen int

// viewFastCache 让对话区绕开 bubbles v2 viewport 的两处整块量宽：
//   - SetContent 每次都逐行按字素量宽求最长行（每次刷新都要扫完整个对话记录）；
//   - View 每帧用 lipgloss 把可见块重新折行、对齐、补宽。
//
// 对话内容已按列宽排好版，所以只把等行数的占位行交给 viewport（滚动、AtBottom、鼠标滚轮照常工作），
// 真正的文字留在这里，View 时只量可见行并补空格拼接，结果与 viewport.View 逐字节相同。
// 内容含 tab/CR 或有行超宽时，退回把真实内容交给 viewport 处理。
type viewFastCache struct {
	raw     string
	lines   []string
	blank   []string // 占位行（单个空格，量宽极快），复用以免每次分配
	gen     int
	slow    bool
	key     [4]int
	out     string
	outGood bool
}

// setViewportContent 设置对话区内容。
func (m *Model) setViewportContent(s string) {
	c := &m.vpFast
	c.gen++
	c.outGood = false
	c.raw = s
	c.lines = strings.Split(s, "\n")
	if len(c.lines) == 1 && xansi.StringWidth(c.lines[0]) == 0 {
		c.lines = nil
	}
	c.slow = strings.ContainsAny(s, "\t\r")
	if c.slow || c.lines == nil {
		m.viewport.SetContent(s)
		return
	}
	for len(c.blank) < len(c.lines) {
		c.blank = append(c.blank, " ")
	}
	m.viewport.SetContentLines(c.blank[:len(c.lines):len(c.lines)])
}

// toSlow 把真实内容交给 viewport（保持滚动位置），之后都走 viewport.View。
func (m *Model) toSlow() string {
	c := &m.vpFast
	y := m.viewport.YOffset()
	c.slow = true
	m.viewport.SetContent(c.raw)
	m.viewport.SetYOffset(y)
	return m.viewport.View()
}

// viewportView 渲染对话区，等价于对真实内容调用 viewport.View()。
func (m *Model) viewportView() string {
	c := &m.vpFast
	w, h, y := m.viewport.Width(), m.viewport.Height(), m.viewport.YOffset()
	if c.slow || c.lines == nil || w <= 0 || h <= 0 {
		return m.viewport.View()
	}
	key := [4]int{c.gen, w, h, y}
	if c.outGood && c.key == key {
		return c.out
	}
	var b strings.Builder
	for i := 0; i < h; i++ {
		line := ""
		if j := y + i; j >= 0 && j < len(c.lines) {
			line = c.lines[j]
		}
		lw := xansi.StringWidth(line)
		if lw > w {
			return m.toSlow()
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", w-lw))
	}
	c.key, c.out, c.outGood = key, b.String(), true
	return c.out
}

// inputFastCache 缓存输入框画面：内容、光标、尺寸、焦点、主题都不变时不必重画。
type inputFastCache struct {
	key inputKey
	out string
	ok  bool
}

type inputKey struct {
	value                string
	row, col, curX, curY int
	width, height        int
	margin, style        int
	focused              bool
}

func (m *Model) inputCacheKey() inputKey {
	li := m.input.LineInfo()
	k := inputKey{
		value:   m.input.Value(),
		row:     m.input.Line(),
		col:     li.StartColumn + li.ColumnOffset,
		width:   m.input.Width(),
		height:  m.input.Height(),
		margin:  m.columnMargin(),
		style:   styleGen,
		focused: m.input.Focused(),
	}
	if c := m.input.Cursor(); c != nil { // 含滚动偏移
		k.curX, k.curY = c.X, c.Y
	}
	return k
}
