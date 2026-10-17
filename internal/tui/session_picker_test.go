package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"agnet/internal/session"
)

func TestSessionPickerFilterAndRelTime(t *testing.T) {
	m := newTestModel(t, 120, 40, "ok")
	m.picker = pickerState{open: true, all: []session.Meta{
		{ID: "a", Title: "修复表格渲染"}, {ID: "b", Title: "子代理开关"}, {ID: "c", Title: "Nix flake"},
	}}
	for _, r := range "子代理" {
		m.sessionPickerKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	got := m.pickerMatches()
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("filter: %+v", got)
	}
	now := time.Now()
	if s := relTime(now.Add(-3*time.Hour), now); s != "3 小时前" {
		t.Fatalf("relTime: %q", s)
	}
	m.sessionPickerKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.picker.open {
		t.Fatal("esc should close")
	}
	if strings.Contains(helpText, "列出已保存会话") {
		t.Fatal("help not updated")
	}
}
