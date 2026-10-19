package tui

import (
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/agent"
)

func TestCompactDoneNotice(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.streaming, m.compacting, m.ctxTokens = true, true, 54000
	m.handleCompactDone(compactDoneMsg{res: agent.CompactResult{Before: 50000, After: 8000}})
	if m.streaming || m.compacting || m.ctxTokens != 8000 {
		t.Fatalf("压缩结束后状态不对：%v %v %d", m.streaming, m.compacting, m.ctxTokens)
	}
	if last := m.messages[len(m.messages)-1].content; !strings.Contains(last, "已压缩 54k → 8k") {
		t.Fatalf("提示不对：%q", last)
	}
}

func TestNeedAutoCompact(t *testing.T) {
	t.Setenv("MIAO_CONTEXT_WINDOW", "100000")
	m := newTestModel(t, 100, 30, "ok")
	m.ctxTokens = 79000
	if m.needAutoCompact() {
		t.Fatal("79% 不应压缩")
	}
	m.ctxTokens = 80000
	if !m.needAutoCompact() {
		t.Fatal("80% 应自动压缩")
	}
}

func TestConfigurableAutoCompactThreshold(t *testing.T) {
	t.Setenv("MIAO_CONTEXT_WINDOW", "100000")
	m := newTestModel(t, 100, 30, "ok")
	m.options.AutoCompactPercent = 65
	m.ctxTokens = 64999
	if m.needAutoCompact() {
		t.Fatal("低于配置阈值不应自动压缩")
	}
	m.ctxTokens = 65000
	if !m.needAutoCompact() {
		t.Fatal("达到配置阈值应自动压缩")
	}
	m.options.AutoCompactPercent = 0
	m.ctxTokens = 100000
	if m.needAutoCompact() {
		t.Fatal("阈值为 0 时关闭自动压缩")
	}
	m.options.AutoCompactPercent = 65
	m.handleCompactDone(compactDoneMsg{auto: true, res: agent.CompactResult{Before: 65000, After: 8000, Summarized: true}})
	if last := m.messages[len(m.messages)-1].content; !strings.Contains(last, "上下文已用 65%，自动压缩") {
		t.Fatalf("提示应显示真实阈值：%q", last)
	}
}

func TestThinkingShowsCompacting(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.streaming, m.compacting = true, true
	if !strings.Contains(stripANSI(m.thinkingView()), "压缩上下文") {
		t.Fatalf("压缩时工具行应显示压缩上下文：%q", m.thinkingView())
	}
}
