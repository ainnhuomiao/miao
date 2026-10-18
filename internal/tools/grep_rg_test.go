package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func needRg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("未安装 rg")
	}
}

// withoutRg 让 grep 只走 Go 实现。
func withoutRg(t *testing.T) {
	t.Helper()
	old := rgLookPath
	rgLookPath = func() (string, error) { return "", errors.New("no rg") }
	t.Cleanup(func() { rgLookPath = old })
}

func grepCall(t *testing.T, r *Registry, args map[string]any) Result {
	t.Helper()
	b, _ := json.Marshal(args)
	return r.Call(context.Background(), "grep", string(b))
}

func TestRgArgvSafe(t *testing.T) {
	argv := rgArgv("--pre=sh", true, "/ws")
	if argv[0] != "--no-config" {
		t.Fatalf("必须带 --no-config：%v", argv)
	}
	n := len(argv)
	if argv[n-4] != "-e" || argv[n-3] != "--pre=sh" || argv[n-2] != "--" || argv[n-1] != "/ws" {
		t.Fatalf("pattern 应走 -e，路径在 -- 之后：%v", argv)
	}
	for _, a := range argv[:n-4] {
		for _, bad := range []string{"-L", "--follow", "--hidden", "--pre", "-z", "--search-zip", "-u", "-uu", "-uuu", "--no-ignore"} {
			if a == bad || strings.HasPrefix(a, bad+"=") {
				t.Fatalf("禁止的参数 %q：%v", a, argv)
			}
		}
	}
}

func TestRgPatternLikeOptionIsLiteral(t *testing.T) {
	needRg(t)
	r := newSearchRegistry(t, map[string]string{"a.txt": "x --pre=sh y\nother\n"})
	res := grepCall(t, r, map[string]any{"pattern": "--pre=sh"})
	if res.IsError || res.Content != "a.txt:1: x --pre=sh y" {
		t.Fatalf("应按字面量搜索：%+v", res)
	}
}

func TestRgConfigPathIgnored(t *testing.T) {
	needRg(t)
	r := newSearchRegistry(t, map[string]string{"a.txt": "needle\nhay\n"})
	cfgDir := t.TempDir()
	marker := filepath.Join(cfgDir, "pre-ran")
	script := filepath.Join(cfgDir, "pre.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\ncat \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(cfgDir, "rgrc")
	if err := os.WriteFile(cfg, []byte("--pre="+script+"\n--invert-match\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RIPGREP_CONFIG_PATH", cfg)
	res := grepCall(t, r, map[string]any{"pattern": "needle"})
	if res.Content != "a.txt:1: needle" {
		t.Fatalf("配置文件应被忽略：%+v", res)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("--pre 被执行了")
	}
}

func TestRgSensitiveFilesFiltered(t *testing.T) {
	needRg(t)
	r := newSearchRegistry(t, map[string]string{
		".env":           "needle=secret1\n",
		"sub/.env.local": "needle=secret2\n",
		"a.txt":          "needle ok\n",
	})
	res := grepCall(t, r, map[string]any{"pattern": "needle"})
	if strings.Contains(res.Content, "secret") || res.Content != "a.txt:1: needle ok" {
		t.Fatalf(".env 内容不应返回：%+v", res)
	}
	// 即使 rg 报出了敏感文件（如将来改了 hidden 策略），过滤层也要拦下。
	gi := loadGitIgnore(r.workspace)
	for _, f := range []string{".env", "sub/.env.local", ".git/config"} {
		if r.rgFileAllowed(gi, filepath.Join(r.workspace, f), "", r.workspace, true) {
			t.Fatalf("%s 应被过滤", f)
		}
	}
}

func TestRgSymlinkOutsideNotFollowed(t *testing.T) {
	needRg(t)
	r := newSearchRegistry(t, map[string]string{"a.txt": "hay\n"})
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "s.txt"), []byte("needle outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(r.workspace, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(out, "s.txt"), filepath.Join(r.workspace, "link.txt")); err != nil {
		t.Fatal(err)
	}
	res := grepCall(t, r, map[string]any{"pattern": "needle"})
	if res.Content != "(无匹配)" {
		t.Fatalf("不应跟随越界符号链接：%+v", res)
	}
	for _, p := range []string{"linkdir", "link.txt"} {
		if res := grepCall(t, r, map[string]any{"pattern": "needle", "path": p}); !res.IsError {
			t.Fatalf("path=%s 应被越界检查拒绝：%+v", p, res)
		}
	}
}

func TestGrepFallbackWithoutRg(t *testing.T) {
	r := newSearchRegistry(t, map[string]string{"a.txt": "needle\n"})
	t.Setenv("PATH", t.TempDir())
	if _, err := rgLookPath(); err == nil {
		t.Fatal("PATH 为空目录时不应找到 rg")
	}
	res := grepCall(t, r, map[string]any{"pattern": "needle"})
	if res.Content != "a.txt:1: needle" {
		t.Fatalf("应退回 Go 实现：%+v", res)
	}
}

func TestRgParityWithGo(t *testing.T) {
	needRg(t)
	long := strings.Repeat("é", 400) + " needle"
	files := map[string]string{
		".gitignore":      "/build/\n*.log\n",
		"main.go":         "package main\n// Needle here\nfunc needle() {}\n",
		"a/b/c.go":        "needle in c\r\nno\r\nneedle again\r\n",
		"a/readme.md":     "needle md\n",
		"a/long.txt":      "needle " + long + "\n",
		"build/out.go":    "needle ignored\n",
		"x.log":           "needle log\n",
		"bin.dat":         "needle\x00binary\n",
		"nested/z/q.txt":  "needleneedle\nneedle\n",
		"uni/中文.txt":      "needle 中文\n",
		"last-no-newline": "needle",
	}
	r := newSearchRegistry(t, files)
	var many strings.Builder
	for i := 0; i < 150; i++ {
		many.WriteString("needle line\n")
	}
	if err := os.WriteFile(filepath.Join(r.workspace, "many.txt"), []byte(many.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []map[string]any{
		{"pattern": "needle"},
		{"pattern": "needle", "ignore_case": true},
		{"pattern": "needle", "glob": "*.go"},
		{"pattern": "needle", "glob": "**/*.go"},
		{"pattern": "needle", "path": "a"},
		{"pattern": "needle", "path": "a/b/c.go"},
		{"pattern": "needle", "path": "build"},
		{"pattern": `^needle$`},
		{"pattern": "nomatch-xyz"},
	}
	if _, ok := r.grepRipgrep(context.Background(), "needle", false, "", r.workspace, true); !ok {
		t.Fatal("rg 路径应直接成功，而不是退回 Go 实现")
	}
	for _, c := range cases {
		rgRes := grepCall(t, r, c)
		func() {
			withoutRg(t)
			goRes := grepCall(t, r, c)
			if rgRes != goRes {
				t.Errorf("%v 结果不一致\nrg: %+v\ngo: %+v", c, rgRes, goRes)
			}
		}()
	}
}
