package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newSearchRegistry(t *testing.T, files map[string]string) *Registry {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)
	return r
}

func TestGlob_DoubleStar(t *testing.T) {
	r := newSearchRegistry(t, map[string]string{
		"main.go":      "package main",
		"a/b/c.go":     "package b",
		"a/readme.md":  "x",
		".git/HEAD.go": "x",
		"build/out.go": "x",
		"logs/x.log":   "x",
		".gitignore":   "# c\n\n/build/\n*.log\n",
	})
	res := r.Call(context.Background(), "glob", `{"pattern":"**/*.go"}`)
	if res.IsError {
		t.Fatal(res.Content)
	}
	got := strings.Split(res.Content, "\n")
	if strings.Join(got, ",") != "a/b/c.go,main.go" {
		t.Fatalf("got %q", res.Content)
	}
	res = r.Call(context.Background(), "glob", `{"pattern":"*.go","path":"a/b"}`)
	if res.Content != "a/b/c.go" {
		t.Fatalf("got %q", res.Content)
	}
	res = r.Call(context.Background(), "glob", `{"pattern":"**/*.log"}`)
	if res.Content != "(无匹配)" {
		t.Fatalf("gitignored file listed: %q", res.Content)
	}
}

func TestGrep_FormatAndSkips(t *testing.T) {
	r := newSearchRegistry(t, map[string]string{
		"a.go":        "package a\nfunc Hello() {}\n",
		"sub/b.txt":   "hello world\n",
		"bin.dat":     "hello\x00\x01",
		".git/config": "hello",
		"ign/x.go":    "func Hello() {}",
		".gitignore":  "ign/\n",
	})
	res := r.Call(context.Background(), "grep", `{"pattern":"hello","ignore_case":true}`)
	if res.IsError {
		t.Fatal(res.Content)
	}
	want := "a.go:2: func Hello() {}\nsub/b.txt:1: hello world"
	if res.Content != want {
		t.Fatalf("got %q want %q", res.Content, want)
	}
	res = r.Call(context.Background(), "grep", `{"pattern":"Hello","glob":"*.go"}`)
	if res.Content != "a.go:2: func Hello() {}" {
		t.Fatalf("glob filter: %q", res.Content)
	}
	res = r.Call(context.Background(), "grep", `{"pattern":"world","path":"sub/b.txt"}`)
	if res.Content != "sub/b.txt:1: hello world" {
		t.Fatalf("single file: %q", res.Content)
	}
}

func TestGitIgnoreNegation(t *testing.T) {
	r := newSearchRegistry(t, map[string]string{
		"a.log":      "x",
		"keep.log":   "x",
		".gitignore": "*.log\n!keep.log\n",
	})
	res := r.Call(context.Background(), "glob", `{"pattern":"*.log"}`)
	if res.Content != "keep.log" {
		t.Fatalf("got %q", res.Content)
	}
}

func TestSearch_Truncation(t *testing.T) {
	files := map[string]string{}
	var b strings.Builder
	for i := 0; i < 150; i++ {
		files[fmt.Sprintf("f%03d.txt", i)] = "x"
		b.WriteString("match\n")
	}
	files["big.md"] = b.String()
	r := newSearchRegistry(t, files)
	res := r.Call(context.Background(), "glob", `{"pattern":"*.txt"}`)
	lines := strings.Split(res.Content, "\n")
	if len(lines) != 101 || !strings.Contains(lines[100], "已截断") || !strings.Contains(lines[100], "150") {
		t.Fatalf("glob truncation: %d lines, last %q", len(lines), lines[len(lines)-1])
	}
	res = r.Call(context.Background(), "grep", `{"pattern":"match"}`)
	lines = strings.Split(res.Content, "\n")
	if len(lines) != 101 || !strings.Contains(lines[100], "已截断") {
		t.Fatalf("grep truncation: %d lines", len(lines))
	}
}

func TestSearch_LongLineTruncated(t *testing.T) {
	r := newSearchRegistry(t, map[string]string{"l.txt": "x" + strings.Repeat("y", 1000)})
	res := r.Call(context.Background(), "grep", `{"pattern":"x"}`)
	if len(res.Content) > 400 || !strings.Contains(res.Content, "截断") {
		t.Fatalf("long line: %d", len(res.Content))
	}
}

func TestSearch_PathEscape(t *testing.T) {
	r := newSearchRegistry(t, map[string]string{"a.txt": "x"})
	for _, name := range []string{"glob", "grep"} {
		res := r.Call(context.Background(), name, `{"pattern":"x","path":"../"}`)
		if !res.IsError {
			t.Fatalf("%s: expected escape error, got %q", name, res.Content)
		}
	}
}
