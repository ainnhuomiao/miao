package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLoadAgentsMD(t *testing.T) {
	big := strings.Repeat("火", MaxAgentsMDBytes) // 3 字节一个字，远超上限
	cases := []struct {
		name      string
		file      *string
		wantText  string
		wantTrunc bool
	}{
		{name: "不存在", file: nil},
		{name: "空文件", file: ptr("  \n\n")},
		{name: "正常", file: ptr("\n用 gofmt。\n"), wantText: "用 gofmt。"},
		{name: "超限截断", file: &big, wantTrunc: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.file != nil {
				if err := os.WriteFile(filepath.Join(dir, AgentsMDName), []byte(*tc.file), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, trunc, err := LoadAgentsMD(dir)
			if err != nil {
				t.Fatal(err)
			}
			if trunc != tc.wantTrunc {
				t.Fatalf("truncated=%v want %v", trunc, tc.wantTrunc)
			}
			if tc.wantTrunc {
				if len(got) > MaxAgentsMDBytes || !utf8.ValidString(got) || len(got) < MaxAgentsMDBytes-3 {
					t.Fatalf("截断结果长度 %d 或 UTF-8 不合法", len(got))
				}
				return
			}
			if got != tc.wantText {
				t.Fatalf("got %q want %q", got, tc.wantText)
			}
		})
	}
}

func TestLoadAgentsMDDirAndEmptyWorkspace(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, AgentsMDName), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _, err := LoadAgentsMD(dir); err != nil || got != "" {
		t.Fatalf("目录应被忽略: %q %v", got, err)
	}
	if got, _, err := LoadAgentsMD(""); err != nil || got != "" {
		t.Fatalf("空工作区: %q %v", got, err)
	}
}

func TestWithAgentsMD(t *testing.T) {
	if got := WithAgentsMD("SYS", "", false); got != "SYS" {
		t.Fatalf("空内容应原样返回，got %q", got)
	}
	got := WithAgentsMD("SYS", "规则", false)
	if !strings.HasPrefix(got, "SYS\n\n") || !strings.Contains(got, "<agents_md>\n规则\n</agents_md>") || strings.Contains(got, "截断") {
		t.Fatalf("拼接不对: %q", got)
	}
	if !strings.Contains(WithAgentsMD("SYS", "规则", true), "已截断") {
		t.Fatal("截断时应注明")
	}
}

func TestAgentsMDLoadedFlag(t *testing.T) {
	var nilAg *Agent
	if nilAg.AgentsMDLoaded() {
		t.Fatal("nil agent 应为 false")
	}
	a := New(nil, "", nil)
	a.SetAgentsMDLoaded(true)
	if !a.AgentsMDLoaded() {
		t.Fatal("应为 true")
	}
}

func ptr(s string) *string { return &s }
