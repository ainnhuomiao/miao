package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T, user, project string) string {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	ws := t.TempDir()
	if user != "" {
		os.MkdirAll(filepath.Join(cfg, "agnet"), 0o755)
		os.WriteFile(filepath.Join(cfg, "agnet", "config.json"), []byte(user), 0o644)
	}
	if project != "" {
		os.MkdirAll(filepath.Join(ws, ".agnet"), 0o755)
		os.WriteFile(filepath.Join(ws, ".agnet", "hooks.json"), []byte(project), 0o644)
	}
	return ws
}

func TestUserHooksRunWithoutApproval(t *testing.T) {
	ws := setup(t, `{"theme":"nord","hooks":{
		"pre_tool":[{"match":"run_command","command":"echo 不许跑 $AGNET_TOOL; exit 3"}],
		"post_tool":[{"match":"edit_file|write_file","command":"cat > seen.json; echo gofmt 报错; exit 1"}],
		"stop":[{"command":"true"}]}}`, "")
	r, err := Load(ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if res := r.PreTool(ctx, "read_file", `{}`); res.Ran != 0 || res.Failed {
		t.Fatalf("match 不命中不应运行：%+v", res)
	}
	res := r.PreTool(ctx, "run_command", `{"command":"ls"}`)
	if !res.Failed || !strings.Contains(res.Output, "不许跑 run_command") {
		t.Fatalf("pre=%+v", res)
	}
	res = r.PostTool(ctx, "edit_file", `{"path":"a.go"}`, "已修改 a.go")
	if !res.Failed || !strings.Contains(res.Output, "gofmt 报错") {
		t.Fatalf("post=%+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(ws, "seen.json"))
	if !strings.Contains(string(data), `"tool":"edit_file"`) || !strings.Contains(string(data), "已修改 a.go") {
		t.Fatalf("stdin JSON=%s", data)
	}
	if res := r.Stop(ctx); res.Failed || res.Ran != 1 {
		t.Fatalf("stop=%+v", res)
	}
}

func TestProjectHooksNeedTrustAndRememberHash(t *testing.T) {
	ws := setup(t, "", `{"stop":[{"command":"touch ran"}]}`)
	asked := 0
	answer := false
	approve := func() ApproveFunc {
		return func(_ context.Context, display string) (bool, error) {
			asked++
			if !strings.Contains(display, "touch ran") {
				t.Errorf("确认卡应列出命令：%s", display)
			}
			return answer, nil
		}
	}
	r, _ := Load(ws, approve)
	r.Stop(context.Background())
	r.Stop(context.Background())
	if asked != 1 || fileExists(filepath.Join(ws, "ran")) {
		t.Fatalf("拒绝后本次运行不再问、也不执行：asked=%d", asked)
	}

	answer = true
	r, _ = Load(ws, approve)
	r.Stop(context.Background())
	if asked != 2 || !fileExists(filepath.Join(ws, "ran")) {
		t.Fatal("允许后应执行")
	}
	r, _ = Load(ws, approve)
	r.Stop(context.Background())
	if asked != 2 {
		t.Fatal("同一份文件已信任，不应再问")
	}
	os.WriteFile(filepath.Join(ws, ".agnet", "hooks.json"), []byte(`{"stop":[{"command":"touch ran2"}]}`), 0o644)
	answer = false
	r, _ = Load(ws, approve)
	r.Stop(context.Background())
	if asked != 3 || fileExists(filepath.Join(ws, "ran2")) {
		t.Fatal("文件改动后应重新确认")
	}
}

func TestProjectHooksSkippedWithoutApprover(t *testing.T) {
	ws := setup(t, "", `{"stop":[{"command":"touch ran"}]}`)
	r, _ := Load(ws, func() ApproveFunc { return nil })
	if res := r.Stop(context.Background()); res.Ran != 0 || fileExists(filepath.Join(ws, "ran")) {
		t.Fatal("无法确认时不能执行项目 hook")
	}
}

func TestHookTimeoutKillsChildren(t *testing.T) {
	ws := setup(t, `{"hooks":{"stop":[{"command":"sleep 30 & sleep 30","timeout":1}]}}`, "")
	r, _ := Load(ws, nil)
	start := time.Now()
	res := r.Stop(context.Background())
	if !res.Failed || !strings.Contains(res.Output, "超时") || time.Since(start) > 5*time.Second {
		t.Fatalf("res=%+v 用时 %s", res, time.Since(start))
	}
}

func TestBadConfigReportsError(t *testing.T) {
	ws := setup(t, `{"hooks":{"pre_tool":[{"match":"(","command":"x"}]}}`, `{not json`)
	r, err := Load(ws, nil)
	if err == nil || !r.Empty() {
		t.Fatalf("err=%v", err)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
