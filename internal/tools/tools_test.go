package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePath_BlocksEscape(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ResolvePath("../outside"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := r.ResolvePath("/etc/passwd"); err == nil {
		t.Fatal("expected abs path error")
	}
	got, err := r.ResolvePath("a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "a", "b.txt")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestReadFileAndListDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)

	res := r.Call(context.Background(), "read_file", `{"path":"hello.txt"}`)
	if res.IsError || res.Content != "hi" {
		t.Fatalf("read_file: %+v", res)
	}

	res = r.Call(context.Background(), "list_dir", `{"path":"."}`)
	if res.IsError {
		t.Fatalf("list_dir error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "hello.txt") || !strings.Contains(res.Content, "sub/") {
		t.Fatalf("list_dir content = %q", res.Content)
	}
}

func TestRunCommand_AllowlistAndExec(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)

	blocked := r.Call(context.Background(), "run_command", `{"command":"rm","args":["-rf","."]}`)
	if !blocked.IsError || !(strings.Contains(blocked.Content, "白名单") || strings.Contains(blocked.Content, "审批")) {
		t.Fatalf("expected allowlist/approval error, got %+v", blocked)
	}

	pathBlocked := r.Call(context.Background(), "run_command", `{"command":"/bin/ls"}`)
	if !pathBlocked.IsError {
		t.Fatalf("expected path reject: %+v", pathBlocked)
	}

	ok := r.Call(context.Background(), "run_command", `{"command":"echo","args":["hello-miao"]}`)
	if ok.IsError {
		t.Fatalf("echo failed: %+v", ok)
	}
	if !strings.Contains(ok.Content, "hello-miao") {
		t.Fatalf("output = %q", ok.Content)
	}

	// cwd 应是工作区
	pwd := r.Call(context.Background(), "run_command", `{"command":"pwd"}`)
	if pwd.IsError {
		t.Fatalf("pwd: %+v", pwd)
	}
	if strings.TrimSpace(pwd.Content) != dir {
		t.Fatalf("pwd = %q want %q", pwd.Content, dir)
	}
}

func TestRunCommand_ApprovalGate(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)

	// 无审批：名单外拒绝
	res := r.Call(context.Background(), "run_command", `{"command":"sleep","args":["0"]}`)
	if !res.IsError {
		t.Fatalf("expected reject without approver: %+v", res)
	}

	// 拒绝
	r.SetApprover(func(ctx context.Context, toolName, display string, args json.RawMessage) (bool, error) {
		if toolName != "run_command" || display != "sleep 0" {
			t.Fatalf("unexpected ask: %s %q", toolName, display)
		}
		return false, nil
	})
	res = r.Call(context.Background(), "run_command", `{"command":"sleep","args":["0"]}`)
	if !res.IsError || !strings.Contains(res.Content, "用户拒绝") {
		t.Fatalf("expected user reject: %+v", res)
	}

	// 允许
	r.SetApprover(func(ctx context.Context, toolName, display string, args json.RawMessage) (bool, error) {
		return true, nil
	})
	res = r.Call(context.Background(), "run_command", `{"command":"sleep","args":["0"]}`)
	if res.IsError {
		t.Fatalf("expected allow: %+v", res)
	}

	// 带路径始终拒绝
	res = r.Call(context.Background(), "run_command", `{"command":"/bin/sleep","args":["0"]}`)
	if !res.IsError || !strings.Contains(res.Content, "路径") {
		t.Fatalf("expected path reject: %+v", res)
	}
}

func TestNeedsApproval(t *testing.T) {
	cases := map[string]bool{
		"ls": false, "git status": false, "git push": true, "git reset --hard": true,
		"go test ./...": false, "go run .": true, "go env -w X=1": true, "make": true,
		"env rm x": true, "sed -i s/a/b/ f": true, "find . -name x": false,
		"find . -delete": true, "find . -exec rm {} ;": true, "sort -o f f": true,
		"go test -exec rm ./...": true, "go build -toolexec=x .": true, "go build -o /tmp/x .": true,
		"git diff --output=f": true, "git log --output f": true, "git -c core.pager=x log": true,
		"go test -run TestX ./...": false, "git diff --stat": false,
		"rg --pre sh x": true, "rg --pre=sh x": true, "uniq in out": true,
		"git diff --no-index a b": true,
	}
	for c, want := range cases {
		f := strings.Fields(c)
		if got := needsApproval(f[0], f[1:]); got != want {
			t.Errorf("%q: got %v want %v", c, got, want)
		}
	}
}

func TestCommandEscapesWorkspace(t *testing.T) {
	dir := t.TempDir()
	if !CommandEscapesWorkspace(dir, "cat", []string{"/etc/passwd"}) {
		t.Fatal("cat /etc/passwd should escape")
	}
	if !CommandEscapesWorkspace(dir, "cat", []string{"../../.ssh/id_rsa"}) {
		t.Fatal("relative escape should be caught")
	}
	if !CommandEscapesWorkspace(dir, "find", []string{"/"}) {
		t.Fatal("find / should escape")
	}
	if !CommandEscapesWorkspace(dir, "head", []string{"/proc/self/environ"}) {
		t.Fatal("head /proc should escape")
	}
	if !CommandEscapesWorkspace(dir, "cat", []string{".env"}) {
		t.Fatal("cat .env is sensitive")
	}
	if CommandEscapesWorkspace(dir, "cat", []string{"README.md"}) {
		t.Fatal("relative in-workspace path should be fine")
	}
	if !CommandEscapesWorkspace(dir, "cat", []string{"./.git/config"}) {
		t.Fatal("cat ./.git/config should be sensitive")
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !CommandEscapesWorkspace(dir, "cat", []string{"sub/../.git/config"}) {
		t.Fatal("cat sub/../.git/config should be sensitive")
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if !CommandEscapesWorkspace(dir, "cat", []string{"link.txt"}) {
		t.Fatal("symlink pointing outside should be caught")
	}
	if !needsApprovalIn(dir, "git", []string{"diff", "--no-index", "/etc/passwd", "x"}) {
		t.Fatal("git diff --no-index outside should need approval")
	}
}

func TestCommandDenied(t *testing.T) {
	t.Setenv("MIAO_DENY_COMMANDS", "curl,rm -rf,go test")
	if !CommandDenied("curl", []string{"https://x"}) {
		t.Fatal("curl should be denied")
	}
	if !CommandDenied("go", []string{"test", "./..."}) {
		t.Fatal("go test should be denied")
	}
	if CommandDenied("go", []string{"vet", "."}) {
		t.Fatal("go vet should not match go test")
	}
}

func TestRestrictTo(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)
	r.RestrictTo([]string{"read_file", "grep"})
	defs := r.Definitions()
	if len(defs) != 2 {
		t.Fatalf("defs=%d", len(defs))
	}
	res := r.Call(context.Background(), "list_dir", `{"path":"."}`)
	if !res.IsError || !strings.Contains(res.Content, "未启用") {
		t.Fatalf("disabled tool should fail: %+v", res)
	}
}

func TestRepoMapGoFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package hello\nfunc World() {}\ntype Box struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := buildRepoMap(context.Background(), dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "hello.go") || !strings.Contains(got, "func World") || !strings.Contains(got, "struct Box") {
		t.Fatalf("repo map: %q", got)
	}
}

func TestReadFileSensitiveAndSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("MIAO_API_KEY=sk-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("leaked"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)

	res := r.Call(context.Background(), "read_file", `{"path":".env"}`)
	if !res.IsError || !strings.Contains(res.Content, "敏感") {
		t.Fatalf("read .env: %+v", res)
	}
	res = r.Call(context.Background(), "read_file", `{"path":"link.txt"}`)
	if !res.IsError || !strings.Contains(res.Content, "符号链接") {
		t.Fatalf("read symlink: %+v", res)
	}
	res = r.Call(context.Background(), "read_file", `{"path":"ok.txt"}`)
	if res.IsError || res.Content != "hi" {
		t.Fatalf("read ok: %+v", res)
	}
}
