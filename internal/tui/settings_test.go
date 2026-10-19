package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ainnhuomiao/miao/internal/config"
)

func TestSettingsMenuPersistsWithoutChangingActiveSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MIAO_HOME", home)
	path := filepath.Join(home, "config.json")
	if err := os.WriteFile(path, []byte(`{"theme":"mocha","subagents":{"enabled":true,"timeout_seconds":45,"max_tool_rounds":8}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t, 100, 30, "ok")
	typeText(m, "/settings")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.input.Value() != "/settings " || len(m.paletteMatches()) != 11 || m.streaming {
		t.Fatal("settings should open a local feature menu")
	}
	m.input.SetValue("/settings sessions")
	m.syncPalette()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	user, err := config.LoadUserFile()
	if err != nil || user.SessionsEnabled == nil || *user.SessionsEnabled {
		t.Fatal("settings selection should persist an explicit false", err)
	}
	if m.options.DisableSessionStorage || m.store == nil || m.streaming || len(m.ag.History()) != 0 {
		t.Fatal("editing startup settings must preserve the active session and avoid model requests")
	}
	if last := m.messages[len(m.messages)-1].content; !strings.Contains(last, "重启后生效") {
		t.Fatalf("restart requirement must be clear: %s", last)
	}
	data, _ := os.ReadFile(path)
	var all map[string]json.RawMessage
	if json.Unmarshal(data, &all) != nil || all["theme"] == nil || !strings.Contains(string(all["subagents"]), "45") {
		t.Fatal("menu must preserve other saved sections")
	}
	m.runCommand("/settings bell off")
	user, _ = config.LoadUserFile()
	if user.BellEnabled == nil || *user.BellEnabled {
		t.Fatal("direct settings command should support explicit on/off")
	}
}

func TestSettingsCompletionScrollAndNarrowRendering(t *testing.T) {
	m := newTestModel(t, 80, 24, "ok")
	m.runCommand("/settings")
	for range 9 {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.palette.top == 0 {
		t.Fatal("long settings menu must scroll")
	}
	m.input.SetValue("/settings ima")
	m.syncPalette()
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "/settings images" {
		t.Fatal("settings should support Tab completion")
	}
	for _, size := range [][2]int{{36, 12}, {80, 24}, {140, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != size[1] {
			t.Fatalf("settings view height=%d, want %d", len(lines), size[1])
		}
		for _, line := range lines {
			if lipgloss.Width(line) > size[0] {
				t.Fatal("settings menu exceeds terminal width")
			}
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.paletteOpen() || len(m.ag.History()) != 0 {
		t.Fatal("Esc should close settings without sending input")
	}
}

func TestSettingsFailurePreservesBrokenFile(t *testing.T) {
	m := newTestModel(t, 80, 24, "ok")
	path, _ := config.UserConfigPath()
	const broken = `{broken`
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	m.runCommand("/settings images off")
	if data, _ := os.ReadFile(path); string(data) != broken {
		t.Fatal("settings must not replace a malformed file")
	}
	if m.streaming || !strings.Contains(m.messages[len(m.messages)-1].content, "失败") {
		t.Fatal("settings failure should be visible without a model request")
	}
}
