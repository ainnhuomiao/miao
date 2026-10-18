package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
