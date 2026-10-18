package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUndoFileOnlyTouchesOneFile(t *testing.T) {
	r, dir := newWriteReg(t, true)
	r.SetSnapshotDir(filepath.Join(t.TempDir(), "snap"))
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("orig"), 0o644)
	r.Call(ctx, "write_file", `{"path":"a.txt","content":"a1"}`)
	r.Call(ctx, "write_file", `{"path":"b.txt","content":"b1"}`)
	r.Call(ctx, "write_file", `{"path":"a.txt","content":"a2"}`)
	if !r.HasSnapshot("a.txt") || r.HasSnapshot("c.txt") {
		t.Fatal("HasSnapshot wrong")
	}
	if err := r.UndoFile("a.txt"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "orig" {
		t.Fatalf("a.txt 应恢复为最初内容，得到 %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(b) != "b1" {
		t.Fatal("b.txt 不应被动")
	}
	if r.UndoDepth() != 1 || r.HasSnapshot("a.txt") {
		t.Fatalf("撤销栈应只剩 b.txt，深度 %d", r.UndoDepth())
	}
	if err := r.UndoFile("b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("新建文件撤销后应删除")
	}
	if err := r.UndoFile("a.txt"); err == nil {
		t.Fatal("没有快照时应报错")
	}
}

func TestPreviewFileGuards(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("1\n2\n3\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("K=V"), 0o644)
	os.WriteFile(filepath.Join(dir, "bin.dat"), []byte("a\x00b"), 0o644)
	os.WriteFile(filepath.Join(outside, "s.txt"), []byte("secret"), 0o644)
	os.Symlink(filepath.Join(outside, "s.txt"), filepath.Join(dir, "link.txt"))
	os.Symlink(outside, filepath.Join(dir, "out"))

	lines, more, err := PreviewFile(dir, "ok.txt", 2)
	if err != nil || len(lines) != 2 || !more {
		t.Fatal(lines, more, err)
	}
	for _, bad := range []string{".env", "bin.dat", "link.txt", "out/s.txt", "../x", ".git/config"} {
		if _, _, err := PreviewFile(dir, bad, 10); err == nil {
			t.Fatalf("%s 应被拒绝", bad)
		}
	}
}
