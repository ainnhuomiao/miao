package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
