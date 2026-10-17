package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 源码里处理的组合键都必须登记在 keyBindings 里，速查卡才不会漏。
func TestKeyBindingsCoverHandledKeys(t *testing.T) {
	known := map[string]bool{}
	for _, k := range keyBindings {
		for _, s := range strings.Split(k.keys, ",") {
			known[s] = true
		}
	}
	re := regexp.MustCompile(`"((?:ctrl|shift|alt)\+[a-z]+)"`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "keys.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, mm := range re.FindAllStringSubmatch(string(src), -1) {
			if !known[mm[1]] {
				t.Errorf("%s 处理了 %s，但 keyBindings 里没有登记", f, mm[1])
			}
		}
	}
}

func TestKeymapTextAligned(t *testing.T) {
	txt := keymapText()
	if !strings.Contains(txt, "Ctrl+B") || !strings.Contains(txt, "会话侧栏") {
		t.Fatalf("快捷键表缺少 Ctrl+B：\n%s", txt)
	}
}
