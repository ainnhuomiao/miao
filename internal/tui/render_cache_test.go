package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ainnhuomiao/miao/internal/agent"
)

// 缓存路径与原始整段渲染（MarginLeft(...).Render(renderTranscript())）逐字节一致，
// 并在各种修改后正确失效。
func TestTranscriptCacheMatchesSlowPath(t *testing.T) {
	t.Cleanup(func() { setTheme("auto") })

	m := newBenchModel(t, 120, 40)
	check := func(step string) {
		t.Helper()
		got := m.transcriptContent()
		want := m.slowTranscriptContent(m.columnMargin())
		if got != want {
			t.Fatalf("%s: 缓存渲染与原实现不一致（len %d vs %d）", step, len(got), len(want))
		}
	}
	check("空会话")
	fillHistory(m, 60)
	check("首次")
	check("命中缓存")
	m.messages[5].content += " 改过"
	check("编辑中间消息")
	m.messages[3].toolResult = "x\ny"
	check("改工具结果")
	m.messages = m.messages[:20]
	check("回退截断")
	startMidStream(m)
	check("流式中")
	m.Update(streamEventMsg{kind: agent.EventDelta, content: "更多"})
	check("delta")
	m.showReasoning = true
	check("展开思考")
	setTheme("latte")
	check("换主题")
	setTheme("mocha")
	check("换回主题")
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	check("改宽度")
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	check("宽屏居中边距")
	m.messages = m.messages[:1]
	check("清空")
	m.messages = nil
	check("无消息")
}

// 流式 delta 合并刷新：delta 不立刻重绘，tick 到达后刷新；结束事件立刻刷新且保持贴底。
func TestStreamRefreshCoalesced(t *testing.T) {
	m := newBenchModel(t, 100, 30)
	fillHistory(m, 40)
	m.messages = append(m.messages, chatMessage{role: roleUser, content: "继续", done: true}, chatMessage{role: roleAssistant})
	m.streaming, m.streamStart = true, time.Now()
	m.events = make(chan streamEventMsg, 1)
	m.refreshViewport(true)

	_, cmd := m.Update(streamEventMsg{kind: agent.EventDelta, content: "第一段ABC"})
	if !m.renderDirty || !m.flushPending || cmd == nil {
		t.Fatal("delta 应该标记待刷新并排一个 tick")
	}
	m.Update(streamEventMsg{kind: agent.EventDelta, content: "第二段XYZ"})
	if containsLine(fullViewport(m), "XYZ") {
		t.Fatal("tick 之前不应重绘")
	}
	m.Update(renderFlushMsg{})
	if m.renderDirty || m.flushPending || !containsLine(fullViewport(m), "第一段ABC第二段XYZ") {
		t.Fatal("tick 后应刷新出全部增量")
	}
	if !m.viewport.AtBottom() {
		t.Fatal("流式时应保持贴底")
	}
	m.Update(streamEventMsg{kind: agent.EventDelta, content: "尾巴TAIL"})
	m.Update(streamEventMsg{done: true})
	if m.renderDirty || !containsLine(fullViewport(m), "TAIL") || !m.viewport.AtBottom() {
		t.Fatal("结束事件应立即冲刷且贴底")
	}
	m.Update(renderFlushMsg{}) // 迟到的 tick 无副作用
	if m.flushPending {
		t.Fatal("flushPending 应复位")
	}
}

func containsLine(s, sub string) bool { return strings.Contains(s, sub) }
