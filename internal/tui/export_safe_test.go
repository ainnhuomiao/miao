package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestExportConfirmsOverwriteAndRefusesSymlink(t *testing.T) {
	m := newTestModel(t, 100, 30, "回答")
	m.input.SetValue("问题")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	collectStream(t, m)
	dir := t.TempDir()
	p := filepath.Join(dir, "out.md")
	os.WriteFile(p, []byte("old"), 0o600)
	m.exportSession(p)
	if b, _ := os.ReadFile(p); string(b) != "old" {
		t.Fatal("first export should ask before overwriting")
	}
	m.exportSession(p)
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "问题") {
		t.Fatal("second export should overwrite")
	}
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link.md")
	os.Symlink(target, link)
	m.exportSession(link)
	m.exportSession(link)
	if _, err := os.Stat(target); err == nil {
		t.Fatal("followed symlink")
	}
}
