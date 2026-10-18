package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/llm"
)

func TestApplyPlanRequiresPlanMode(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.applyPlan()
	if last := m.messages[len(m.messages)-1].content; !strings.Contains(last, "只读规划") {
		t.Fatalf("got %q", last)
	}
	m.ag.SetMode(agent.ModePlan)
	m.applyPlan()
	if last := m.messages[len(m.messages)-1].content; !strings.Contains(last, "还没有方案") {
		t.Fatalf("got %q", last)
	}
}

func TestShowCostEmpty(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.showCost()
	if last := m.messages[len(m.messages)-1].content; !strings.Contains(last, "还没有用量") {
		t.Fatalf("got %q", last)
	}
	m.usage = &llm.Usage{PromptTokens: 100, CompletionTokens: 20}
	m.costUSD = 0.012
	m.showCost()
	got := m.messages[len(m.messages)-1].content
	if !strings.Contains(got, "累计") || !strings.Contains(got, "$") {
		t.Fatalf("got %q", got)
	}
}

func TestHandleBangDoneRecordsHistory(t *testing.T) {
	m := newTestModel(t, 80, 24, "ok")
	m.handleBangDone(bangDoneMsg{cmd: "echo hi", out: "hi"})
	h := m.ag.History()
	if len(h) != 2 || h[0].Content != "!echo hi" || !strings.Contains(h[1].Content, "hi") {
		t.Fatalf("history=%+v", h)
	}
	if m.streaming || m.shelling {
		t.Fatal("结束后不应仍在执行")
	}
}

func TestHandleBangDoneCancelSkipsHistory(t *testing.T) {
	m := newTestModel(t, 80, 24, "ok")
	m.streaming, m.shelling = true, true
	m.handleBangDone(bangDoneMsg{cmd: "sleep 9", err: context.Canceled})
	if m.streaming || m.shelling {
		t.Fatal("取消后应清状态")
	}
	if len(m.ag.History()) != 0 {
		t.Fatalf("取消不应写入历史：%+v", m.ag.History())
	}
	if last := m.messages[len(m.messages)-1].content; !strings.Contains(last, "已取消") {
		t.Fatalf("got %q", last)
	}
}

func TestHelpListsNewCommands(t *testing.T) {
	for _, s := range []string{"/apply", "/cost", "/init", "/trust-project", "!"} {
		if !strings.Contains(helpText, s) {
			t.Fatalf("help missing %s", s)
		}
	}
}
