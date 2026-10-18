package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAgentsMDRejectsSymlink(t *testing.T) {
	ws, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "evil.md")
	if err := os.Symlink(target, filepath.Join(ws, AgentsMDName)); err != nil {
		t.Skip(err)
	}
	if err := WriteAgentsMD(ws, "x", true); err == nil {
		t.Fatal("should refuse symlink")
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("wrote outside workspace")
	}
}
