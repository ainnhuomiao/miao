package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newWriteReg(t *testing.T, allow bool) (*Registry, string) {
	dir := t.TempDir()
	r, _ := NewRegistry(dir)
	RegisterBuiltins(r)
	r.SetApprover(func(ctx context.Context, n, d string, a json.RawMessage) (bool, error) { return allow, nil })
	return r, dir
}

func TestWriteFileApproved(t *testing.T) {
	r, dir := newWriteReg(t, true)
	res := r.Call(context.Background(), "write_file", `{"path":"a/b.txt","content":"hi\n"}`)
	if res.IsError {
		t.Fatal(res.Content)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "a/b.txt"))
	if string(b) != "hi\n" {
		t.Fatalf("got %q", b)
	}
}

func TestWriteFileRejected(t *testing.T) {
	r, dir := newWriteReg(t, false)
	res := r.Call(context.Background(), "write_file", `{"path":"x.txt","content":"hi"}`)
	if !res.IsError {
		t.Fatal("expected reject")
	}
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Fatal("file should not exist")
	}
}

func TestWriteFileGuards(t *testing.T) {
	r, dir := newWriteReg(t, true)
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(dir, "link"))
	for _, p := range []string{"../x", ".env", "sub/.env.local", ".git/config", "link/evil.txt"} {
		args, _ := json.Marshal(map[string]string{"path": p, "content": "x"})
		if res := r.Call(context.Background(), "write_file", string(args)); !res.IsError {
			t.Fatalf("%s should be rejected", p)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.txt")); err == nil {
		t.Fatal("escaped via symlink")
	}
}

func TestWriteFileNoApprover(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewRegistry(dir)
	RegisterBuiltins(r)
	if res := r.Call(context.Background(), "write_file", `{"path":"x","content":"y"}`); !res.IsError {
		t.Fatal("expected reject without approver")
	}
}

func TestWriteFileDiffPreview(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "m.go"), []byte("a\nb\nc\n"), 0o644)
	r, _ := NewRegistry(dir)
	RegisterBuiltins(r)
	var got string
	r.SetApprover(func(ctx context.Context, n, d string, a json.RawMessage) (bool, error) { got = d; return true, nil })
	if res := r.Call(context.Background(), "write_file", `{"path":"m.go","content":"a\nB\nc\nd\n"}`); res.IsError {
		t.Fatal(res.Content)
	}
	want := "m.go · +2 -1\n- b\n+ B\n+ d"
	if got != want {
		t.Fatalf("display=%q want %q", got, want)
	}
}

func TestWriteFileTooLarge(t *testing.T) {
	r, _ := newWriteReg(t, true)
	big := make([]byte, 65<<10)
	for i := range big {
		big[i] = 'x'
	}
	args, _ := json.Marshal(map[string]string{"path": "big.txt", "content": string(big)})
	if res := r.Call(context.Background(), "write_file", string(args)); !res.IsError {
		t.Fatal("expected size reject")
	}
}
