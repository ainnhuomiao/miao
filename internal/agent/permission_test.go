package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
)

func TestPermissionDecisions(t *testing.T) {
	cmd := func(c string, a ...string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"command": c, "args": a})
		return b
	}
	edit := json.RawMessage(`{"path":"a.go"}`)
	cases := []struct {
		name    string
		mode    PermissionMode
		allow   []string
		session []struct {
			tool string
			args json.RawMessage
		}
		tool        string
		args        json.RawMessage
		wantAsked   bool
		wantAllowed bool
	}{
		{name: "询问模式问用户", mode: ModeAsk, tool: "edit_file", args: edit, wantAsked: true, wantAllowed: true},
		{name: "自动同意编辑 edit", mode: ModeAcceptEdits, tool: "edit_file", args: edit, wantAllowed: true},
		{name: "自动同意编辑 write", mode: ModeAcceptEdits, tool: "write_file", args: edit, wantAllowed: true},
		{name: "自动同意编辑 命令仍问", mode: ModeAcceptEdits, tool: "run_command", args: cmd("rm", "x"), wantAsked: true, wantAllowed: true},
		{name: "只读规划拒编辑", mode: ModePlan, tool: "edit_file", args: edit},
		{name: "只读规划拒命令", mode: ModePlan, allow: []string{"go test"}, tool: "run_command", args: cmd("go", "test", "./...")},
		{name: "白名单前缀", allow: []string{"go test"}, tool: "run_command", args: cmd("go", "test", "./..."), wantAllowed: true},
		{name: "白名单按词边界", allow: []string{"go test"}, tool: "run_command", args: cmd("go", "testx"), wantAsked: true, wantAllowed: true},
		{name: "白名单拒绝 shell 元字符", allow: []string{"go test"}, tool: "run_command", args: cmd("go", "test", ";rm"), wantAsked: true, wantAllowed: true},
		{name: "白名单不放行 -exec", allow: []string{"go test"}, tool: "run_command", args: cmd("go", "test", "-exec", "./evil"), wantAsked: true, wantAllowed: true},
		{name: "白名单不放行 -toolexec=", allow: []string{"go test"}, tool: "run_command", args: cmd("go", "test", "-toolexec=./evil"), wantAsked: true, wantAllowed: true},
		{name: "白名单不放行 &&", allow: []string{"go test"}, tool: "run_command", args: cmd("go", "test", "&&", "rm", "-rf", "x"), wantAsked: true, wantAllowed: true},
		{name: "白名单不放行换行", allow: []string{"go test"}, tool: "run_command", args: cmd("go", "test\nrm"), wantAsked: true, wantAllowed: true},
		{name: "白名单不放行反引号和重定向", allow: []string{"go test"}, tool: "run_command", args: cmd("go", "test", "`id`", ">x"), wantAsked: true, wantAllowed: true},
		{name: "sh -c 即使在白名单也要审批", allow: []string{"sh", "bash -c"}, tool: "run_command", args: cmd("sh", "-c", "rm -rf x"), wantAsked: true, wantAllowed: true},
		{name: "带路径的 bash 也要审批", allow: []string{"/bin/bash"}, tool: "run_command", args: cmd("/bin/bash", "-c", "id"), wantAsked: true, wantAllowed: true},
		{name: "env 包装也要审批", allow: []string{"env"}, tool: "run_command", args: cmd("env", "go", "test"), wantAsked: true, wantAllowed: true},
		{name: "go run 即使白名单写 go 也要审批", allow: []string{"go"}, tool: "run_command", args: cmd("go", "run", "./x"), wantAsked: true, wantAllowed: true},
		{name: "白名单 grep 不放行 grep -r", allow: []string{"grep"}, tool: "run_command", args: cmd("grep", "-rn", "KEY"), wantAsked: true, wantAllowed: true},
		{name: "白名单 rg 不放行 rg -uu", allow: []string{"rg"}, tool: "run_command", args: cmd("rg", "-uu", "KEY"), wantAsked: true, wantAllowed: true},
		{name: "白名单 rg 不放行 rg --hidden", allow: []string{"rg"}, tool: "run_command", args: cmd("rg", "--hidden", "KEY"), wantAsked: true, wantAllowed: true},
		{name: "白名单 sort 不放行 --compress-program", allow: []string{"sort"}, tool: "run_command", args: cmd("sort", "--compress-program=sh", "a"), wantAsked: true, wantAllowed: true},
		{name: "白名单 sort 不放行 -T", allow: []string{"sort"}, tool: "run_command", args: cmd("sort", "-T", "/tmp", "a"), wantAsked: true, wantAllowed: true},
		{name: "白名单 diff 不放行 -r", allow: []string{"diff"}, tool: "run_command", args: cmd("diff", "-r", "a", "b"), wantAsked: true, wantAllowed: true},
		{name: "白名单 grep 不放行 --recurs", allow: []string{"grep"}, tool: "run_command", args: cmd("grep", "--recurs", "KEY", "."), wantAsked: true, wantAllowed: true},
		{name: "白名单 go vet 不放行 -vettool", allow: []string{"go vet"}, tool: "run_command", args: cmd("go", "vet", "-vettool=./evil"), wantAsked: true, wantAllowed: true},
		{name: "会话放行编辑整类", session: []struct {
			tool string
			args json.RawMessage
		}{{"edit_file", edit}}, tool: "write_file", args: json.RawMessage(`{"path":"b.go"}`), wantAllowed: true},
		{name: "会话放行命令只认同一条", session: []struct {
			tool string
			args json.RawMessage
		}{{"run_command", cmd("make", "build")}}, tool: "run_command", args: cmd("make", "clean"), wantAsked: true, wantAllowed: true},
		{name: "会话放行同一命令", session: []struct {
			tool string
			args json.RawMessage
		}{{"run_command", cmd("make", "build")}}, tool: "run_command", args: cmd("make", " build"), wantAllowed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := New(nil, "", nil)
			a.SetMode(tc.mode)
			a.SetAllowedCommands(tc.allow)
			for _, s := range tc.session {
				a.AllowForSession(s.tool, s.args)
			}
			asked := false
			ap := a.wrapApprover(func(context.Context, string, string, json.RawMessage) (bool, error) {
				asked = true
				return true, nil
			})
			got, err := ap(context.Background(), tc.tool, "", tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if asked != tc.wantAsked || got != tc.wantAllowed {
				t.Fatalf("asked=%v allowed=%v, want %v %v", asked, got, tc.wantAsked, tc.wantAllowed)
			}
		})
	}
}

func TestCycleModeAndReset(t *testing.T) {
	a := New(nil, "", nil)
	want := []PermissionMode{ModeAcceptEdits, ModePlan, ModeAsk}
	for _, w := range want {
		if got := a.CycleMode(); got != w {
			t.Fatalf("got %v want %v", got, w)
		}
	}
	a.AllowForSession("edit_file", nil)
	a.Reset()
	if d, _ := a.perm.decide("edit_file", nil, ""); d {
		t.Fatal("Reset 后会话放行应清空")
	}
}

func TestPlanModePromptAndEnv(t *testing.T) {
	a := New(nil, "SYS", nil)
	if strings.Contains(a.systemPrompt(), "只读规划") {
		t.Fatal("默认不应有规划说明")
	}
	a.SetMode(ModePlan)
	if a.systemPrompt() != "SYS" {
		t.Fatalf("规划模式不应改系统提示词：%q", a.systemPrompt())
	}
	if a.pendingModeNote == "" || !strings.Contains(a.pendingModeNote, "只读规划") {
		t.Fatalf("规划说明应写入历史备注：%q", a.pendingModeNote)
	}
	a.history = []llm.Message{{Role: llm.RoleUser, Content: "hi"}, {Role: llm.RoleAssistant, Content: "yo"}}
	a.CycleMode()
	a.CycleMode()
	if len(a.History()) != 2 {
		t.Fatalf("切模式不应立刻改历史：%+v", a.History())
	}
	if !strings.Contains(a.pendingModeNote, "每次询问") && !strings.Contains(a.pendingModeNote, "自动同意") {
		t.Fatalf("连按应只保留最后一次：%q", a.pendingModeNote)
	}
	t.Setenv(AllowCommandsEnv, " go test , git  status ,")
	a.SetAllowedCommands(AllowedCommandsFromEnv())
	if got := a.perm.allowCmds; len(got) != 2 || got[1] != "git status" {
		t.Fatalf("白名单解析：%q", got)
	}
	a.SetAllowedCommands(append(a.AllowedCommands(), "go test", "make"))
	if got := a.AllowedCommands(); len(got) != 3 || got[2] != "make" {
		t.Fatalf("应去重：%q", got)
	}
}

var _ tools.Approver = (&Agent{}).wrapApprover(nil)

func TestAllowlistDoesNotSkipDiffDirsOrGitExecConfig(t *testing.T) {
	t.Setenv("MIAO_HOME", t.TempDir())
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".env"), []byte("SECRET=real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "sub", ".env"), []byte("SECRET=fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := tools.NewRegistry(ws)
	if err != nil {
		t.Fatal(err)
	}
	ask := func(a *Agent, args json.RawMessage) (asked, allowed bool) {
		t.Helper()
		ap := a.wrapApprover(func(context.Context, string, string, json.RawMessage) (bool, error) {
			asked = true
			return true, nil
		})
		var err error
		allowed, err = ap(context.Background(), "run_command", "", args)
		if err != nil {
			t.Fatal(err)
		}
		return asked, allowed
	}
	cmd := func(c string, args ...string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"command": c, "args": args})
		return b
	}
	a := New(nil, "", r)
	a.SetAllowedCommands([]string{"diff", "git"})
	if asked, allowed := ask(a, cmd("diff", ".", "sub")); !asked || !allowed {
		t.Fatalf("diff . sub 应询问，asked=%v allowed=%v", asked, allowed)
	}
	if asked, allowed := ask(a, cmd("diff", "-u", "a.txt", "b.txt")); asked || !allowed {
		t.Fatalf("diff 两个文件应直接放行，asked=%v allowed=%v", asked, allowed)
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = ws
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	if asked, allowed := ask(a, cmd("git", "status")); asked || !allowed {
		t.Fatalf("普通仓库的 git status 应直接放行，asked=%v allowed=%v", asked, allowed)
	}
	git("config", "filter.evil.clean", "evil")
	if asked, allowed := ask(a, cmd("git", "status")); !asked || !allowed {
		t.Fatalf("含 filter 的未信任仓库应询问，asked=%v allowed=%v", asked, allowed)
	}
	if err := os.Mkdir(filepath.Join(ws, ".miao"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".miao", "config.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.TrustProject(ws); err != nil {
		t.Fatal(err)
	}
	if asked, allowed := ask(a, cmd("git", "status")); asked || !allowed {
		t.Fatalf("信任项目配置后 git status 应直接放行，asked=%v allowed=%v", asked, allowed)
	}
}
