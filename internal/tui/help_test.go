package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestHelpEscRestoresWelcomeWithoutAddingMessages(t *testing.T) {
	m := newTestModel(t, 80, 24, "你好")
	before := m.View().Content
	messageCount := len(m.messages)
	for range 2 {
		typeText(m, "/help")
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if !m.help.open || !strings.Contains(m.View().Content, "Esc 关闭帮助") {
			t.Fatal("/help 应打开可关闭的帮助页")
		}
		if m.View().Cursor != nil {
			t.Fatal("帮助页不应显示输入框光标")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.help.open || m.View().Content != before {
			t.Fatal("Esc 应恢复原来的欢迎界面")
		}
		if len(m.messages) != messageCount || len(m.ag.History()) != 0 {
			t.Fatal("帮助内容不应加入对话消息或模型历史")
		}
	}
	sendMsg(t, m, "你好")
	if !strings.Contains(m.View().Content, "你好") {
		t.Fatal("关闭帮助后应能继续对话")
	}
}

func TestHelpScrollPreservesConversationDraftAndEscState(t *testing.T) {
	m := newTestModel(t, 80, 24, "回答")
	sendMsg(t, m, "问题")
	fillHistory(m, 60)
	m.refreshViewport(true)
	m.viewport.SetYOffset(5)
	m.input.SetValue("保留这份草稿")
	offset, count := m.viewport.YOffset(), len(m.messages)
	historyCount := len(m.ag.History())
	m.runCommand("/help")
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.help.viewport.YOffset() == 0 || m.viewport.YOffset() != offset {
		t.Fatal("帮助应独立滚动，不能滚动原对话")
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m.Update(tea.PasteMsg{Content: "不要写入草稿"})
	if m.input.Value() != "保留这份草稿" {
		t.Fatal("帮助页中的按键不能写入对话输入框")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if !strings.Contains(ansi.Strip(m.View().Content), "Ctrl+C") {
		t.Fatal("应能滚动到帮助末尾")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.viewport.YOffset() != offset || len(m.messages) != count || len(m.ag.History()) != historyCount {
		t.Fatal("关闭帮助应保留原对话与滚动位置")
	}
	m.input.Reset()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.rewind.open {
		t.Fatal("关闭帮助的 Esc 不能参与 Esc Esc 回退")
	}
}

func TestHelpFitsTerminalAfterResize(t *testing.T) {
	m := newTestModel(t, 80, 24, "ok")
	m.runCommand("/help")
	for _, size := range [][2]int{{36, 12}, {60, 18}, {80, 24}, {140, 50}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			view := ansi.Strip(m.View().Content)
			lines := strings.Split(view, "\n")
			if len(lines) != size[1] || !strings.Contains(lines[len(lines)-1], "Esc") {
				t.Fatalf("帮助页应占 %d 行且关闭提示始终可见，实际 %d 行", size[1], len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("帮助页行宽超出 %d：%q", size[0], line)
				}
			}
		})
	}
}
