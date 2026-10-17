package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newThemeModel(t *testing.T) *Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	setTheme("auto")
	t.Cleanup(func() { setTheme("auto") })
	return newTestModel(t, 100, 30, "ok")
}

func savedConfig(t *testing.T) map[string]any {
	t.Helper()
	p, _ := configPath()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestAllThemesFillEverySlot(t *testing.T) {
	if len(themes) != 8 {
		t.Fatalf("应有 8 个主题，got %d", len(themes))
	}
	for _, th := range themes {
		r := resolveTheme(th)
		for i, h := range []string{r.accent, r.user, r.agent, r.tool, r.error_, r.text, r.muted, r.surface0, r.surface1, r.crust, r.mantle, r.subtext0, r.yellow} {
			if len(h) != 7 || h[0] != '#' {
				t.Errorf("%s 第 %d 个配色槽无效：%q", th.name, i, h)
			}
		}
	}
	if _, ok := findTheme("Frappé"); !ok {
		t.Error("Frappé 应能匹配 frappe")
	}
}

func TestThemeCommandSwitchesAndSaves(t *testing.T) {
	m := newThemeModel(t)
	p, _ := configPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"other":1}`), 0o644)

	m.runCommand("/theme nord")
	if currentTheme != "nord" || accentColor.Dark != "#b48ead" {
		t.Fatalf("theme=%s accent=%v", currentTheme, accentColor)
	}
	if fg := titleStyle.GetForeground(); fg != accentColor {
		t.Fatal("样式应随主题重建")
	}
	cfg := savedConfig(t)
	if cfg["theme"] != "nord" || cfg["other"] != float64(1) {
		t.Fatalf("保存应只改 theme：%v", cfg)
	}
	setTheme("auto")
	loadSavedTheme()
	if currentTheme != "nord" {
		t.Fatal("启动时应读回保存的主题")
	}
	m.runCommand("/theme 不存在")
	if currentTheme != "nord" || !strings.Contains(m.messages[len(m.messages)-1].content, "没有这个主题") {
		t.Fatal("无效主题应提示且不改变")
	}
}

func TestThemeMenuPreviewAndEsc(t *testing.T) {
	m := newThemeModel(t)
	typeText(m, "/theme ")
	if !m.paletteOpen() {
		t.Fatal("/theme 后应弹出主题菜单")
	}
	got := m.paletteMatches()
	if got[0].name != "auto" || !strings.Contains(got[0].desc, "当前") {
		t.Fatalf("当前主题应排第一：%v", got[0])
	}
	if v := stripANSI(m.View()); !strings.Contains(v, "●●●●") || !strings.Contains(v, "Tokyo Night") {
		t.Fatal("菜单应显示色块和主题名")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if currentTheme != "mocha" {
		t.Fatalf("↓ 应实时预览，got %s", currentTheme)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if currentTheme != "auto" {
		t.Fatalf("Esc 应恢复原主题，got %s", currentTheme)
	}
	if p, _ := configPath(); fileExists(p) {
		t.Fatal("预览不应保存")
	}
}

func TestThemeMenuEnterConfirms(t *testing.T) {
	m := newThemeModel(t)
	typeText(m, "/theme grv")
	if got := m.paletteMatches(); len(got) != 1 || got[0].name != "gruvbox" {
		t.Fatalf("matches=%v", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if currentTheme != "gruvbox" || savedConfig(t)["theme"] != "gruvbox" || m.input.Value() != "" {
		t.Fatalf("Enter 应确认并保存，theme=%s input=%q", currentTheme, m.input.Value())
	}
}

func TestThemePreviewRevertsWhenLeavingMenu(t *testing.T) {
	m := newThemeModel(t)
	typeText(m, "/theme ")
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	for range "/theme " {
		m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if currentTheme != "auto" {
		t.Fatalf("删掉 /theme 后应恢复，got %s", currentTheme)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
