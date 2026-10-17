package tui

import "strings"

// themeMode 判断是否在 /theme 第二层菜单里；query 是已输入的主题名部分。
func (m *Model) themeMode() (query string, ok bool) {
	v := m.input.Value()
	if !strings.HasPrefix(v, "/theme ") || strings.ContainsAny(v, "\n") {
		return "", false
	}
	return strings.TrimLeft(strings.TrimPrefix(v, "/theme "), " \t"), true
}

// themeMatches 列出主题：已保存的主题排第一，其余按前缀、子序列筛选。
func (m *Model) themeMatches(query string) []paletteCmd {
	q := strings.ToLower(query)
	saved := m.savedTheme()
	var first, prefix, fuzzy []paletteCmd
	for _, t := range themes {
		c := paletteCmd{name: t.name, desc: t.title, swatch: themeSwatch(t)}
		if t.name == saved {
			c.desc += "（当前）"
		}
		switch {
		case q != "" && !strings.HasPrefix(t.name, q) && !isSubsequence(q, t.name):
			continue
		case t.name == saved:
			first = append(first, c)
		case q == "" || strings.HasPrefix(t.name, q):
			prefix = append(prefix, c)
		default:
			fuzzy = append(fuzzy, c)
		}
	}
	if q == "" || strings.HasPrefix(fillCmdName, q) {
		state := "关"
		if fillBg {
			state = "开"
		}
		fuzzy = append(fuzzy, paletteCmd{name: fillCmdName, desc: "铺底色：用主题底色盖住透明终端背后的内容（当前：" + state + "）"})
	}
	return append(append(first, prefix...), fuzzy...)
}

// savedTheme 是确认过的主题；预览期间 currentTheme 会临时变化。
func (m *Model) savedTheme() string {
	if m.themeOrig != "" {
		return m.themeOrig
	}
	return currentTheme
}

// previewTheme 用 ↑/↓ 移动时实时预览，记住原主题以便 Esc 恢复。
func (m *Model) previewTheme(name string) {
	if m.themeOrig == "" {
		m.themeOrig = currentTheme
	}
	if setTheme(name) {
		m.restyle()
	}
}

// cancelThemePreview 恢复预览前的主题。
func (m *Model) cancelThemePreview() {
	if m.themeOrig == "" {
		return
	}
	setTheme(m.themeOrig)
	m.themeOrig = ""
	m.restyle()
}

// applyTheme 确认切换并保存到配置文件。
func (m *Model) applyTheme(name string) {
	orig := m.themeOrig
	m.themeOrig = ""
	if strings.TrimSpace(strings.ToLower(name)) == fillCmdName {
		if orig != "" {
			setTheme(orig)
		}
		m.toggleFill()
		return
	}
	if !setTheme(name) {
		if orig != "" {
			setTheme(orig)
			m.restyle()
		}
		names := make([]string, len(themes))
		for i, t := range themes {
			names[i] = t.name
		}
		m.notice("没有这个主题：" + name + "。可选：" + strings.Join(names, "、"))
		return
	}
	m.restyle()
	if err := saveTheme(currentTheme); err != nil {
		m.notice("已切换到主题 " + currentTheme + "，但保存失败：" + err.Error())
		return
	}
	m.notice("已切换到主题 " + currentTheme + "。")
}

// restyle 把新配色同步到创建时就固定的组件样式，并重绘对话。
func (m *Model) restyle() {
	m.input.FocusedStyle.Prompt = promptStyle
	m.input.FocusedStyle.Placeholder = mutedStyle
	m.input.Cursor.Style = m.input.Cursor.Style.Foreground(accentColor)
	m.spinner.Style = m.spinner.Style.Foreground(accentColor)
	if m.width > 0 {
		m.layout()
		m.refreshViewport(false)
	}
}

// fillCmdName 是 /theme 菜单里的铺底色开关。
const fillCmdName = "fill"

// toggleFill 切换铺底色并保存。
func (m *Model) toggleFill() {
	fillBg = !fillBg
	syncFill()
	m.restyle()
	state := "已关闭铺底色。"
	if fillBg {
		state = "已打开铺底色。"
	}
	if err := saveConfigKey("fill_bg", fillBg); err != nil {
		m.notice(state + "但保存失败：" + err.Error())
		return
	}
	m.notice(state)
}
