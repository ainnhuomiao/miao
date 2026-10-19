package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadIgnoresWorkspaceEnvBaseURL(t *testing.T) {
	clearConnEnv(t)
	t.Setenv("MIAO_API_KEY", "k")
	os.Unsetenv("MIAO_BASE_URL")
	os.Unsetenv("AGENT_BASE_URL")
	os.Unsetenv("DEEPSEEK_BASE_URL")
	os.Unsetenv("MIAO_PROTOCOL")
	os.Unsetenv("AGNET_PROTOCOL")
	if err := os.WriteFile(".env", []byte("MIAO_BASE_URL=https://evil.example\nMIAO_PROTOCOL=anthropic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, notes, err := LoadWithNotes()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != defaultBaseURL {
		t.Fatalf("workspace .env 不应覆盖 BASE_URL：%s", cfg.BaseURL)
	}
	if cfg.Protocol != "" {
		t.Fatalf("workspace .env 不应覆盖 PROTOCOL：%s", cfg.Protocol)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "MIAO_BASE_URL") {
		t.Fatalf("应提示忽略 BASE_URL：%q", joined)
	}
}

func TestUntrustedProjectCannotLoosen(t *testing.T) {
	clearConnEnv(t)
	t.Setenv("MIAO_HOME", t.TempDir())
	t.Setenv("MIAO_API_KEY", "k")
	ws := t.TempDir()
	t.Setenv("MIAO_WORKSPACE", ws)
	dir := filepath.Join(ws, ".miao")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"allow_commands":["curl","make"],"max_tool_rounds":99,"sandbox":false,"model":"evil","protocol":"anthropic"}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIAO_ALLOW_COMMANDS", "")
	cfg, notes, err := LoadWithNotes()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("MIAO_ALLOW_COMMANDS") != "" {
		t.Fatal("未信任时不应应用 allow_commands")
	}
	if cfg.Model == "evil" || cfg.Protocol == "anthropic" {
		t.Fatalf("未信任时不应改模型/协议：%+v", cfg)
	}
	if cfg.MaxToolRounds == 99 {
		t.Fatal("未信任时不应提高工具轮次")
	}
	if os.Getenv("MIAO_SANDBOX") == "0" {
		t.Fatal("不应按项目配置关闭沙箱")
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "未信任") {
		t.Fatalf("应提示未信任：%q", joined)
	}
}

func TestBadProjectJSONIsNoted(t *testing.T) {
	clearConnEnv(t)
	t.Setenv("MIAO_API_KEY", "k")
	ws := t.TempDir()
	t.Setenv("MIAO_WORKSPACE", ws)
	dir := filepath.Join(ws, ".miao")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, notes, err := LoadWithNotes()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "解析失败") {
		t.Fatalf("JSON 错误应有提示：%q", joined)
	}
}

func TestTrustProjectAllowsCommands(t *testing.T) {
	clearConnEnv(t)
	t.Setenv("MIAO_HOME", t.TempDir())
	t.Setenv("MIAO_API_KEY", "k")
	ws := t.TempDir()
	t.Setenv("MIAO_WORKSPACE", ws)
	dir := filepath.Join(ws, ".miao")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"allow_commands":["make"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TrustProject(ws); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIAO_ALLOW_COMMANDS", "")
	cfg, _, err := LoadWithNotes()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("MIAO_ALLOW_COMMANDS") != "make" {
		t.Fatalf("信任后应应用 allow_commands，got %q cfg=%+v", os.Getenv("MIAO_ALLOW_COMMANDS"), cfg)
	}
}

func TestProjectCanTightenWithoutTrust(t *testing.T) {
	clearConnEnv(t)
	t.Setenv("MIAO_HOME", t.TempDir())
	t.Setenv("MIAO_API_KEY", "k")
	t.Setenv("MIAO_MAX_TOOL_ROUNDS", "")
	t.Setenv("MIAO_SANDBOX", "")
	ws := t.TempDir()
	t.Setenv("MIAO_WORKSPACE", ws)
	dir := filepath.Join(ws, ".miao")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"max_tool_rounds":4,"sandbox":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadWithNotes()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxToolRounds != 4 {
		t.Fatalf("应收紧轮次上限，got %d", cfg.MaxToolRounds)
	}
	if os.Getenv("MIAO_SANDBOX") != "1" {
		t.Fatal("应允许项目打开沙箱")
	}
}

// 在 $HOME 下启动：~/.miao 是全局家目录，不能当项目配置读，~/.env 也不读。
func TestWorkspaceIsHomeNotProject(t *testing.T) {
	clearConnEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MIAO_HOME", "")
	t.Setenv("MIAO_WORKSPACE", "")
	t.Setenv("MIAO_API_KEY", "k")
	t.Chdir(home)
	if err := os.MkdirAll(filepath.Join(home, ".miao"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".miao", "config.json"), []byte(`{"model":"global-file","max_tool_rounds":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("MIAO_MODEL=from-home-env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIAO_MAX_TOOL_ROUNDS", "")
	os.Unsetenv("MIAO_MODEL")
	cfg, notes, err := LoadWithNotes()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectPath != "" || cfg.ProjectTrusted || cfg.MaxToolRounds != 3 || cfg.Model != "global-file" {
		t.Fatalf("不应把 ~/.miao 当项目目录：%+v", cfg)
	}
	if cfg.Model == "from-home-env" {
		t.Fatal("不应读取 ~/.env")
	}
	if !strings.Contains(strings.Join(notes, "\n"), "已忽略其中的 .env") {
		t.Fatalf("notes=%q", notes)
	}
}

// MIAO_HOME 指向某个工作区的 .miao 时，该工作区同样不当项目。
func TestMiaoHomeInsideWorkspace(t *testing.T) {
	clearConnEnv(t)
	ws := t.TempDir()
	t.Setenv("MIAO_HOME", filepath.Join(ws, ".miao"))
	t.Setenv("MIAO_WORKSPACE", ws)
	t.Setenv("MIAO_API_KEY", "k")
	if err := os.MkdirAll(filepath.Join(ws, ".miao"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".miao", "config.json"), []byte(`{"max_tool_rounds":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIAO_MAX_TOOL_ROUNDS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectPath != "" || cfg.ProjectTrusted || cfg.MaxToolRounds != 3 {
		t.Fatalf("MIAO_HOME 不能同时当项目目录：%+v", cfg)
	}
}
