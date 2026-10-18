package main

import (
	"strings"
	"testing"
)

func TestMaskKey(t *testing.T) {
	if got := maskKey(""); got != "未设置" {
		t.Fatalf("%q", got)
	}
	if got := maskKey("short"); got != "***" {
		t.Fatalf("%q", got)
	}
	got := maskKey("sk-abcdefghij")
	if !strings.HasPrefix(got, "sk-") || !strings.Contains(got, "***") || strings.Contains(got, "abcdefgh") {
		t.Fatalf("%q", got)
	}
}

func TestClipboardBinsMatchPasteTools(t *testing.T) {
	want := []string{"wl-paste", "xclip", "pngpaste"}
	if strings.Join(clipboardBins, ",") != strings.Join(want, ",") {
		t.Fatalf("doctor 应检查粘贴图片用的工具，got %v", clipboardBins)
	}
}

func TestNonemptyAndTTYLabel(t *testing.T) {
	if nonempty("", "missing") != "missing" || nonempty("x", "missing") != "x" {
		t.Fatal("nonempty")
	}
}
