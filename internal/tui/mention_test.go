package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/tools"
)

func newMentionModel(t *testing.T) *Model {
	t.Helper()
	t.Setenv("MIAO_HOME", t.TempDir())
	dir := t.TempDir()
	for name, body := range map[string]string{"main.go": "package main\n", "internal/tui/view.go": "package tui\n", "README.md": "hi"} {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	reg, err := tools.NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	tools.RegisterBuiltins(reg)
	m := New(agent.New(&fakeClient{}, "sys", reg), "test-model")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func TestAtMenuOpensAndCompletes(t *testing.T) {
	m := newMentionModel(t)
	typeText(m, "看看 @vie")
	if !m.paletteOpen() {
		t.Fatal("@ 后应弹出文件菜单")
	}
	got := m.paletteMatches()
	if len(got) == 0 || got[0].name != "internal/tui/view.go" {
		t.Fatalf("matches=%v", got)
	}
	if !strings.Contains(stripANSI(m.View().Content), "internal/tui/view.go") {
		t.Fatal("菜单应渲染文件路径")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if v := m.input.Value(); v != "看看 @internal/tui/view.go " {
		t.Fatalf("补全后=%q", v)
	}
	if m.paletteOpen() {
		t.Fatal("补全后菜单应关闭")
	}
}

func TestAtMenuNotForCommandsOrEmail(t *testing.T) {
	m := newMentionModel(t)
	typeText(m, "/load @x")
	if _, ok := m.atMode(); ok {
		t.Fatal("斜杠命令里不触发 @")
	}
	m.input.Reset()
	typeText(m, "mail a@b")
	if _, ok := m.atMode(); ok {
		t.Fatal("邮箱这类词中间的 @ 不触发")
	}
}

func TestAtMenuEscHides(t *testing.T) {
	m := newMentionModel(t)
	typeText(m, "@ma")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.paletteOpen() {
		t.Fatal("Esc 后应收起")
	}
}

func TestExpandMentionsNotice(t *testing.T) {
	m := newMentionModel(t)
	out := m.expandMentions("解释 @main.go")
	if !strings.Contains(out, "package main") {
		t.Fatalf("应附上文件内容：%q", out)
	}
	if last := m.messages[len(m.messages)-1]; last.role != roleNotice || last.content != "已附加 main.go" {
		t.Fatalf("应有灰字提示，got %+v", last)
	}
}
