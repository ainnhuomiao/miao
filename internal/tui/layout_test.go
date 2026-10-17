package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestInputHeightGrowsAndClamps(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  int
	}{
		{"空", "", 1},
		{"一行", "hello", 1},
		{"三行", "a\nb\nc", 3},
		{"六行", strings.Repeat("x\n", 5) + "x", 6},
		{"超过六行封顶", strings.Repeat("x\n", 20), inputMaxLines},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newTestModel(t, 80, 30, "ok")
			m.input.SetValue(c.value)
			m.fitInput()
			if got := m.inputHeight(); got != c.want {
				t.Fatalf("inputHeight=%d，想要 %d", got, c.want)
			}
			if m.input.Height() != c.want {
				t.Fatalf("textarea 高度=%d，想要 %d", m.input.Height(), c.want)
			}
		})
	}
}

func TestInputSoftWrapCountsLines(t *testing.T) {
	m := newTestModel(t, 40, 30, "ok")
	w := m.input.Width()
	m.input.SetValue(strings.Repeat("a", w*2+1))
	if got := m.inputHeight(); got != 3 {
		t.Fatalf("软换行后 inputHeight=%d，想要 3（宽度 %d）", got, w)
	}
}

func TestInputShrinksAfterSubmit(t *testing.T) {
	m := newTestModel(t, 80, 30, "ok")
	m.input.SetValue("a\nb\nc\nd")
	m.fitInput()
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	collectStream(t, m)
	if m.input.Height() != 1 {
		t.Fatalf("发送后输入框高度=%d，想要 1", m.input.Height())
	}
}

func TestViewStillFitsWithTallInput(t *testing.T) {
	m := newTestModel(t, 80, 24, "ok")
	m.input.SetValue(strings.Repeat("x\n", 10))
	m.fitInput()
	if n := len(strings.Split(m.View(), "\n")); n != 24 {
		t.Fatalf("输入框长高后渲染 %d 行，想要 24", n)
	}
}

func TestColumnWidthAndCentering(t *testing.T) {
	cases := []struct {
		width      int
		wantCol    int
		wantMargin int
	}{
		{60, 60, 0},
		{columnMax, columnMax, 0},
		{140, columnMax, (140 - columnMax) / 2},
		{200, columnMax, (200 - columnMax) / 2},
	}
	for _, c := range cases {
		m := newTestModel(t, c.width, 30, "ok")
		if got := m.columnWidth(); got != c.wantCol {
			t.Errorf("宽 %d：columnWidth=%d，想要 %d", c.width, got, c.wantCol)
		}
		if got := m.columnMargin(); got != c.wantMargin {
			t.Errorf("宽 %d：columnMargin=%d，想要 %d", c.width, got, c.wantMargin)
		}
		for i, l := range strings.Split(m.View(), "\n") {
			if lw := lipgloss.Width(l); lw > c.width {
				t.Errorf("宽 %d：第 %d 行宽 %d 超出", c.width, i, lw)
			}
		}
	}
}

func TestDrawerPanelAboveInput(t *testing.T) {
	for _, h := range []int{16, 30, 50} {
		m := newTestModel(t, 140, h, "ok")
		m.drawerOpen = true
		m.layout()
		if m.viewport.Width != 140 {
			t.Fatalf("高 %d：面板打开后对话区应占满宽度，得到 %d", h, m.viewport.Width)
		}
		if m.drawerViewport.Width != m.columnWidth()-4 {
			t.Fatalf("高 %d：面板宽=%d，想要和输入框同宽 %d", h, m.drawerViewport.Width, m.columnWidth()-4)
		}
		if ph := m.drawerViewport.Height; ph < 3 || ph > drawerMaxLines {
			t.Fatalf("高 %d：面板高=%d 超出 3..%d", h, ph, drawerMaxLines)
		}
		lines := strings.Split(m.View(), "\n")
		if len(lines) != h {
			t.Fatalf("高 %d：整屏 %d 行，应刚好占满", h, len(lines))
		}
		// 面板在输入框上方：倒数第 4 行（输入框上边框之上）是面板下边框。
		if !strings.Contains(lines[len(lines)-5], "╰") {
			t.Fatalf("高 %d：输入框上方不是面板：%q", h, lines[len(lines)-5])
		}
	}
}

func TestInputBorderMutedWhenEmpty(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := newTestModel(t, 80, 24, "ok")
	top := func(v string) string { m.input.SetValue(v); return firstLine(m.inputView()) }
	empty, blank, typed := top(""), top("   "), top("hi")
	if empty == typed {
		t.Fatal("有内容时顶边框颜色应和空输入不同")
	}
	if empty != blank {
		t.Fatal("纯空白输入应和空输入一样用灰色边框")
	}
}
