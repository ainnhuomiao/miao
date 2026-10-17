package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUserCommandInPaletteAndSend(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	dir := filepath.Join(cfg, "miao", "commands")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "review.md"), []byte("---\ndescription: 审查代码\n---\n请审查 $ARGS"), 0o644)
	os.WriteFile(filepath.Join(dir, "help.md"), []byte("不能覆盖内置"), 0o644)

	m := newTestModel(t, 100, 30, "ok")
	typeText(m, "/rev")
	names := paletteNames(m)
	if len(names) != 1 || names[0] != "/review" {
		t.Fatalf("菜单=%v", names)
	}
	if !strings.Contains(stripANSI(m.View()), "审查代码（用户命令）") {
		t.Fatal("菜单应显示描述和来源")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.input.Value() != "/review " {
		t.Fatalf("带 $ARGS 的命令 Enter 应先补全，got %q", m.input.Value())
	}
	typeText(m, "main.go")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.streaming {
		t.Fatal("应发送给模型")
	}
	collectStream(t, m)
	var shown bool
	for _, msg := range m.messages {
		if msg.role == roleUser && msg.content == "/review main.go" {
			shown = true
		}
	}
	if !shown {
		t.Fatal("界面应显示命令原文")
	}
	h := m.ag.History()
	if len(h) == 0 || h[0].Content != "请审查 main.go" {
		t.Fatalf("发给模型的应是展开后的模板：%+v", h)
	}
	m.runCommand("/help")
	if strings.Contains(m.messages[len(m.messages)-1].content, "不能覆盖内置") {
		t.Fatal("内置命令优先")
	}
}

func TestHooksCommandNoHooks(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.runCommand("/hooks")
	if !strings.Contains(m.messages[len(m.messages)-1].content, "没有配置 hook") {
		t.Fatal(m.messages[len(m.messages)-1].content)
	}
}
