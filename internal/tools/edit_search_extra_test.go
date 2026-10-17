package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditFileExtraCases(t *testing.T) {
	cases := []struct {
		name, before, args, after string
	}{
		{"CRLF 保持原样", "a\r\nb\r\nc\r\n", `{"path":"f","old_string":"b","new_string":"B"}`, "a\r\nB\r\nc\r\n"},
		{"跨行匹配 CRLF", "a\r\nb\r\n", `{"path":"f","old_string":"a\r\nb","new_string":"ab"}`, "ab\r\n"},
		{"new_string 为空即删除", "keep\ndrop\nkeep2\n", `{"path":"f","old_string":"drop\n","new_string":""}`, "keep\nkeep2\n"},
		{"多行替换", "func a() {\n\treturn 1\n}\n", `{"path":"f","old_string":"\treturn 1\n","new_string":"\treturn 2\n\t// done\n"}`, "func a() {\n\treturn 2\n\t// done\n}\n"},
		{"中文", "你好世界", `{"path":"f","old_string":"世界","new_string":"火苗"}`, "你好火苗"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, dir := newWriteReg(t, true)
			p := filepath.Join(dir, "f")
			os.WriteFile(p, []byte(tc.before), 0o644)
			if res := r.Call(context.Background(), "edit_file", tc.args); res.IsError {
				t.Fatal(res.Content)
			}
			if b, _ := os.ReadFile(p); string(b) != tc.after {
				t.Fatalf("got %q want %q", b, tc.after)
			}
		})
	}
}

func TestEditFileSymlinkEscape(t *testing.T) {
	r, dir := newWriteReg(t, true)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("orig"), 0o644)
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Skip(err)
	}
	res := r.Call(context.Background(), "edit_file", `{"path":"link.txt","old_string":"orig","new_string":"pwned"}`)
	if !res.IsError {
		t.Fatal("经符号链接越出工作区应被拒绝")
	}
	if b, _ := os.ReadFile(outside); string(b) != "orig" {
		t.Fatalf("工作区外文件被改：%q", b)
	}
}

func TestGrepExtraCases(t *testing.T) {
	r := newSearchRegistry(t, map[string]string{
		"a.go":     "func Foo() {}\n",
		"b.md":     "Foo in docs\n",
		"sub/c.go": "// foo lower\n",
	})
	ctx := context.Background()
	if res := r.Call(ctx, "grep", `{"pattern":"("}`); !res.IsError {
		t.Fatalf("非法正则应报错：%q", res.Content)
	}
	res := r.Call(ctx, "grep", `{"pattern":"Foo","glob":"**/*.go"}`)
	if res.IsError || !strings.Contains(res.Content, "a.go:1:") || strings.Contains(res.Content, "b.md") {
		t.Fatalf("glob 过滤不对：%q", res.Content)
	}
	res = r.Call(ctx, "grep", `{"pattern":"(?i)foo","path":"sub"}`)
	if res.IsError || !strings.Contains(res.Content, "c.go:1:") || strings.Contains(res.Content, "a.go") {
		t.Fatalf("path 限定或 (?i) 不对：%q", res.Content)
	}
	if res := r.Call(ctx, "glob", `{"pattern":"**/*.rs"}`); res.IsError {
		t.Fatalf("无匹配不应报错：%q", res.Content)
	}
}
