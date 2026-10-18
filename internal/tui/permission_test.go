package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ainnhuomiao/miao/internal/agent"
)

func TestShiftTabCyclesModeAndBarShowsIt(t *testing.T) {
	m := newTestModel(t, 120, 30, "x")
	if !strings.Contains(stripANSI(m.View().Content), "每次询问") {
		t.Fatal("底栏应显示当前模式")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.ag.Mode() != agent.ModeAcceptEdits {
		t.Fatalf("mode=%v", m.ag.Mode())
	}
	v := stripANSI(m.View().Content)
	if !strings.Contains(v, "自动同意编辑") {
		t.Fatalf("切换后底栏/提示应出现新模式:\n%s", v)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.ag.Mode() != agent.ModeAsk {
		t.Fatal("三次后应回到每次询问")
	}
}

func TestApprovalAlwaysAllow(t *testing.T) {
	m := newTestModel(t, 120, 30, "x")
	resp := make(chan bool, 1)
	m.Update(approvalRequestMsg{toolName: "edit_file", display: "a.go", args: json.RawMessage(`{"path":"a.go"}`), resp: resp})
	if !strings.Contains(stripANSI(m.View().Content), "A 本会话总是允许") {
		t.Fatal("审批卡应提示 A")
	}
	m.Update(tea.KeyPressMsg{Code: 'a', Text: string('a')})
	if got := <-resp; !got || m.approval != nil {
		t.Fatal("按 A 应允许并关闭卡片")
	}
}

func TestApprovalBatchThisRound(t *testing.T) {
	m := newTestModel(t, 120, 30, "x")
	resp := make(chan bool, 1)
	m.Update(approvalRequestMsg{toolName: "write_file", display: "a.go\n+ x", args: json.RawMessage(`{"path":"a.go"}`), resp: resp})
	if !strings.Contains(stripANSI(m.View().Content), "B 本轮全部写入") {
		t.Fatal("文件审批卡应提示 B")
	}
	m.Update(tea.KeyPressMsg{Code: 'b', Text: string('b')})
	if got := <-resp; !got || m.approval != nil {
		t.Fatal("按 B 应允许并关闭卡片")
	}
}
