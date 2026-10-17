package config

import "testing"

func TestGetenvFallback(t *testing.T) {
	t.Setenv("AGNET_X_TEST", "old")
	t.Setenv("MIAO_X_TEST", "new")
	if got := Getenv("X_TEST"); got != "new" {
		t.Fatalf("新名应优先：%q", got)
	}
	t.Setenv("MIAO_X_TEST", "")
	if got := Getenv("X_TEST"); got != "" {
		t.Fatalf("新名已设置（即使为空）也应优先：%q", got)
	}
	if got := Getenv("Z_UNSET_TEST"); got != "" {
		t.Fatalf("都没设置应为空：%q", got)
	}
}

func TestGetenvOldOnly(t *testing.T) {
	t.Setenv("AGNET_ONLY_TEST", "old")
	if got := Getenv("ONLY_TEST"); got != "old" {
		t.Fatalf("新名未设置时应回退旧名：%q", got)
	}
}

// clearConnEnv 清掉连接配置相关变量，并切到没有 .env 的临时目录。
func clearConnEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, k := range []string{"API_KEY", "BASE_URL", "MODEL", "WORKSPACE"} {
		t.Setenv("MIAO_"+k, "")
		t.Setenv("AGENT_"+k, "")
	}
	for _, k := range []string{"DEEPSEEK_API_KEY", "DEEPSEEK_BASE_URL", "DEEPSEEK_MODEL"} {
		t.Setenv(k, "")
	}
}

func TestLoadMiaoEnvWins(t *testing.T) {
	clearConnEnv(t)
	ws := t.TempDir()
	t.Setenv("MIAO_API_KEY", "new-key")
	t.Setenv("AGENT_API_KEY", "old-key")
	t.Setenv("MIAO_BASE_URL", "http://new")
	t.Setenv("AGENT_BASE_URL", "http://old")
	t.Setenv("MIAO_MODEL", "new-model")
	t.Setenv("AGENT_MODEL", "old-model")
	t.Setenv("MIAO_WORKSPACE", ws)
	t.Setenv("AGENT_WORKSPACE", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "new-key" || cfg.BaseURL != "http://new" || cfg.Model != "new-model" || cfg.Workspace != ws {
		t.Fatalf("MIAO_* 应优先：%+v", cfg)
	}
}

func TestLoadAgentEnvFallback(t *testing.T) {
	clearConnEnv(t)
	ws := t.TempDir()
	t.Setenv("AGENT_API_KEY", "old-key")
	t.Setenv("AGENT_BASE_URL", "http://old")
	t.Setenv("AGENT_MODEL", "old-model")
	t.Setenv("AGENT_WORKSPACE", ws)
	t.Setenv("DEEPSEEK_API_KEY", "ds-key")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "old-key" || cfg.BaseURL != "http://old" || cfg.Model != "old-model" || cfg.Workspace != ws {
		t.Fatalf("MIAO_* 未设置时应回退 AGENT_*：%+v", cfg)
	}
}

func TestLoadDeepseekFallback(t *testing.T) {
	clearConnEnv(t)
	t.Setenv("DEEPSEEK_API_KEY", "ds-key")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "ds-key" || cfg.BaseURL != defaultBaseURL || cfg.Model != defaultModel {
		t.Fatalf("应回退 DEEPSEEK_API_KEY 和默认值：%+v", cfg)
	}
}
