package tui

import (
	"strings"
	"testing"

	"agnet/internal/llm"
)

func TestSessionEditedFiles(t *testing.T) {
	call := func(name, args string) llm.ToolCall {
		var tc llm.ToolCall
		tc.Function.Name, tc.Function.Arguments = name, args
		return tc
	}
	hist := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			call("edit_file", `{"path":"./b.go"}`),
			call("read_file", `{"path":"x.go"}`),
			call("write_file", `{"path":"a.go"}`),
		}},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call("edit_file", `{"path":"b.go"}`)}},
	}
	got := strings.Join(sessionEditedFiles(hist), ",")
	if got != "a.go,b.go" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatDiffLines(t *testing.T) {
	diff := "diff --git a/x.go b/x.go\nindex 1..2 100644\n--- a/x.go\n+++ b/x.go\n@@ -1,2 +1,2 @@\n ctx\n-old\n+new\n"
	got := strings.Join(formatDiffLines(diff), "|")
	if got != "── x.go|  ⋯|  ctx|- old|+ new" {
		t.Fatalf("got %q", got)
	}
}

func TestCommitApprovalFlow(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.git.busy = true
	_, cmd := m.handleGitMsg(commitMsgReady{msg: "feat: x", files: []string{"a.go"}})
	if m.approval == nil || m.approval.title() != "确认执行 git commit" || cmd == nil {
		t.Fatal("应弹出提交审批卡")
	}
	if !strings.Contains(m.approval.display, "a.go") {
		t.Fatal("审批卡应列出文件")
	}
	m.resolveApproval(false)
	dec := cmd().(commitDecision)
	if dec.allow {
		t.Fatal("拒绝后不应提交")
	}
	m.handleGitMsg(dec)
	if m.git.busy || !strings.Contains(m.messages[len(m.messages)-1].content, "已取消") {
		t.Fatal("拒绝后应提示已取消")
	}
}
