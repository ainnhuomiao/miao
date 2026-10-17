package tui

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
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

func TestActivityBarAboveInput(t *testing.T) {
	m := newTestModel(t, 120, 30, "ok")
	if !strings.Contains(m.activityView(), "工具待命") {
		t.Fatalf("没有工具调用时应显示待命占位，得到 %q", m.activityView())
	}
	for i := 0; i < 5; i++ {
		m.messages = append(m.messages, chatMessage{role: roleTool, toolName: fmt.Sprintf("t%d", i),
			toolArgs: `{"path":"."}`, toolResult: "a\nb", done: i < 4})
	}
	m.layout()
	rows := m.activityRows()
	if len(rows) != activityMaxRows || rows[len(rows)-1].toolName != "t4" {
		t.Fatalf("应显示最近 %d 条且最新在下：%+v", activityMaxRows, rows)
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 30 {
		t.Fatalf("整屏 %d 行，应刚好 30", len(lines))
	}
	for _, l := range lines {
		if lipgloss.Width(l) > 120 {
			t.Fatalf("行超宽：%q", l)
		}
	}
	// 状态条紧贴输入框上边框
	if !strings.Contains(ansi.Strip(lines[len(lines)-5]), "t4") {
		t.Fatalf("输入框上方应是最新工具行：%q", lines[len(lines)-5])
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
