package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

var (
	mdRenderer *glamour.TermRenderer
	mdWidth    int
	mdCodeFG   string
	mdDark     = true
)

// initMarkdown 在程序启动前探测一次背景色，避免在 Bubble Tea 运行中查询终端。
func initMarkdown() { mdDark = lipgloss.HasDarkBackground() }

// renderMarkdown 用 Glamour 渲染 Markdown；失败时退回纯文本折行。
func renderMarkdown(s string, width int) string {
	codeFG := toolColor.Dark
	if !mdDark {
		codeFG = toolColor.Light
	}
	if mdRenderer == nil || mdWidth != width || mdCodeFG != codeFG {
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
		// 行内代码：主题 peach 色文字，不加底色。
		cfg.Code.StylePrimitive.Color = &codeFG
		cfg.Code.StylePrimitive.BackgroundColor = nil
		r, err := glamour.NewTermRenderer(glamour.WithStyles(cfg), glamour.WithWordWrap(width), glamour.WithChromaFormatter(chromaFormatterName))
		if err != nil {
			return wrapText(s, width)
		}
		mdRenderer, mdWidth, mdCodeFG = r, width, codeFG
	}
	out, err := mdRenderer.Render(s)
	if err != nil {
		return wrapText(s, width)
	}
	return clampWidth(strings.Trim(out, "\n"), width)
}

// clampWidth 保证每一行都不超过 width 显示列（中文算 2 列）。
// Glamour 只在空格处折行，连续的中文不会被折开，这里补一次按显示宽度的折行。
func clampWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	out := lines[:0:0]
	for _, ln := range lines {
		if xansi.StringWidth(ln) <= width {
			out = append(out, ln)
			continue
		}
		out = append(out, strings.Split(xansi.Wrap(ln, width, ""), "\n")...)
	}
	return strings.Join(out, "\n")
}
