package tui

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

func TestMarkdownStaysInWidth(t *testing.T) {
	md := "3 个子代理已并行派出并全部返回，每个都有全新的上下文，只把结论带回给我。这一行很长很长很长很长很长很长很长很长很长很长很长很长很长很长。用 `go test` 跑。\n\n" +
		"| 子代理 | 结论 |\n|---|---|\n| 项目概览 | 这是一个用 Go 写的终端 agent，使用 Bubble Tea 和 lipgloss 构建界面，支持工具调用与审批卡片等等等等 |\n\n---\n"
	for _, w := range []int{30, 60, 100} {
		out := renderMarkdown(md, w)
		for _, ln := range strings.Split(out, "\n") {
			if got := xansi.StringWidth(ln); got > w {
				t.Fatalf("width %d: line is %d cols: %q", w, got, xansi.Strip(ln))
			}
		}
		if strings.Contains(out, "48;") {
			t.Fatalf("inline code should have no background")
		}
	}
}
