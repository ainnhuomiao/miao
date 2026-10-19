package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/tools"
)

func TestSessionStorageCanBeDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MIAO_HOME", home)
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tools.RegisterBuiltins(reg)
	ag := agent.New(&fakeClient{}, "sys", reg)
	m := New(ag, "test-model", Options{DisableSessionStorage: true})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if err := ag.Send(context.Background(), "do not persist", nil); err != nil {
		t.Fatal(err)
	}
	m.saveCurrentSession()
	for _, command := range []string{"/sessions", "/fork", "/load nonexistent"} {
		m.runCommand(command)
		if last := m.messages[len(m.messages)-1].content; !strings.Contains(last, "会话保存已关闭") {
			t.Fatalf("disabled storage should be explained for %s: %s", command, last)
		}
	}
	if m.store != nil || m.sess != nil {
		t.Fatal("disabled sessions must not create a store")
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("disabled storage must not create session or snapshot files: %v, %v", entries, err)
	}
	if err := os.WriteFile(filepath.Join(reg.Workspace(), "a.txt"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	ag.SetMode(agent.ModeAcceptEdits)
	ag.SetApprover(nil)
	if res := reg.Call(context.Background(), "write_file", `{"path":"a.txt","content":"after"}`); res.IsError {
		t.Fatal(res.Content)
	}
	if ag.UndoDepth() != 1 {
		t.Fatal("undo should still work in memory with session storage disabled")
	}
	m.beginNewSession("new")
	if ag.UndoDepth() != 0 || len(ag.History()) != 0 {
		t.Fatal("starting a new ephemeral session must reset its undo stack and history")
	}
}

func TestDisabledShellAndProjectInitHaveNoSideEffects(t *testing.T) {
	t.Setenv("MIAO_HOME", t.TempDir())
	workspace := t.TempDir()
	reg, _ := tools.NewRegistry(workspace)
	m := New(agent.New(&fakeClient{}, "sys", reg), "test-model", Options{
		DisableSessionStorage: true, DisableShell: true, DisableProjectMemory: true,
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd := m.runBang("touch shell-started"); cmd != nil || m.streaming {
		t.Fatal("disabled shell must not return an executable command")
	}
	m.initAgentsMD()
	if entries, err := os.ReadDir(workspace); err != nil || len(entries) != 0 {
		t.Fatalf("disabled features must not write workspace files: %v, %v", entries, err)
	}
	for _, want := range []string{"! 命令已关闭", "项目记忆已关闭"} {
		found := false
		for _, msg := range m.messages {
			found = found || strings.Contains(msg.content, want)
		}
		if !found {
			t.Fatalf("missing disabled-feature notice %q", want)
		}
	}
}

func TestDisabledCustomCommandsIgnoreTemplatesAndCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MIAO_HOME", home)
	dir := filepath.Join(home, "commands")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review.md"), []byte("custom secret prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(agent.New(&fakeClient{}, "sys", nil), "test-model")
	if len(m.userCommands()) != 1 {
		t.Fatal("template fixture must be visible when enabled")
	}
	m.options.DisableCustomCommands = true
	if len(m.userCommands()) != 0 || len(m.userPalette()) != 0 {
		t.Fatal("disabled custom commands must also bypass a populated cache")
	}
	if cmd, ok := m.runUserCommand("/review"); ok || cmd != nil || m.streaming {
		t.Fatal("disabled template must never be expanded or sent")
	}
}

func TestDisabledImagesDoNotReadClipboardOrAttach(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.ag.SetImagesEnabled(false)
	calls := fakeClip(t, map[string][]byte{"wl-paste": tuiPNG})
	if handled, cmd := m.handleImageKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}); handled || cmd != nil {
		t.Fatal("disabled images must leave Ctrl+V to normal text paste")
	}
	if m.addPendingImage("a.png", tuiPNG) {
		t.Fatal("disabled image input must reject direct additions")
	}
	m.onClipImage(clipImageMsg{data: tuiPNG})
	if len(*calls) != 0 || len(m.img.pending) != 0 {
		t.Fatal("disabled image input must not invoke clipboard tools or queue images")
	}
	m.Update(tea.PasteMsg{Content: "plain text"})
	if m.input.Value() != "plain text" {
		t.Fatal("text paste must remain available")
	}
}

func TestDisabledFeatureCommandsHiddenButReserved(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.options.DisableSessionStorage = true
	m.options.DisableProjectMemory = true
	m.options.DisableSnapshots = true
	for _, command := range m.paletteCommands() {
		switch command.name {
		case "/sessions", "/fork", "/init", "/undo":
			t.Fatalf("disabled feature should not appear in the menu: %s", command.name)
		}
	}
	for _, name := range []string{"sessions", "fork", "init", "undo"} {
		if !m.builtinCommands()[name] {
			t.Fatalf("hidden builtin must remain reserved from custom templates: %s", name)
		}
	}
}
