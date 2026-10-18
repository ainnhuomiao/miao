package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/session"
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
	t.Setenv("XDG_DATA_HOME", t.TempDir())
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
	t.Setenv("XDG_DATA_HOME", t.TempDir())
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

func TestSessionAutoSaveAndCommands(t *testing.T) {
	m := newTestModel(t, 80, 24, "你好！")
	if m.store == nil || m.sess == nil {
		t.Fatal("启动时应创建 Store 与空会话")
	}
	oldID := m.sess.ID

	m.input.SetValue("hi")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	collectStream(t, m)

	loaded, err := m.store.Load(oldID)
	if err != nil {
		t.Fatalf("流结束后应自动保存: %v", err)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[0].Content != "hi" {
		t.Fatalf("保存内容不符: %+v", loaded.Messages)
	}

	m.input.SetValue("/sessions")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.picker.open || len(m.pickerMatches()) == 0 || m.pickerMatches()[0].ID != oldID {
		t.Fatalf("/sessions 应弹出列表并包含 %s", oldID)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	m.input.SetValue("/new")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.sess.ID == oldID {
		t.Fatal("/new 应换新会话 id")
	}
	if len(m.ag.History()) != 0 {
		t.Fatal("/new 应清空 Agent 历史")
	}
	if len(m.messages) != 1 || !strings.Contains(m.messages[0].content, "新建") {
		t.Fatalf("/new 提示不符: %+v", m.messages)
	}

	m.input.SetValue("/load " + oldID)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.sess.ID != oldID {
		t.Fatalf("加载后 id = %s, 想要 %s", m.sess.ID, oldID)
	}
	if got := len(m.ag.History()); got != 2 {
		t.Fatalf("加载后历史长度 = %d, 想要 2", got)
	}
	var sawUser, sawAsst bool
	for _, cm := range m.messages {
		if cm.role == roleUser && cm.content == "hi" {
			sawUser = true
		}
		if cm.role == roleAssistant && cm.content == "你好！" {
			sawAsst = true
		}
	}
	if !sawUser || !sawAsst {
		t.Fatalf("加载后界面应还原对话: %+v", m.messages)
	}
}

func TestLoadMissingAndEmptySessions(t *testing.T) {
	m := newTestModel(t, 80, 24, "")
	m.input.SetValue("/load nosuch-id-zzz")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	last := m.messages[len(m.messages)-1].content
	if !strings.Contains(last, "不存在") {
		t.Fatalf("缺失会话提示不符: %q", last)
	}

	m.input.SetValue("/sessions")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	last = m.messages[len(m.messages)-1].content
	if !strings.Contains(last, "暂无") {
		t.Fatalf("空列表提示不符: %q", last)
	}

	m.input.SetValue("/load")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // /load 已不在菜单里，无参直接给用法
	last = m.messages[len(m.messages)-1].content
	if !strings.Contains(last, "用法") {
		t.Fatalf("/load 无参提示不符: %q", last)
	}
}

func TestClearRotatesSessionID(t *testing.T) {
	m := newTestModel(t, 80, 24, "回答")
	oldID := m.sess.ID
	m.input.SetValue("问题")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	collectStream(t, m)

	m.input.SetValue("/clear")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.sess.ID == oldID {
		t.Fatal("/clear 应换新 id，避免覆盖已保存会话")
	}
	if _, err := m.store.Load(oldID); err != nil {
		t.Fatalf("旧会话文件应仍在: %v", err)
	}
	if !errors.Is(func() error {
		_, err := m.store.Load(m.sess.ID)
		return err
	}(), session.ErrNotFound) {
		t.Fatal("新空会话不应落盘")
	}
}

func TestChatMessagesFromHistoryWithTools(t *testing.T) {
	sess := &session.Session{
		ID:    "abc",
		Title: "工具回合",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "读文件"},
			{
				Role:    llm.RoleAssistant,
				Content: "好的",
				ToolCalls: []llm.ToolCall{{
					ID: "c1", Type: "function",
					Function: llm.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`},
				}},
			},
			{Role: llm.RoleTool, ToolCallID: "c1", Name: "read_file", Content: "package main"},
			{Role: llm.RoleAssistant, Content: "这是 main 包"},
		},
	}
	msgs := chatMessagesFromHistory(sess)
	var tools int
	for _, cm := range msgs {
		if cm.role == roleTool {
			tools++
			if !cm.done || cm.toolResult != "package main" || cm.toolName != "read_file" {
				t.Fatalf("工具卡不符: %+v", cm)
			}
		}
	}
	if tools != 1 {
		t.Fatalf("工具卡数 = %d", tools)
	}
}

func TestChatMessagesFromHistorySkipsReminders(t *testing.T) {
	sess := &session.Session{
		ID: "x",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "<system-reminder>\n工作区根目录是 \"/tmp\"。\n</system-reminder>"},
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleAssistant, Content: "yo"},
		},
	}
	var users int
	for _, cm := range chatMessagesFromHistory(sess) {
		if cm.role == roleUser {
			users++
			if cm.content != "hi" {
				t.Fatalf("不应显示 reminder：%q", cm.content)
			}
		}
	}
	if users != 1 {
		t.Fatalf("用户气泡数 = %d", users)
	}
}
