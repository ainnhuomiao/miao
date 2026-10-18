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
	t.Setenv("MIAO_HOME", "")
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

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestDirsUnderHome(t *testing.T) {
	home := setHome(t)
	g := filepath.Join(home, ".miao")
	for name, f := range map[string]func() (string, error){
		g: ConfigDir, filepath.Join(g, "data"): DataDir, filepath.Join(g, "state"): StateDir,
		filepath.Join(g, "credentials"): CredentialsPath,
	} {
		if got, _ := f(); got != name {
			t.Fatalf("got %s want %s", got, name)
		}
	}
	custom := t.TempDir()
	t.Setenv("MIAO_HOME", custom)
	if got, _ := DataDir(); got != filepath.Join(custom, "data") {
		t.Fatalf("MIAO_HOME 应生效：%s", got)
	}
}

func TestMigrateCopiesXDGAndKeepsOld(t *testing.T) {
	home := setHome(t)
	write(t, filepath.Join(home, ".config/miao/config.json"), `{"theme":"nord"}`)
	write(t, filepath.Join(home, ".local/share/miao/sessions/a.json"), `{}`)
	write(t, filepath.Join(home, ".local/state/miao/mcp/x.log"), `log`)

	notes := Migrate()
	if len(notes) != 1 || !strings.Contains(notes[0], "~/.miao") || !strings.Contains(notes[0], "手动删除") {
		t.Fatalf("应只有一条成功提示：%q", notes)
	}
	g := filepath.Join(home, ".miao")
	if read(t, filepath.Join(g, "config.json")) != `{"theme":"nord"}` ||
		read(t, filepath.Join(g, "data/sessions/a.json")) != `{}` ||
		read(t, filepath.Join(g, "state/mcp/x.log")) != "log" {
		t.Fatal("应复制到新布局")
	}
	if !exists(filepath.Join(home, ".config/miao/config.json")) || !exists(filepath.Join(home, ".local/share/miao/sessions/a.json")) {
		t.Fatal("旧目录必须保留")
	}
	if info, _ := os.Stat(g); info.Mode().Perm() != 0o700 {
		t.Fatalf("家目录权限应为 0700：%v", info.Mode().Perm())
	}
	if again := Migrate(); len(again) != 0 {
		t.Fatalf("提示只出现一次：%q", again)
	}
}

func TestMigrateAgnetChain(t *testing.T) {
	home := setHome(t)
	write(t, filepath.Join(home, ".config/agnet/config.json"), "agnet")
	write(t, filepath.Join(home, ".local/share/agnet/sessions/b.json"), "b")
	notes := Migrate()
	if len(notes) != 1 || !strings.Contains(notes[0], "~/.config/agnet") {
		t.Fatalf("notes=%q", notes)
	}
	if read(t, filepath.Join(home, ".miao/config.json")) != "agnet" || read(t, filepath.Join(home, ".miao/data/sessions/b.json")) != "b" {
		t.Fatal("agnet 目录也应迁移")
	}
}

func TestMigrateNeverOverwrites(t *testing.T) {
	home := setHome(t)
	write(t, filepath.Join(home, ".config/miao/config.json"), "old")
	write(t, filepath.Join(home, ".config/miao/same.json"), "same")
	write(t, filepath.Join(home, ".config/miao/new.json"), "fresh")
	write(t, filepath.Join(home, ".miao/config.json"), "new")
	write(t, filepath.Join(home, ".miao/same.json"), "same")
	notes := Migrate()
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "跳过了 1 个已存在的文件") || !strings.Contains(joined, "config.json") {
		t.Fatalf("应报告冲突：%q", notes)
	}
	if read(t, filepath.Join(home, ".miao/config.json")) != "new" {
		t.Fatal("不能覆盖新目录")
	}
	if read(t, filepath.Join(home, ".miao/new.json")) != "fresh" {
		t.Fatal("不冲突的文件照常复制")
	}
	if read(t, filepath.Join(home, ".config/miao/config.json")) != "old" {
		t.Fatal("旧目录应原样保留")
	}
}

func TestMigrateSkipsSymlinks(t *testing.T) {
	home := setHome(t)
	secret := filepath.Join(t.TempDir(), "secret")
	write(t, secret, "s")
	write(t, filepath.Join(home, ".config/miao/a.txt"), "a")
	if err := os.Symlink(secret, filepath.Join(home, ".config/miao/link")); err != nil {
		t.Fatal(err)
	}
	notes := Migrate()
	if exists(filepath.Join(home, ".miao/link")) {
		t.Fatal("不应复制符号链接")
	}
	if !strings.Contains(strings.Join(notes, "\n"), "符号链接") {
		t.Fatalf("应提示跳过的符号链接：%q", notes)
	}
}

func TestMigrateSymlinkedRootNotFollowed(t *testing.T) {
	home := setHome(t)
	real := t.TempDir()
	write(t, filepath.Join(real, "x.json"), "x")
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(home, ".config/miao")); err != nil {
		t.Fatal(err)
	}
	_ = Migrate()
	if exists(filepath.Join(home, ".miao/x.json")) {
		t.Fatal("旧目录本身是符号链接时不应跟随")
	}
}

func TestMigrateFailureKeepsRetrying(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 下权限不生效")
	}
	home := setHome(t)
	write(t, filepath.Join(home, ".config/miao/ok.json"), "ok")
	bad := filepath.Join(home, ".config/miao/bad.json")
	write(t, bad, "bad")
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}
	notes := Migrate()
	if !strings.Contains(strings.Join(notes, "\n"), "未完成") {
		t.Fatalf("部分失败应警告：%q", notes)
	}
	if read(t, filepath.Join(home, ".miao/ok.json")) != "ok" {
		t.Fatal("其它文件照常复制")
	}
	if !exists(bad) {
		t.Fatal("旧文件应保留")
	}
	_ = os.Chmod(bad, 0o644)
	notes = Migrate()
	if len(notes) != 1 || !strings.Contains(notes[0], "手动删除") {
		t.Fatalf("修好后重试应成功：%q", notes)
	}
}

func TestMigrateRespectsXDGAndMiaoHome(t *testing.T) {
	setHome(t)
	cfg, data, mh := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("MIAO_HOME", mh)
	write(t, filepath.Join(cfg, "miao/mcp.json"), "{}")
	write(t, filepath.Join(data, "agnet/sessions/x.json"), "{}")
	if notes := Migrate(); len(notes) != 1 {
		t.Fatalf("notes=%q", notes)
	}
	if read(t, filepath.Join(mh, "mcp.json")) != "{}" || read(t, filepath.Join(mh, "data/sessions/x.json")) != "{}" {
		t.Fatal("应迁移到 MIAO_HOME")
	}
}

func TestMigrateMiaoHomeInsideOld(t *testing.T) {
	home := setHome(t)
	write(t, filepath.Join(home, ".config/miao/a.json"), "a")
	t.Setenv("MIAO_HOME", filepath.Join(home, ".config/miao/inner"))
	notes := Migrate()
	if !strings.Contains(strings.Join(notes, "\n"), "互相包含") {
		t.Fatalf("notes=%q", notes)
	}
}

func TestOldDataDirs(t *testing.T) {
	home := setHome(t)
	got := OldDataDirs()
	if len(got) != 2 || got[0] != filepath.Join(home, ".local/share/miao") || got[1] != filepath.Join(home, ".local/share/agnet") {
		t.Fatalf("%q", got)
	}
}

func TestProjectDirFallback(t *testing.T) {
	setHome(t)
	ws := t.TempDir()
	if got := ProjectDir(ws); got != filepath.Join(ws, ".miao") {
		t.Fatalf("都不存在时用 .miao：%s", got)
	}
	_ = os.Mkdir(filepath.Join(ws, ".agnet"), 0o755)
	if got := ProjectDir(ws); got != filepath.Join(ws, ".agnet") {
		t.Fatalf("只有 .agnet 时回退：%s", got)
	}
	_ = os.Mkdir(filepath.Join(ws, ".miao"), 0o755)
	if got := ProjectDir(ws); !strings.HasSuffix(got, ".miao") {
		t.Fatalf(".miao 存在时优先：%s", got)
	}
	if _, err := os.Stat(filepath.Join(ws, ".agnet")); err != nil {
		t.Fatal("不应移动项目里的 .agnet")
	}
}

func TestProjectDirNotGlobalHome(t *testing.T) {
	home := setHome(t)
	_ = os.Mkdir(filepath.Join(home, ".miao"), 0o700)
	if got := ProjectDir(home); got != "" {
		t.Fatalf("工作区是 $HOME 时不能把 ~/.miao 当项目目录：%q", got)
	}
	// 通过符号链接进入 $HOME 也一样
	link := filepath.Join(t.TempDir(), "h")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	if got := ProjectDir(link); got != "" {
		t.Fatalf("符号链接指向 $HOME：%q", got)
	}
	// MIAO_HOME 本身作为工作区，或 MIAO_HOME 就是某工作区的 .miao
	ws := t.TempDir()
	t.Setenv("MIAO_HOME", filepath.Join(ws, ".miao"))
	if got := ProjectDir(ws); got != "" {
		t.Fatalf("MIAO_HOME=<ws>/.miao：%q", got)
	}
	mh := t.TempDir()
	t.Setenv("MIAO_HOME", mh)
	if got := ProjectDir(mh); got != "" {
		t.Fatalf("工作区就是 MIAO_HOME：%q", got)
	}
	if got := ProjectDir(ws); got != filepath.Join(ws, ".miao") {
		t.Fatalf("普通工作区不受影响：%q", got)
	}
}

func TestMigratePermissions(t *testing.T) {
	home := setHome(t)
	src := filepath.Join(home, ".local/share/miao")
	write(t, filepath.Join(src, "sub/deep/a.json"), `{}`)
	write(t, filepath.Join(src, "b.txt"), "b")
	if err := os.Chmod(filepath.Join(src, "b.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(src, "sub/deep"), 0o777); err != nil {
		t.Fatal(err)
	}
	Migrate()
	d := filepath.Join(home, ".miao/data")
	for _, f := range []string{"sub/deep/a.json", "b.txt"} {
		info, err := os.Stat(filepath.Join(d, f))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s 权限应为 0600：%v %v", f, info, err)
		}
	}
	for _, dir := range []string{"", "sub", "sub/deep"} {
		info, err := os.Stat(filepath.Join(d, dir))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("目录 %q 权限应为 0700：%v %v", dir, info, err)
		}
	}
}
