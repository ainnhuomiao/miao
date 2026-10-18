package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func newSecRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("MIAO_API_KEY=sk-secret-xyz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".env", filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)
	return r, dir
}

// PoC：工作区内 notes.txt -> .env 曾能经 read_file 读出密钥。
func TestSymlinkToEnvRejected(t *testing.T) {
	r, dir := newSecRegistry(t)
	ctx := context.Background()
	for _, c := range []struct{ tool, args string }{
		{"read_file", `{"path":"notes.txt"}`},
		{"edit_file", `{"path":"notes.txt","old_string":"sk-secret-xyz","new_string":"x"}`},
		{"write_file", `{"path":"notes.txt","content":"x"}`},
		{"grep", `{"pattern":"sk-secret","path":"notes.txt"}`},
	} {
		res := r.Call(ctx, c.tool, c.args)
		if !res.IsError || strings.Contains(res.Content, "sk-secret-xyz") {
			t.Errorf("%s 未拒绝符号链接到 .env: %+v", c.tool, res)
		}
	}
	for _, c := range []struct{ tool, args string }{
		{"grep", `{"pattern":"sk-secret"}`},
		{"glob", `{"pattern":"**/*"}`},
	} {
		res := r.Call(ctx, c.tool, c.args)
		if strings.Contains(res.Content, "sk-secret-xyz") || strings.Contains(res.Content, ".env") {
			t.Errorf("%s 泄露 .env: %+v", c.tool, res)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".env")); !strings.Contains(string(b), "sk-secret-xyz") {
		t.Fatalf(".env 被修改: %q", b)
	}
}

// 目录符号链接指向 .git，写入其下新文件也应拒绝。
func TestSymlinkDirToGitRejected(t *testing.T) {
	r, dir := newSecRegistry(t)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".git", filepath.Join(dir, "g")); err != nil {
		t.Fatal(err)
	}
	res := r.Call(context.Background(), "write_file", `{"path":"g/hooks.sh","content":"x"}`)
	if !res.IsError {
		t.Fatalf("应拒绝: %+v", res)
	}
}

func TestSensitiveRulesExpanded(t *testing.T) {
	for _, p := range []string{
		".env", ".env.local", ".miao/config.json", "sub/.miao/x", "key.pem", "certs/Server.PEM",
		"id_rsa", "id_ed25519.pub", ".ssh/id_ecdsa", ".npmrc", "a/.netrc", ".envrc", ".git-credentials", ".git/config",
	} {
		if !isSensitive(p) {
			t.Errorf("%s 应为敏感", p)
		}
	}
	for _, p := range []string{"main.go", "AGENTS.md", "identity.go", "id_test.go", "pem.go", "envrc.md", "docs/miao.md"} {
		if isSensitive(p) {
			t.Errorf("%s 不应为敏感", p)
		}
	}
}

func TestAgentsMDReadOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(dir, "a.md")); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)
	ctx := context.Background()
	if res := r.Call(ctx, "read_file", `{"path":"AGENTS.md"}`); res.IsError || !strings.Contains(res.Content, "rules") {
		t.Fatalf("AGENTS.md 应可读: %+v", res)
	}
	for _, args := range []string{
		`{"path":"AGENTS.md","old_string":"rules","new_string":"x"}`,
		`{"path":"a.md","old_string":"rules","new_string":"x"}`,
	} {
		if res := r.Call(ctx, "edit_file", args); !res.IsError {
			t.Errorf("edit 应拒绝: %s %+v", args, res)
		}
	}
	for _, args := range []string{`{"path":"AGENTS.md","content":"x"}`, `{"path":"sub/AGENTS.md","content":"x"}`} {
		if res := r.Call(ctx, "write_file", args); !res.IsError {
			t.Errorf("write 应拒绝: %s %+v", args, res)
		}
	}
}

// PoC：符号链接指向 FIFO 时 Lstat 判定为链接而放行，Open 会永久阻塞。
func TestSymlinkToFIFONoHang(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("mkfifo 不可用:", err)
	}
	if err := os.Symlink("pipe", filepath.Join(dir, "p.txt")); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)
	for _, c := range []struct{ tool, args string }{
		{"read_file", `{"path":"p.txt"}`},
		{"read_file", `{"path":"pipe"}`},
		{"edit_file", `{"path":"p.txt","old_string":"a","new_string":"b"}`},
		{"write_file", `{"path":"p.txt","content":"x"}`},
	} {
		done := make(chan Result, 1)
		go func() { done <- r.Call(context.Background(), c.tool, c.args) }()
		select {
		case res := <-done:
			if !res.IsError {
				t.Errorf("%s %s 应拒绝: %+v", c.tool, c.args, res)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s %s 阻塞", c.tool, c.args)
		}
	}
}
