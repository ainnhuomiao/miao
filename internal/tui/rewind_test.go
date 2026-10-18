package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func sendMsg(t *testing.T, m *Model, text string) {
	t.Helper()
	m.input.SetValue(text)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	collectStream(t, m)
}

func TestDoubleEscRewind(t *testing.T) {
	m := newTestModel(t, 100, 30, "好的")
	sendMsg(t, m, "第一条")
	sendMsg(t, m, "第二条")
	before := len(m.ag.History())

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.rewind.open {
		t.Fatal("单次 Esc 不应打开回退菜单")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.rewind.open || len(m.rewind.items) != 2 || m.rewind.items[0].text != "第二条" {
		t.Fatalf("Esc Esc 应列出消息（最新在上）: %+v", m.rewind.items)
	}
	if !strings.Contains(m.View().Content, "回到哪条消息") {
		t.Fatal("菜单没有渲染")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.rewind.open || m.input.Value() != "第二条" {
		t.Fatalf("回退后输入框应是原文，got %q", m.input.Value())
	}
	if got := len(m.ag.History()); got >= before || got == 0 {
		t.Fatalf("历史应截到第二条之前: %d -> %d", before, got)
	}
	for _, msg := range m.ag.History() {
		if msg.Content == "第二条" {
			t.Fatal("第二条应已从历史中移除")
		}
	}
}

func TestEscWithInputDoesNotRewind(t *testing.T) {
	m := newTestModel(t, 100, 30, "好的")
	sendMsg(t, m, "第一条")
	m.input.SetValue("草稿")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.rewind.open {
		t.Fatal("输入框有内容时不应打开回退菜单")
	}
}

func TestForkSession(t *testing.T) {
	m := newTestModel(t, 100, 30, "好的")
	sendMsg(t, m, "原始问题")
	orig := m.sess.ID
	m.forkSession()
	if m.sess.ID == orig || !strings.HasSuffix(m.sess.Title, "· 分支") {
		t.Fatalf("应切到新会话: %s %q", m.sess.ID, m.sess.Title)
	}
	list, _ := m.store.List()
	if len(list) != 2 {
		t.Fatalf("原会话应保留，共 2 个，got %d", len(list))
	}
	if len(m.ag.History()) == 0 {
		t.Fatal("分支应保留对话历史")
	}
}
