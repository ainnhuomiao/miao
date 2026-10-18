package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditEventAndRotate(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	oldSkip, oldMax := skipAudit, maxAuditBytes
	skipAudit, maxAuditBytes = false, 80
	t.Cleanup(func() { skipAudit, maxAuditBytes = oldSkip, oldMax })

	auditEvent("/ws", "write_file", "a.go", "allow")
	auditEvent("/ws", "web_fetch", "https://example.com", "reject")
	p := filepath.Join(data, "miao", "audit.log")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "write_file") || !strings.Contains(string(b), "web_fetch") {
		t.Fatalf("log: %s", b)
	}
	oldPath := p + ".old"
	for i := 0; i < 20; i++ {
		auditCommand("/ws", "curl http://x", "deny")
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("应轮换出 .old：%v", err)
	}
}

func TestClipAudit(t *testing.T) {
	got := clipAudit("a.go\n+ lots of diff")
	if strings.Contains(got, "\n") {
		t.Fatalf("should flatten: %q", got)
	}
	long := strings.Repeat("x", 400)
	if n := len([]rune(clipAudit(long))); n > maxAuditField {
		t.Fatalf("len %d", n)
	}
}
