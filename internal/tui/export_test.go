package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/session"
)

func TestSessionMarkdown(t *testing.T) {
	got := sessionMarkdown("m", []llm.Message{
		{Role: llm.RoleUser, Content: "hi"},
		{Role: llm.RoleAssistant, Content: "yo", ToolCalls: []llm.ToolCall{{
			Function: llm.FunctionCall{Name: "list_dir", Arguments: `{}`},
		}}},
		{Role: llm.RoleTool, Name: "list_dir", Content: "a.go"},
	})
	for _, want := range []string{"## 用户", "hi", "## 助手", "yo", "list_dir", "a.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}

func TestExportRedactsAndUsesDataDir(t *testing.T) {
	m := newTestModel(t, 80, 24, "ok")
	secret := "sk-abcdefghijklmnopqrstuvwxyz"
	m.ag.SetHistory([]llm.Message{{Role: llm.RoleUser, Content: "key " + secret}})
	m.exportSession("")
	last := m.messages[len(m.messages)-1].content
	if !strings.HasPrefix(last, "已导出到 ") {
		t.Fatalf("notice: %q", last)
	}
	path := strings.TrimPrefix(last, "已导出到 ")
	if !strings.Contains(path, "exports") {
		t.Fatalf("应写到数据目录 exports：%s", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secret) {
		t.Fatalf("导出应脱敏：%s", body)
	}
}

func TestSessionMarkdownRedactHelper(t *testing.T) {
	raw := sessionMarkdown("m", []llm.Message{{Role: llm.RoleUser, Content: "sk-abcdefghijklmnopqrstuvwxyz"}})
	if !strings.Contains(session.RedactSecrets(raw), "sk-***") {
		t.Fatal(raw)
	}
}
