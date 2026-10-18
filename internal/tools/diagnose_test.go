package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ainnhuomiao/miao/internal/childenv"
	"github.com/ainnhuomiao/miao/internal/config"
)

func TestGoVetArgs(t *testing.T) {
	if got := strings.Join(goVetArgs("internal/tools/read.go"), " "); got != "vet ./internal/tools" {
		t.Fatalf("got %q", got)
	}
	if got := strings.Join(goVetArgs("main.go"), " "); got != "vet ." {
		t.Fatalf("root file: %q", got)
	}
	if got := strings.Join(tscArgs(), " "); got != "--noEmit --pretty false" {
		t.Fatalf("tsc should not pass a file: %q", got)
	}
}

func TestDiagnoseGoVetsPackageNotSingleFile(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/diag\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(dir, "pkg")
	if err := os.Mkdir(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "a.go"), []byte("package pkg\nfunc A() { B() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "b.go"), []byte("package pkg\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := diagnoseFile(context.Background(), dir, "pkg/a.go")
	if got != "" {
		t.Fatalf("同一包内的符号应能解析，不应报假错误：%q", got)
	}
}

func TestFlushDiagnoseBatchesAndClears(t *testing.T) {
	t.Setenv("MIAO_DIAGNOSE", "0")
	r, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := r.FlushDiagnose(context.Background()); got != "" {
		t.Fatalf("empty: %q", got)
	}
	r.noteEdited("pkg/a.go")
	r.noteEdited("pkg/b.go")
	if got := r.FlushDiagnose(context.Background()); got != "" {
		t.Fatalf("disabled: %q", got)
	}
	if got := r.FlushDiagnose(context.Background()); got != "" {
		t.Fatalf("cleared: %q", got)
	}
}

func TestFormatDiagnoseTimeout(t *testing.T) {
	parent := context.Background()
	dctx, cancel := context.WithDeadline(parent, time.Now().Add(-time.Second))
	defer cancel()
	got := formatDiagnose(parent, dctx, "tsc", "signal: killed", dctx.Err())
	if !strings.Contains(got, "诊断超时，已跳过") || strings.Contains(got, "killed") {
		t.Fatalf("超时不应把 signal: killed 交给模型：%q", got)
	}
	canceled, c := context.WithCancel(context.Background())
	c()
	if got := formatDiagnose(canceled, dctx, "tsc", "x", context.Canceled); got != "" {
		t.Fatalf("父上下文取消应静默：%q", got)
	}
}

func TestDiagnoseDisabled(t *testing.T) {
	t.Setenv("MIAO_DIAGNOSE", "0")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := diagnoseFile(context.Background(), dir, "a.go"); got != "" {
		t.Fatalf("disabled: %q", got)
	}
}

func vetWorkspace(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	t.Setenv("MIAO_HOME", t.TempDir())
	t.Setenv("MIAO_DIAGNOSE", "")
	ws := t.TempDir()
	files := map[string]string{
		"go.mod":            "module example.com/x\n\ngo 1.21\n",
		"a.go":              "package x\n\nimport \"fmt\"\n\nfunc A() { fmt.Printf(\"%d\", \"s\") }\n",
		".miao/config.json": "{}\n",
	}
	for name, body := range files {
		p := filepath.Join(ws, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

// 未信任仓库：编辑 .go 后不自动跑 go vet（会编译仓库代码），只给一次灰色提示。
func TestAutoVetSkippedInUntrustedProject(t *testing.T) {
	ws := vetWorkspace(t)
	r, err := NewRegistry(ws)
	if err != nil {
		t.Fatal(err)
	}
	r.noteEdited("a.go")
	if got := r.FlushDiagnose(context.Background()); got != "" {
		t.Fatalf("未信任仓库不应运行 go vet: %q", got)
	}
	if n := r.TakeDiagnoseNotice(); !strings.Contains(n, "/trust-project") {
		t.Fatalf("应提示如何信任仓库: %q", n)
	}
	r.noteEdited("a.go")
	r.FlushDiagnose(context.Background())
	if n := r.TakeDiagnoseNotice(); n != "" {
		t.Fatalf("提示每个会话只显示一次: %q", n)
	}
}

func TestAutoVetRunsInTrustedProject(t *testing.T) {
	ws := vetWorkspace(t)
	if _, err := config.TrustProject(ws); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(ws)
	if err != nil {
		t.Fatal(err)
	}
	r.noteEdited("a.go")
	if got := r.FlushDiagnose(context.Background()); !strings.Contains(got, "go vet") {
		t.Fatalf("已信任仓库应运行 go vet: %q", got)
	}
	if n := r.TakeDiagnoseNotice(); n != "" {
		t.Fatalf("已信任时不应有跳过提示: %q", n)
	}
}

func TestGoVetEnvOverridesInherited(t *testing.T) {
	t.Setenv("GOFLAGS", "-toolexec=/tmp/evil")
	t.Setenv("GOTOOLCHAIN", "go1.99.0")
	t.Setenv("CGO_ENABLED", "1")
	t.Setenv("MIAO_API_KEY", "sk-vet-leak")
	env := goVetEnv(childenv.Environ())
	got := map[string][]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = append(got[k], v)
	}
	want := map[string]string{"GOTOOLCHAIN": "local", "GOFLAGS": "-mod=readonly", "CGO_ENABLED": "0"}
	for k, v := range want {
		if len(got[k]) != 1 || got[k][0] != v {
			t.Fatalf("%s = %q, want exactly [%q]", k, got[k], v)
		}
	}
	if len(got["MIAO_API_KEY"]) != 0 {
		t.Fatal("go vet 环境不应包含 MIAO_API_KEY")
	}
	if len(got["PATH"]) != 1 {
		t.Fatal("PATH 应保留")
	}
}

// 真实运行一次：继承的 GOFLAGS=-toolexec 不能生效。
func TestAutoVetIgnoresInheritedGOFLAGS(t *testing.T) {
	ws := vetWorkspace(t)
	marker := filepath.Join(t.TempDir(), "pwned")
	evil := filepath.Join(t.TempDir(), "evil.sh")
	if err := os.WriteFile(evil, []byte("#!/bin/sh\ntouch "+marker+"\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", "-toolexec="+evil)
	if out := diagnoseFile(context.Background(), ws, "a.go"); !strings.Contains(out, "go vet") {
		t.Fatalf("go vet 应正常运行并报错: %q", out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("继承的 GOFLAGS -toolexec 被执行了")
	}
}
