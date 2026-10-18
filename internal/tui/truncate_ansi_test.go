package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// 带颜色的字符串截断后不能留下半截转义序列，宽度也要准。
func TestTruncateWidthKeepsANSIIntact(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)

	s := "   " + lipgloss.NewStyle().Foreground(agentColor).Render("●") + " " +
		lipgloss.NewStyle().Foreground(textColor).Render("compact_test.go") + "  +2 −0"
	if !strings.Contains(s, "\x1b[") {
		t.Fatal("test string should carry ANSI color")
	}
	for w := 4; w < 30; w++ {
		got := truncateWidth(s, w)
		if lipgloss.Width(got) > w {
			t.Fatalf("width %d: got %d", w, lipgloss.Width(got))
		}
		// 每个 ESC[ 后面都必须以 m 收尾
		rest := got
		for {
			i := strings.Index(rest, "\x1b[")
			if i < 0 {
				break
			}
			j := strings.IndexByte(rest[i:], 'm')
			if j < 0 {
				t.Fatalf("width %d: dangling escape in %q", w, got)
			}
			rest = rest[i+j+1:]
		}
	}
}
