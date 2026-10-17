package usercmd

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAndExpand(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	ws := t.TempDir()
	write(t, filepath.Join(cfg, "miao/commands/review.md"), "---\ndescription: 用户版\n---\n用户 review $ARGS")
	write(t, filepath.Join(cfg, "miao/commands/hi.md"), "# 打个招呼\n你好")
	write(t, filepath.Join(ws, ".miao/commands/review.md"), "---\ndescription: \"审查代码\"\n---\n请审查 $ARGS，重点看并发。\n")
	write(t, filepath.Join(ws, ".miao/commands/help.md"), "覆盖内置")
	write(t, filepath.Join(ws, ".miao/commands/bad name.md"), "x")
	write(t, filepath.Join(ws, ".miao/commands/empty.md"), "---\ndescription: 空\n---\n")
	write(t, filepath.Join(ws, ".miao/commands/notes.txt"), "x")

	cmds := Load(ws, map[string]bool{"help": true})
	if len(cmds) != 2 || cmds[0].Name != "hi" || cmds[1].Name != "review" {
		t.Fatalf("cmds=%+v", cmds)
	}
	hi, rv := cmds[0], cmds[1]
	if hi.Description != "打个招呼" || hi.Source != "用户" || hi.TakesArgs() {
		t.Fatalf("hi=%+v", hi)
	}
	if rv.Description != "审查代码" || rv.Source != "项目" || !rv.TakesArgs() {
		t.Fatalf("项目级应覆盖用户级：%+v", rv)
	}
	if got := rv.Expand(" agent.go "); got != "请审查 agent.go，重点看并发。" {
		t.Fatalf("expand=%q", got)
	}
	if got := hi.Expand("顺便讲个笑话"); got != "# 打个招呼\n你好\n\n顺便讲个笑话" {
		t.Fatalf("无 $ARGS 时参数接在末尾：%q", got)
	}
}

func TestLoadOldProjectDirFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ws := t.TempDir()
	write(t, filepath.Join(ws, ".agnet/commands/old.md"), "旧目录命令")
	if cmds := Load(ws, nil); len(cmds) != 1 || cmds[0].Name != "old" {
		t.Fatalf("没有 .miao 时应读 .agnet：%+v", cmds)
	}
	write(t, filepath.Join(ws, ".miao/commands/new.md"), "新目录命令")
	if cmds := Load(ws, nil); len(cmds) != 1 || cmds[0].Name != "new" {
		t.Fatalf(".miao 存在时只读 .miao：%+v", cmds)
	}
}
