package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	catppuccin "github.com/catppuccin/go"
	"github.com/charmbracelet/lipgloss"

	"github.com/ainnhuomiao/miao/internal/appdir"
)

// theme 是一套主题：界面代码只引用这些配色槽，不直接写颜色名。
type theme struct {
	name, title                                         string
	dark                                                bool
	accent, user, agent, tool, error_, text, muted      string
	surface0, surface1, crust, mantle, subtext0, yellow string
	base                                                string // 铺底色用
}

func ctpTheme(name, title string, f catppuccin.Flavor, dark bool) theme {
	return theme{
		name: name, title: title, dark: dark,
		accent: f.Mauve().Hex, user: f.Blue().Hex, agent: f.Green().Hex, tool: f.Peach().Hex,
		error_: f.Red().Hex, text: f.Text().Hex, muted: f.Overlay0().Hex,
		surface0: f.Surface0().Hex, surface1: f.Surface1().Hex, crust: f.Crust().Hex,
		mantle: f.Mantle().Hex, base: f.Base().Hex, subtext0: f.Subtext0().Hex, yellow: f.Yellow().Hex,
	}
}

// themes 按菜单顺序排列；auto 在运行时解析成 Mocha 或 Latte。
var themes = []theme{
	{name: "auto", title: "跟随终端（深色 Mocha / 浅色 Latte）"},
	ctpTheme("mocha", "Catppuccin Mocha", catppuccin.Mocha, true),
	ctpTheme("macchiato", "Catppuccin Macchiato", catppuccin.Macchiato, true),
	ctpTheme("frappe", "Catppuccin Frappé", catppuccin.Frappe, true),
	ctpTheme("latte", "Catppuccin Latte", catppuccin.Latte, false),
	{name: "tokyonight", title: "Tokyo Night", dark: true,
		accent: "#bb9af7", user: "#7aa2f7", agent: "#9ece6a", tool: "#ff9e64", error_: "#f7768e",
		text: "#c0caf5", muted: "#565f89", surface0: "#292e42", surface1: "#3b4261",
		crust: "#16161e", mantle: "#1f2335", base: "#1a1b26", subtext0: "#a9b1d6", yellow: "#e0af68"},
	{name: "gruvbox", title: "Gruvbox Dark", dark: true,
		accent: "#d3869b", user: "#83a598", agent: "#b8bb26", tool: "#fe8019", error_: "#fb4934",
		text: "#ebdbb2", muted: "#928374", surface0: "#3c3836", surface1: "#504945",
		crust: "#1d2021", mantle: "#282828", base: "#282828", subtext0: "#d5c4a1", yellow: "#fabd2f"},
	{name: "nord", title: "Nord", dark: true,
		accent: "#b48ead", user: "#81a1c1", agent: "#a3be8c", tool: "#d08770", error_: "#bf616a",
		text: "#eceff4", muted: "#616e88", surface0: "#3b4252", surface1: "#434c5e",
		crust: "#242933", mantle: "#2e3440", base: "#2e3440", subtext0: "#d8dee9", yellow: "#ebcb8b"},
}

var (
	// termDark 是启动时探测到的终端背景（auto 主题据此选 Mocha/Latte）。
	termDark = true
	// currentTheme 是当前生效（含预览中）的主题名。
	currentTheme = "auto"
)

// 配色槽，由 setTheme 赋值。
var (
	accentColor, userColor, agentColor, toolColor, errorColor, textColor, mutedColor     lipgloss.AdaptiveColor
	surface0Color, crustColor, mantleColor, surface1Color, subtext0Color, pacYellowColor lipgloss.AdaptiveColor
)

// 由 buildStyles 按当前配色生成的样式。
var (
	titleStyle, userLabelStyle, agentLabelStyle, toolLabelStyle, mutedStyle, errorStyle, promptStyle lipgloss.Style
	inputBoxStyle, drawerBorderStyle, drawerBoxStyle, userTagStyle, agentTagStyle                    lipgloss.Style
	segAccentStyle, segStyle, approvalBoxStyle, diffAddStyle, diffDelStyle                           lipgloss.Style
)

func init() { setTheme("auto") }

// findTheme 返回名字对应的主题（大小写、é 不敏感）；auto 解析成具体配色。
func findTheme(name string) (theme, bool) {
	n := strings.NewReplacer("é", "e", " ", "", "-", "", "_", "").Replace(strings.ToLower(strings.TrimSpace(name)))
	for _, t := range themes {
		if t.name == n {
			return resolveTheme(t), true
		}
	}
	return theme{}, false
}

func resolveTheme(t theme) theme {
	if t.name != "auto" {
		return t
	}
	r := ctpTheme("auto", t.title, catppuccin.Latte, false)
	if termDark {
		r = ctpTheme("auto", t.title, catppuccin.Mocha, true)
	}
	return r
}

// setTheme 切换配色并重建所有样式；名字无效返回 false。
func setTheme(name string) bool {
	t, ok := findTheme(name)
	if !ok {
		return false
	}
	c := func(hex string) lipgloss.AdaptiveColor { return lipgloss.AdaptiveColor{Light: hex, Dark: hex} }
	accentColor, userColor, agentColor, toolColor = c(t.accent), c(t.user), c(t.agent), c(t.tool)
	errorColor, textColor, mutedColor = c(t.error_), c(t.text), c(t.muted)
	surface0Color, surface1Color, crustColor, mantleColor = c(t.surface0), c(t.surface1), c(t.crust), c(t.mantle)
	subtext0Color, pacYellowColor = c(t.subtext0), c(t.yellow)
	currentTheme = t.name
	if mdDark != t.dark {
		mdDark, mdRenderer = t.dark, nil // Markdown 跟着换深/浅色样式
	}
	buildStyles()
	syncFill()
	return true
}

// ---- 铺底色：用 OSC 11 把终端默认背景设成主题 base ----
// 不给每个样式加 Background：样式结尾的 \x1b[0m 会把背景重置掉，有字处反而露洞。
// 改终端默认背景色则所有格子（含重置后的）都用 base，退出时用 OSC 111 恢复。

var (
	fillBg    bool      // 用户设置，默认关
	fillOut   io.Writer // Run 时设为终端；测试里为 nil，不输出转义
	fillShown bool      // 是否已改过终端背景，需要恢复
)

func syncFill() {
	if fillOut == nil {
		return
	}
	if fillBg {
		t, _ := findTheme(currentTheme)
		fmt.Fprintf(fillOut, "\x1b]11;%s\x1b\\", t.base)
		fillShown = true
	} else if fillShown {
		resetFill()
	}
}

// resetFill 恢复终端原来的背景色。
func resetFill() {
	if fillOut != nil && fillShown {
		fmt.Fprint(fillOut, "\x1b]111\x1b\\")
		fillShown = false
	}
}

func buildStyles() {
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	userLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(userColor)
	agentLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(agentColor)
	toolLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(toolColor)
	mutedStyle = lipgloss.NewStyle().Foreground(mutedColor)
	errorStyle = lipgloss.NewStyle().Foreground(errorColor)
	promptStyle = lipgloss.NewStyle().Foreground(accentColor)
	inputBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accentColor).Padding(0, 1)
	drawerBorderStyle = lipgloss.NewStyle().Foreground(toolColor)
	drawerBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(toolColor).Padding(0, 1)
	userTagStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(userColor).Background(surface0Color)
	agentTagStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(agentColor).Background(surface0Color)
	segAccentStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(crustColor).Background(accentColor)
	segStyle = lipgloss.NewStyle().Padding(0, 1).Foreground(textColor).Background(surface0Color)
	approvalBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accentColor).Padding(0, 1)
	diffAddStyle = lipgloss.NewStyle().Foreground(agentColor)
	diffDelStyle = lipgloss.NewStyle().Foreground(errorColor)
}

// gradientEnds 是 logo 渐变的起止色（accent → user）。
func gradientEnds() (from, to [3]uint8) {
	t, _ := findTheme(currentTheme)
	return hexRGB(t.accent), hexRGB(t.user)
}

func hexRGB(h string) [3]uint8 {
	var out [3]uint8
	h = strings.TrimPrefix(h, "#")
	for i := 0; i < 3 && len(h) >= 2*i+2; i++ {
		var v uint8
		for _, ch := range h[2*i : 2*i+2] {
			v <<= 4
			switch {
			case ch >= '0' && ch <= '9':
				v |= uint8(ch - '0')
			case ch >= 'a' && ch <= 'f':
				v |= uint8(ch-'a') + 10
			case ch >= 'A' && ch <= 'F':
				v |= uint8(ch-'A') + 10
			}
		}
		out[i] = v
	}
	return out
}

// themeSwatch 是菜单里主题名后的四个色块：accent、user、agent、tool。
func themeSwatch(t theme) string {
	t = resolveTheme(t)
	var b strings.Builder
	for _, h := range []string{t.accent, t.user, t.agent, t.tool} {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(h)).Render("●"))
	}
	return b.String()
}

// ---- 持久化：~/.config/miao/config.json 里的 "theme" 字段 ----

func configPath() (string, error) {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// loadSavedTheme 启动时应用保存的主题；文件缺失或主题无效时保持 auto。
func loadSavedTheme() {
	p, err := configPath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var cfg struct {
		Theme  string `json:"theme"`
		FillBg bool   `json:"fill_bg"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return
	}
	fillBg = cfg.FillBg
	if cfg.Theme != "" {
		setTheme(cfg.Theme)
	}
}

// saveTheme 只改 theme 字段，保留配置文件里的其它键。
func saveTheme(name string) error { return saveConfigKey("theme", name) }

// saveConfigKey 只改一个键，保留配置文件里的其它键。
func saveConfigKey(key string, val any) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	cfg := map[string]any{}
	if data, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(data, &cfg)
	}
	cfg[key] = val
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
