package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ainnhuomiao/miao/internal/appdir"
)

func setup(t *testing.T, user, project string) string {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("MIAO_HOME", cfg)
	ws := t.TempDir()
	if user != "" {
		os.MkdirAll(cfg, 0o755)
		os.WriteFile(filepath.Join(cfg, "config.json"), []byte(user), 0o644)
	}
	if project != "" {
		os.MkdirAll(filepath.Join(ws, ".miao"), 0o755)
		os.WriteFile(filepath.Join(ws, ".miao", "hooks.json"), []byte(project), 0o644)
	}
	return ws
}

func TestUserHooksRunWithoutApproval(t *testing.T) {
	ws := setup(t, `{"theme":"nord","hooks":{
		"pre_tool":[{"match":"run_command","command":"echo 不许跑 $MIAO_TOOL; exit 3"}],
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
	os.WriteFile(filepath.Join(ws, ".miao", "hooks.json"), []byte(`{"stop":[{"command":"touch ran2"}]}`), 0o644)
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

func TestProjectHooksOldDirAndTrustSurvivesRename(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MIAO_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	ws := t.TempDir()
	content := []byte(`{"stop":[{"command":"touch ran"}]}`)
	oldHooks := filepath.Join(ws, ".agnet", "hooks.json")
	os.MkdirAll(filepath.Dir(oldHooks), 0o755)
	os.WriteFile(oldHooks, content, 0o644)
	// 改名前的信任记录：~/.config/agnet/trusted_hooks.json，按旧路径记的哈希
	r, _ := Load(ws, nil)
	trust := `{"` + oldHooks + `": "` + r.projectHash + `"}`
	os.MkdirAll(filepath.Join(home, ".config", "agnet"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "agnet", "trusted_hooks.json"), []byte(trust), 0o600)
	// 模拟启动迁移
	appdir.Migrate()

	asked := 0
	approve := func() ApproveFunc {
		return func(context.Context, string) (bool, error) { asked++; return false, nil }
	}
	r, _ = Load(ws, approve)
	r.Stop(context.Background())
	if asked != 0 || !fileExists(filepath.Join(ws, "ran")) {
		t.Fatalf("回退读 .agnet 且已信任，不应再问：asked=%d", asked)
	}

	// 用户把 .agnet 改名成 .miao，内容不变仍算已信任
	os.Remove(filepath.Join(ws, "ran"))
	if err := os.Rename(filepath.Join(ws, ".agnet"), filepath.Join(ws, ".miao")); err != nil {
		t.Fatal(err)
	}
	r, _ = Load(ws, approve)
	r.Stop(context.Background())
	if asked != 0 || !fileExists(filepath.Join(ws, "ran")) {
		t.Fatalf("挪到 .miao 后应仍信任：asked=%d", asked)
	}

	// 内容变了就要重新确认
	os.WriteFile(filepath.Join(ws, ".miao", "hooks.json"), []byte(`{"stop":[{"command":"touch ran3"}]}`), 0o644)
	r, _ = Load(ws, approve)
	r.Stop(context.Background())
	if asked != 1 || fileExists(filepath.Join(ws, "ran3")) {
		t.Fatalf("内容变化应重新确认：asked=%d", asked)
	}
}

// PoC：hook 子进程原先继承完整环境，能读到 MIAO_API_KEY。
func TestHookEnvDoesNotLeakAPIKey(t *testing.T) {
	ws := setup(t, `{"hooks":{"stop":[{"command":"env > envout"}]}}`, "")
	t.Setenv("MIAO_API_KEY", "sk-hook-leak")
	t.Setenv("GITHUB_TOKEN", "ghp-hook-leak")
	r, _ := Load(ws, nil)
	r.Stop(context.Background())
	b, err := os.ReadFile(filepath.Join(ws, "envout"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "sk-hook-leak") || strings.Contains(string(b), "ghp-hook-leak") {
		t.Fatalf("hook 环境泄露密钥:\n%s", b)
	}
	if !strings.Contains(string(b), "MIAO_EVENT=stop") {
		t.Fatalf("hook 应仍能拿到 MIAO_EVENT:\n%s", b)
	}
}
