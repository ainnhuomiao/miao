package tools

import (
	"context"
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
