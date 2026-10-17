package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestFillBackground(t *testing.T) {
	var buf bytes.Buffer
	fillOut, fillBg, fillShown = &buf, false, false
	defer func() { fillOut, fillBg, fillShown = nil, false, false; setTheme("auto") }()

	setTheme("nord")
	if buf.Len() != 0 {
		t.Fatalf("默认关闭时不应输出转义：%q", buf.String())
	}
	fillBg = true
	setTheme("nord")
	if !strings.Contains(buf.String(), "\x1b]11;#2e3440") {
		t.Fatalf("打开后应把背景设成 base：%q", buf.String())
	}
	buf.Reset()
	fillBg = false
	syncFill()
	if buf.String() != "\x1b]111\x1b\\" {
		t.Fatalf("关闭后应恢复终端背景：%q", buf.String())
	}
}
