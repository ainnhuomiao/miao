package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestPaletteNotOpenMidText(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	typeText(m, "看看 /new")
	if m.paletteOpen() {
		t.Fatal("/ 不在开头时不应弹菜单")
	}
}

func TestPaletteClosesAfterArgOrBackspace(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	typeText(m, "/load abc")
	if m.paletteOpen() {
		t.Fatal("输入参数后菜单应收起")
	}
	m.input.Reset()
	typeText(m, "/")
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.paletteOpen() {
		t.Fatal("删掉 / 后菜单应收起")
	}
}

func TestPaletteUpWrapsToLast(t *testing.T) {
	m := newTestModel(t, 80, 30, "ok")
	typeText(m, "/")
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	all := m.paletteMatches()
	if m.palette.sel != len(all)-1 {
		t.Fatalf("在第一项按 ↑ 应跳到最后一项，sel=%d", m.palette.sel)
	}
}

func TestPaletteSelectionClampedWhenFilterNarrows(t *testing.T) {
	m := newTestModel(t, 80, 30, "ok")
	typeText(m, "/")
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // 选中最后一项
	typeText(m, "ne")                          // 只剩 /new
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("筛选变窄后按 Tab 不应崩溃：%v", r)
		}
	}()
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := m.input.Value(); got != "/new " {
		t.Fatalf("Tab 补全得到 %q，想要 \"/new \"", got)
	}
}

func TestPaletteReopensAfterEscWhenTyping(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	typeText(m, "/s")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.paletteOpen() {
		t.Fatal("Esc 后菜单应关闭")
	}
	typeText(m, "e")
	if !m.paletteOpen() {
		t.Fatal("Esc 后继续打字菜单应重新出现")
	}
}

func TestPaletteFitsWidth(t *testing.T) {
	for _, w := range []int{40, 80, 160} {
		m := newTestModel(t, w, 24, "ok")
		typeText(m, "/")
		for i, l := range strings.Split(m.View().Content, "\n") {
			if lw := lipgloss.Width(l); lw > w {
				t.Errorf("宽 %d：第 %d 行宽 %d 超出", w, i, lw)
			}
		}
	}
}
