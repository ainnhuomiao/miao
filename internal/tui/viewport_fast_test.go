package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// 快速路径必须与 viewport.View() 逐字节相同（滚动、空内容、超宽回退）。
func TestViewportViewMatchesBubbles(t *testing.T) {
	m := newTestModel(t, 100, 30, "")
	fillHistory(m, 30)
	m.refreshViewport(true)
	for _, y := range []int{0, 3, 50, 1 << 20} {
		m.viewport.SetYOffset(y)
		if got, want := m.viewportView(), refViewport(m); got != want {
			t.Fatalf("y=%d 快速路径与 viewport.View 不同", y)
		}
	}
	m.setViewportContent("")
	if got, want := m.viewportView(), refViewport(m); got != want {
		t.Fatalf("空内容不同: %q vs %q", got, want)
	}
	m.setViewportContent("中文\x1b[31m红\x1b[m\n  缩进 ok")
	if got, want := m.viewportView(), refViewport(m); got != want {
		t.Fatalf("中文和颜色码不同: %q vs %q", got, want)
	}
	m.setViewportContent("tab\tx")
	if got, want := m.viewportView(), refViewport(m); got != want {
		t.Fatalf("含 tab 时应回退: %q vs %q", got, want)
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if got, want := m.viewportView(), refViewport(m); got != want {
		t.Fatal("改尺寸后缓存应失效")
	}
}

// refViewport 用真实内容渲染一个 bubbles viewport 作为对照。
func refViewport(m *Model) string {
	v := m.viewport
	y := v.YOffset()
	v.SetContent(m.vpFast.raw)
	v.SetYOffset(y)
	return v.View()
}

func TestViewportViewOverwideFallsBack(t *testing.T) {
	m := newTestModel(t, 60, 20, "")
	m.setViewportContent("short\n" + strings.Repeat("宽", 80))
	if got, want := m.viewportView(), refViewport(m); got != want {
		t.Fatalf("超宽行应回退到 viewport.View: %q vs %q", got, want)
	}
}
