package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditFileReplacesUnique(t *testing.T) {
	r, dir := newWriteReg(t, true)
	p := filepath.Join(dir, "a.go")
	os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0o640)
	res := r.Call(context.Background(), "edit_file", `{"path":"a.go","old_string":"two","new_string":"2"}`)
	if res.IsError {
		t.Fatal(res.Content)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "one\n2\nthree\n" {
		t.Fatalf("got %q", b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Fatalf("权限应保留，得到 %v", fi.Mode().Perm())
	}
}

func TestEditFileErrors(t *testing.T) {
	r, dir := newWriteReg(t, true)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x x x"), 0o644)
	cases := map[string]string{
		`{"path":"a.txt","old_string":"x","new_string":"y"}`:    "3 次",
		`{"path":"a.txt","old_string":"zz","new_string":"y"}`:   "没有找到",
		`{"path":"a.txt","old_string":"","new_string":"y"}`:     "不能为空",
		`{"path":"nope.txt","old_string":"x","new_string":"y"}`: "不存在",
		`{"path":".env","old_string":"x","new_string":"y"}`:     "敏感",
	}
	for args, want := range cases {
		res := r.Call(context.Background(), "edit_file", args)
		if !res.IsError || !strings.Contains(res.Content, want) {
			t.Errorf("%s: 期望含 %q 的错误，得到 %q", args, want, res.Content)
		}
	}
}

func TestEditFileRejectedKeepsFile(t *testing.T) {
	r, dir := newWriteReg(t, false)
	p := filepath.Join(dir, "a.txt")
	os.WriteFile(p, []byte("hello"), 0o644)
	if res := r.Call(context.Background(), "edit_file", `{"path":"a.txt","old_string":"hello","new_string":"bye"}`); !res.IsError {
		t.Fatal("拒绝后应报错")
	}
	if b, _ := os.ReadFile(p); string(b) != "hello" {
		t.Fatalf("拒绝后文件不应变化：%q", b)
	}
}
