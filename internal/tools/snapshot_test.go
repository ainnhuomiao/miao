package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUndoChain(t *testing.T) {
	r, dir := newWriteReg(t, true)
	r.SetSnapshotDir(filepath.Join(t.TempDir(), "snap"))
	p := filepath.Join(dir, "a.txt")
	ctx := context.Background()
	r.Call(ctx, "write_file", `{"path":"a.txt","content":"v1"}`)
	r.Call(ctx, "edit_file", `{"path":"a.txt","old_string":"v1","new_string":"v2"}`)
	r.Call(ctx, "write_file", `{"path":"a.txt","content":"v3"}`)
	if r.UndoDepth() != 3 {
		t.Fatalf("应有 3 次可撤销，得到 %d", r.UndoDepth())
	}
	for _, want := range []string{"v2", "v1"} {
		if _, err := r.Undo(); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(p); string(b) != want {
			t.Fatalf("想要 %q，得到 %q", want, b)
		}
	}
	if rel, err := r.Undo(); err != nil || rel != "a.txt" {
		t.Fatal(rel, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("撤销新建应删除文件")
	}
	if _, err := r.Undo(); err == nil {
		t.Fatal("栈空时应报错")
	}
}

func TestUndoSkipsRejected(t *testing.T) {
	r, _ := newWriteReg(t, false)
	r.Call(context.Background(), "write_file", `{"path":"a.txt","content":"x"}`)
	if r.UndoDepth() != 0 {
		t.Fatal("被拒绝的写入不应留快照")
	}
}
