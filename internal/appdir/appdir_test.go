package appdir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	return home
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMigrateMovesOldDirs(t *testing.T) {
	home := setHome(t)
	write(t, filepath.Join(home, ".config/agnet/config.json"), `{"theme":"nord"}`)
	write(t, filepath.Join(home, ".local/share/agnet/sessions/a.json"), `{}`)

	notes := Migrate()
	if len(notes) != 2 {
		t.Fatalf("应有两条提示：%q", notes)
	}
	if notes[0] != "已把配置从 ~/.config/agnet 迁移到 ~/.config/miao" {
		t.Fatalf("提示文字：%q", notes[0])
	}
	if read(t, filepath.Join(home, ".config/miao/config.json")) != `{"theme":"nord"}` {
		t.Fatal("配置应搬到新目录")
	}
	if read(t, filepath.Join(home, ".local/share/miao/sessions/a.json")) != `{}` {
		t.Fatal("数据应搬到新目录")
	}
	if _, err := os.Stat(filepath.Join(home, ".config/agnet")); !os.IsNotExist(err) {
		t.Fatal("旧目录应已移走")
	}
	if again := Migrate(); len(again) != 0 {
		t.Fatalf("提示只出现一次：%q", again)
	}
}

func TestMigrateNeverOverwrites(t *testing.T) {
	home := setHome(t)
	write(t, filepath.Join(home, ".config/agnet/config.json"), "old")
	write(t, filepath.Join(home, ".config/miao/config.json"), "new")
	if notes := Migrate(); len(notes) != 0 {
		t.Fatalf("新目录已存在时不应迁移：%q", notes)
	}
	if read(t, filepath.Join(home, ".config/miao/config.json")) != "new" {
		t.Fatal("不能覆盖新目录")
	}
	if read(t, filepath.Join(home, ".config/agnet/config.json")) != "old" {
		t.Fatal("旧目录应原样保留")
	}
}

func TestMigrateRespectsXDG(t *testing.T) {
	setHome(t)
	cfg, data := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_DATA_HOME", data)
	write(t, filepath.Join(cfg, "agnet/mcp.json"), "{}")
	write(t, filepath.Join(data, "agnet/sessions/x.json"), "{}")
	if notes := Migrate(); len(notes) != 2 {
		t.Fatalf("notes=%q", notes)
	}
	if read(t, filepath.Join(cfg, "miao/mcp.json")) != "{}" || read(t, filepath.Join(data, "miao/sessions/x.json")) != "{}" {
		t.Fatal("应迁移到 XDG 目录下的 miao")
	}
	if d, _ := ConfigDir(); d != filepath.Join(cfg, "miao") {
		t.Fatalf("ConfigDir=%s", d)
	}
	if d, _ := DataDir(); d != filepath.Join(data, "miao") {
		t.Fatalf("DataDir=%s", d)
	}
}

func TestCopyTreeFallback(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agnet")
	write(t, filepath.Join(src, "a/b.txt"), "hi")
	if err := os.Symlink("a/b.txt", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "miao")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dst, "a/b.txt")) != "hi" || read(t, filepath.Join(dst, "link")) != "hi" {
		t.Fatal("递归复制应保留文件和符号链接")
	}
}

func TestProjectDirFallback(t *testing.T) {
	ws := t.TempDir()
	if got := ProjectDir(ws); got != filepath.Join(ws, ".miao") {
		t.Fatalf("都不存在时用 .miao：%s", got)
	}
	os.Mkdir(filepath.Join(ws, ".agnet"), 0o755)
	if got := ProjectDir(ws); got != filepath.Join(ws, ".agnet") {
		t.Fatalf("只有 .agnet 时回退：%s", got)
	}
	os.Mkdir(filepath.Join(ws, ".miao"), 0o755)
	if got := ProjectDir(ws); !strings.HasSuffix(got, ".miao") {
		t.Fatalf(".miao 存在时优先：%s", got)
	}
	if _, err := os.Stat(filepath.Join(ws, ".agnet")); err != nil {
		t.Fatal("不应移动项目里的 .agnet")
	}
}
