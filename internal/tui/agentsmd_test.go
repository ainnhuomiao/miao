package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"miao/internal/agent"
	"miao/internal/llm"
)

func TestAgentsMDNotice(t *testing.T) {
	for _, loaded := range []bool{false, true} {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		ag := agent.New(&fakeClient{deltas: []llm.Delta{{Content: "x"}}}, "测试助手", nil)
		ag.SetAgentsMDLoaded(loaded)
		m := New(ag, "test-model")
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		found := false
		for i, msg := range m.messages {
			if msg.content == "已加载 AGENTS.md" {
				found = true
				if i != 1 || msg.role != roleNotice {
					t.Fatalf("提示应紧跟欢迎语且为灰字 notice，got idx=%d role=%v", i, msg.role)
				}
			}
		}
		if found != loaded {
			t.Fatalf("loaded=%v 但提示出现=%v", loaded, found)
		}
	}
}
