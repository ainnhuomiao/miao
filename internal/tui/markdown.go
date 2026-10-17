package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

var (
	mdRenderer *glamour.TermRenderer
	mdWidth    int
	mdDark     = true
)

// initMarkdown 在程序启动前探测一次背景色，避免在 Bubble Tea 运行中查询终端。
func initMarkdown() { mdDark = lipgloss.HasDarkBackground() }

// renderMarkdown 用 Glamour 渲染 Markdown；失败时退回纯文本折行。
func renderMarkdown(s string, width int) string {
	if mdRenderer == nil || mdWidth != width {
		cfg := styles.DarkStyleConfig
		if !mdDark {
			cfg = styles.LightStyleConfig
		}
		zero := uint(0)
		cfg.Document.Margin = &zero
		cfg.Document.BlockPrefix = ""
		cfg.Document.BlockSuffix = ""
		cfg.Document.StylePrimitive = ansi.StylePrimitive{}
		cfg.Code.StylePrimitive.Prefix = ""
		cfg.Code.StylePrimitive.Suffix = ""
		r, err := glamour.NewTermRenderer(glamour.WithStyles(cfg), glamour.WithWordWrap(width))
		if err != nil {
			return wrapText(s, width)
		}
		mdRenderer, mdWidth = r, width
	}
	out, err := mdRenderer.Render(s)
	if err != nil {
		return wrapText(s, width)
	}
	return strings.Trim(out, "\n")
}
