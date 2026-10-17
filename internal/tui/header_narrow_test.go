package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestHeaderNarrowKeepsPercent(t *testing.T) {
	t.Setenv("AGNET_CONTEXT_WINDOW", "")
	for w := 30; w <= 90; w++ {
		m := &Model{modelName: "deepseek-flash", width: w, ctxTokens: 54000}
		line := strings.SplitN(m.headerView(), "\n", 2)[0]
		if lipgloss.Width(line) > w {
			t.Fatalf("宽 %d 时顶栏溢出：%q", w, line)
		}
		if !strings.Contains(line, "42%") {
			t.Fatalf("宽 %d 时百分比不该消失：%q", w, line)
		}
	}
}
