package tui

import (
	"os"
	"strings"
	"unicode/utf8"

	catppuccin "github.com/catppuccin/go"
	"github.com/charmbracelet/lipgloss"
)

// ctp 取 Catppuccin 色：深色终端用 Mocha，浅色终端用 Latte。
func ctp(pick func(catppuccin.Flavor) catppuccin.Color) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: pick(catppuccin.Latte).Hex, Dark: pick(catppuccin.Mocha).Hex}
}

var (
	accentColor   = ctp(catppuccin.Flavor.Mauve)
	userColor     = ctp(catppuccin.Flavor.Blue)
	agentColor    = ctp(catppuccin.Flavor.Green)
	toolColor     = ctp(catppuccin.Flavor.Peach)
	errorColor    = ctp(catppuccin.Flavor.Red)
	textColor     = ctp(catppuccin.Flavor.Text)
	mutedColor    = ctp(catppuccin.Flavor.Overlay0)
	surface0Color = ctp(catppuccin.Flavor.Surface0)
	crustColor    = ctp(catppuccin.Flavor.Crust)
	mantleColor   = ctp(catppuccin.Flavor.Mantle)
	surface1Color = ctp(catppuccin.Flavor.Surface1)

	// nerdFont 为真时底栏用 powerline 箭头分段；设置 AGNET_NERD_FONT=1 开启。
	nerdFont = os.Getenv("AGNET_NERD_FONT") == "1"
)

const logoArt = "▄▀█ █▀▀ █▄ █ █▀▀ ▀█▀\n█▀█ █▄█ █ ▀█ ██▄  █ "

// gradientLogo 把 logo 按列从 mauve 渐变到 blue。
func gradientLogo() string {
	from, to := catppuccin.Mocha.Mauve().RGB, catppuccin.Mocha.Blue().RGB
	if !mdDark {
		from, to = catppuccin.Latte.Mauve().RGB, catppuccin.Latte.Blue().RGB
	}
	lines := strings.Split(logoArt, "\n")
	w := 0
	for _, l := range lines {
		w = max(w, utf8.RuneCountInString(l))
	}
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		x := 0
		for _, r := range l {
			t := float64(x) / float64(max(1, w-1))
			c := lipgloss.Color(lerpHex(from, to, t))
			b.WriteString(lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(r)))
			x++
		}
		b.WriteString(strings.Repeat(" ", w-x)) // 补齐宽度，居中时各行对齐
	}
	return b.String()
}

func lerpHex(a, b [3]uint8, t float64) string {
	const hex = "0123456789ABCDEF"
	out := []byte{'#'}
	for i := 0; i < 3; i++ {
		v := uint8(float64(a[i]) + (float64(b[i])-float64(a[i]))*t)
		out = append(out, hex[v>>4], hex[v&15])
	}
	return string(out)
}
