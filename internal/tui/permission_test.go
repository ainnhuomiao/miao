package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"agnet/internal/agent"
)

func TestShiftTabCyclesModeAndBarShowsIt(t *testing.T) {
	m := newTestModel(t, 120, 30, "x")
	if !strings.Contains(stripANSI(m.View()), "每次询问") {
		t.Fatal("底栏应显示当前模式")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.ag.Mode() != agent.ModeAcceptEdits {
		t.Fatalf("mode=%v", m.ag.Mode())
	}
	v := stripANSI(m.View())
	if !strings.Contains(v, "自动同意编辑") {
		t.Fatalf("切换后底栏/提示应出现新模式:\n%s", v)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.ag.Mode() != agent.ModeAsk {
		t.Fatal("三次后应回到每次询问")
	}
}

func TestApprovalAlwaysAllow(t *testing.T) {
	m := newTestModel(t, 120, 30, "x")
	resp := make(chan bool, 1)
	m.Update(approvalRequestMsg{toolName: "edit_file", display: "a.go", args: json.RawMessage(`{"path":"a.go"}`), resp: resp})
	if !strings.Contains(stripANSI(m.View()), "A 本会话总是允许") {
		t.Fatal("审批卡应提示 A")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if got := <-resp; !got || m.approval != nil {
		t.Fatal("按 A 应允许并关闭卡片")
	}
}
