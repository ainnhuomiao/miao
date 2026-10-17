package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMentionedPaths(t *testing.T) {
	got := MentionedPaths("看 @a.go 和 @sub/b.md，还有 x@y.com 以及 @a.go。")
	if strings.Join(got, ",") != "a.go,sub/b.md" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandMentions(t *testing.T) {
	r := newSearchRegistry(t, map[string]string{
		"a.go":       "package a\n",
		"sub/b.md":   "# B",
		".env":       "KEY=1",
		"bin.dat":    "a\x00b",
		"big.txt":    strings.Repeat("火", mentionMaxFileBytes),
		".gitignore": "ignored.txt\n",
	})
	out, att, warn := r.ExpandMentions("改 @a.go 参考 @sub/b.md 找 @某人 @nope.go @.env @bin.dat @big.txt @../etc/passwd")
	if strings.Join(att, ",") != "a.go,sub/b.md,big.txt" {
		t.Fatalf("attached=%q", att)
	}
	for _, want := range []string{`<file path="a.go">` + "\npackage a\n</file>", `<file path="sub/b.md">`, `truncated=`} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q", want)
		}
	}
	if !strings.HasPrefix(out, "改 @a.go") {
		t.Fatal("原文应保留在开头")
	}
	ws := strings.Join(warn, "\n")
	if !strings.Contains(ws, "@.env 未附加：敏感文件") || !strings.Contains(ws, "@bin.dat 未附加：不是文本文件") {
		t.Fatalf("warnings=%q", warn)
	}
	if strings.Contains(ws, "某人") || strings.Contains(ws, "nope") || strings.Contains(ws, "passwd") || strings.Contains(out, "root:") {
		t.Fatalf("不存在/越界的 @ 应静默忽略：%q", warn)
	}
	if same, att, _ := r.ExpandMentions("没有引用"); same != "没有引用" || att != nil {
		t.Fatal("无引用应原样返回")
	}
}

func TestListFilesSkipsIgnoredAndSensitive(t *testing.T) {
	r := newSearchRegistry(t, map[string]string{
		"b.go": "", "a/c.go": "", ".env": "", "ignored.txt": "", ".gitignore": "ignored.txt\n", ".git/HEAD": "",
	})
	got, err := r.ListFiles(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != ".gitignore,a/c.go,b.go" {
		t.Fatalf("got %q", got)
	}
	if got, _ := r.ListFiles(context.Background(), 1); len(got) != 1 {
		t.Fatalf("limit 无效：%q", got)
	}
}

func TestExpandMentionsBoundary(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_rsa"), []byte("SECRET-KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newSearchRegistry(t, map[string]string{"ok.go": "package ok"})
	if err := os.Symlink(filepath.Join(outside, "id_rsa"), filepath.Join(r.workspace, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.workspace, "dirlink")); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(outside, "id_rsa")
	out, att, warn := r.ExpandMentions("@../../.ssh/id_rsa @" + abs + " @/etc/passwd @link @dirlink/id_rsa @ok.go")
	if strings.Contains(out, "SECRET-KEY") || strings.Contains(out, "root:") {
		t.Fatalf("越界内容被附加：%q", out)
	}
	if strings.Join(att, ",") != "ok.go" {
		t.Fatalf("attached=%q", att)
	}
	if ws := strings.Join(warn, "\n"); !strings.Contains(ws, "@link 未附加") || !strings.Contains(ws, "@dirlink/id_rsa 未附加") {
		t.Fatalf("符号链接越界应提示：%q", warn)
	}
}
