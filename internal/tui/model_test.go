package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"agnet/internal/agent"
	"agnet/internal/llm"
)

type fakeClient struct {
	deltas []llm.Delta
	err    error
}

func (f *fakeClient) StreamChat(ctx context.Context, _ llm.ChatRequest, onDelta llm.DeltaHandler) (llm.ChatResult, error) {
	var content, reasoning strings.Builder
	for _, d := range f.deltas {
		select {
		case <-ctx.Done():
			return llm.ChatResult{}, ctx.Err()
		default:
		}
		content.WriteString(d.Content)
		reasoning.WriteString(d.Reasoning)
		if onDelta != nil {
			if err := onDelta(d); err != nil {
				return llm.ChatResult{}, err
			}
		}
	}
	if f.err != nil {
		return llm.ChatResult{}, f.err
	}
	return llm.ChatResult{
		Content:      content.String(),
		Reasoning:    reasoning.String(),
		FinishReason: "stop",
	}, nil
}

type blockingClient struct {
	started chan struct{}
}

func (b *blockingClient) StreamChat(ctx context.Context, _ llm.ChatRequest, _ llm.DeltaHandler) (llm.ChatResult, error) {
	close(b.started)
	<-ctx.Done()
	return llm.ChatResult{}, ctx.Err()
}

func newTestModel(t *testing.T, width, height int, reply string) *Model {
	t.Helper()
	ag := agent.New(&fakeClient{deltas: []llm.Delta{{Content: reply}}}, "测试助手", nil)
	m := New(ag, "test-model")
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

func collectStream(t *testing.T, m *Model) {
	t.Helper()
	events := m.events
	deadline := time.After(3 * time.Second)

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("事件通道在 done 之前关闭")
			}
			m.Update(ev)
			if ev.done {
				return
			}
		case <-deadline:
			t.Fatal("等待流式事件超时")
		}
	}
}

func TestViewFitsTerminal(t *testing.T) {
	sizes := []struct {
		width, height int
	}{
		{36, 12},
		{80, 24},
		{120, 40},
	}

	for _, size := range sizes {
		name := fmt.Sprintf("%dx%d", size.width, size.height)
		t.Run(name, func(t *testing.T) {
			m := newTestModel(t, size.width, size.height, "你好，这是一条测试回答。")
			view := m.View()

			lines := strings.Split(view, "\n")
			if len(lines) != size.height {
				t.Fatalf("渲染了 %d 行，想要 %d 行", len(lines), size.height)
			}
			for i, line := range lines {
				if w := lipgloss.Width(line); w > size.width {
					t.Errorf("第 %d 行宽度为 %d，超过终端宽度 %d：%q",
						i, w, size.width, line)
				}
			}
		})
	}
}

func TestSubmitStreamsIntoTranscript(t *testing.T) {
	m := newTestModel(t, 80, 24, "你好！")

	m.input.SetValue("hi")
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Fatal("发送后应返回等待事件的命令")
	}
	if !m.streaming {
		t.Fatal("发送后 streaming 应为 true")
	}

	collectStream(t, m)

	if m.streaming {
		t.Error("done 之后 streaming 仍为 true")
	}
	if len(m.messages) != 3 { // 欢迎提示 + 用户 + 助手
		t.Fatalf("消息数 = %d, 想要 3", len(m.messages))
	}
	if got := m.messages[2].content; got != "你好！" {
		t.Errorf("助手回答 = %q, 想要 %q", got, "你好！")
	}
	if !strings.Contains(m.View(), "你好！") {
		t.Error("界面上应显示完整回答")
	}
	if got := len(m.ag.History()); got != 2 {
		t.Errorf("Agent 历史长度 = %d, 想要 2", got)
	}
}

func TestEscCancelsStream(t *testing.T) {
	fc := &blockingClient{started: make(chan struct{})}
	m := New(agent.New(fc, "", nil), "test-model")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.input.SetValue("请写一篇很长的文章")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case <-fc.started:
	case <-time.After(2 * time.Second):
		t.Fatal("请求没有开始")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	collectStream(t, m)

	if m.streaming {
		t.Error("取消后 streaming 应变为 false")
	}
	if !strings.Contains(m.View(), "已取消") {
		t.Error("界面上应提示已取消")
	}
}

func TestTabIsNoop(t *testing.T) {
	m := newTestModel(t, 100, 24, "")
	before := m.View()
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.drawerOpen || m.View() != before {
		t.Fatal("工具状态条常驻后 Tab 不应改变界面")
	}
}

func TestReasoningToggle(t *testing.T) {
	m := newTestModel(t, 80, 24, "")
	m.messages = append(m.messages, chatMessage{
		role:      roleAssistant,
		content:   "这是答案",
		reasoning: "这是思考过程",
		done:      true,
	})
	m.refreshViewport(true)

	if !strings.Contains(m.View(), "思考 6 字") {
		t.Error("默认应折叠思考过程，只显示字数")
	}
	if strings.Contains(m.View(), "这是思考过程") {
		t.Error("默认不应显示思考内容")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if !strings.Contains(m.View(), "这是思考过程") {
		t.Error("Ctrl+R 后应展开思考内容")
	}
}

func TestClearCommand(t *testing.T) {
	m := newTestModel(t, 80, 24, "回答")
	m.input.SetValue("问题")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	collectStream(t, m)

	if len(m.ag.History()) != 2 {
		t.Fatalf("清空前历史长度 = %d, 想要 2", len(m.ag.History()))
	}

	m.input.SetValue("/clear")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if len(m.ag.History()) != 0 {
		t.Errorf("清空后历史长度 = %d, 想要 0", len(m.ag.History()))
	}
	if len(m.messages) != 1 {
		t.Errorf("清空后消息数 = %d, 想要 1 条提示", len(m.messages))
	}
}

func TestSmallTerminalFallback(t *testing.T) {
	m := newTestModel(t, 20, 8, "")
	if !strings.Contains(m.View(), "太小") {
		t.Error("小终端应显示提示而不是破版")
	}
}

func TestWrapTextCJK(t *testing.T) {
	text := strings.Repeat("中文", 30)
	wrapped := wrapText(text, 20)

	lines := strings.Split(wrapped, "\n")
	if len(lines) != 6 {
		t.Fatalf("行数 = %d, 想要 6：\n%s", len(lines), wrapped)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > 20 {
			t.Errorf("第 %d 行宽度 = %d, 超过 20: %q", i, w, line)
		}
	}
	if lines[0] != strings.Repeat("中文", 5) {
		t.Errorf("首行 = %q, 想要 5 组“中文”", lines[0])
	}
}

func TestApprovalOverlayKeys(t *testing.T) {
	m := newTestModel(t, 80, 24, "")
	resp := make(chan bool, 1)
	m.Update(approvalRequestMsg{
		toolName: "run_command",
		display:  "sleep 0",
		resp:     resp,
	})
	if m.approval == nil {
		t.Fatal("approval should be pending")
	}
	if !strings.Contains(m.View(), "确认执行") || !strings.Contains(m.View(), "sleep 0") {
		t.Fatalf("view missing approval card:\n%s", m.View())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	select {
	case allow := <-resp:
		if !allow {
			t.Fatal("Y should allow")
		}
	default:
		t.Fatal("no approval response")
	}
	if m.approval != nil {
		t.Fatal("approval should clear after Y")
	}

	resp2 := make(chan bool, 1)
	m.Update(approvalRequestMsg{toolName: "run_command", display: "rm -rf /", resp: resp2})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	select {
	case allow := <-resp2:
		if allow {
			t.Fatal("Esc should reject")
		}
	default:
		t.Fatal("no reject response")
	}
}
