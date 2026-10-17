package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestDayGroupAndStamp(t *testing.T) {
	now := time.Date(2026, 10, 17, 21, 0, 0, 0, time.Local)
	if dayGroup(now.Add(-time.Hour), now) != "今天" || dayGroup(now.Add(-24*time.Hour), now) != "昨天" || dayGroup(now.Add(-72*time.Hour), now) != "更早" {
		t.Fatal("dayGroup wrong")
	}
	if sessionStamp(now.Add(-72*time.Hour), now) != "10-14" || sessionStamp(now, now) != "21:00" {
		t.Fatal("stamp wrong")
	}
}

func TestSidebarToggleAndWidth(t *testing.T) {
	m := newTestModel(t, 120, 30, "ok")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if !m.side.open || !m.side.focus || !m.sidebarVisible() {
		t.Fatal("ctrl+b should open and focus sidebar")
	}
	v := m.View()
	for i, line := range strings.Split(v, "\n") {
		if w := lipgloss.Width(line); w > 120 {
			t.Fatalf("line %d too wide: %d", i, w)
		}
	}
	if !strings.Contains(v, "会话") || !strings.Contains(v, "│") {
		t.Fatal("sidebar not rendered")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.side.focus || !m.side.open {
		t.Fatal("esc should only return focus")
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.sidebarVisible() || m.viewport.Width != 100 {
		t.Fatal("should hide under 110 cols")
	}
}
