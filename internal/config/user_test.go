package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeUserConfig(t *testing.T, body string) {
	t.Helper()
	p, err := UserConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func clearRuntimeEnv(t *testing.T) {
	t.Helper()
	clearConnEnv(t)
	for _, key := range []string{"PROTOCOL", "THINKING", "FALLBACK_MODEL", "TIMEOUT", "MAX_TOOL_ROUNDS", "MAX_RETRIES", "MAX_OUTPUT_TOKENS",
		"MODE", "PARALLEL_TOOLS",
		"SESSIONS_ENABLED", "PROJECT_MEMORY_ENABLED", "CUSTOM_COMMANDS_ENABLED", "SHELL_ENABLED", "IMAGES_ENABLED", "DIAGNOSTICS_ENABLED", "SNAPSHOTS_ENABLED", "BELL_ENABLED", "DIAGNOSE"} {
		for _, prefix := range []string{EnvPrefix, OldEnvPrefix} {
			name := prefix + key
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestUserRuntimeConfig(t *testing.T) {
	clearRuntimeEnv(t)
	t.Setenv("MIAO_API_KEY", "test-key")
	writeUserConfig(t, `{
		"base_url":"http://localhost:1234/v1", "model":"local-model", "protocol":"openai",
		"fallback_model":"backup", "thinking":"low", "system":"请简洁回答",
		"timeout_seconds":25, "max_retries":0, "max_tool_rounds":7, "max_output_tokens":2048,
		"auto_compact_percent":0, "compact_keep_turns":4,
		"allowed_tools":[], "disabled_tools":["web_fetch"], "mcp_enabled":false, "hooks_enabled":false, "plain":true,
		"theme":"mocha", "subagents":{"enabled":false}, "hooks":{}
	}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://localhost:1234/v1" || cfg.Model != "local-model" || cfg.Protocol != "openai" ||
		cfg.FallbackModel != "backup" || cfg.Thinking != "low" || cfg.System != "请简洁回答" {
		t.Fatal("用户连接配置或提示词未生效")
	}
	if cfg.Timeout != 25*time.Second || cfg.MaxRetries != 0 || cfg.MaxToolRounds != 7 || cfg.MaxOutputTokens != 2048 {
		t.Fatal("用户请求限制未生效")
	}
	if cfg.AutoCompactPercent != 0 || cfg.CompactKeepTurns != 4 {
		t.Fatal("压缩设置未生效")
	}
	if cfg.MCPEnabled || cfg.HooksEnabled || !cfg.Plain || cfg.AllowedTools == nil || len(cfg.AllowedTools) != 0 {
		t.Fatal("显式关闭功能和空工具列表未保留")
	}
	if !reflect.DeepEqual(cfg.DisabledTools, []string{"web_fetch"}) {
		t.Fatal("工具拒绝列表未保留")
	}
	if cfg.ProjectTrusted || os.Getenv("MIAO_MODEL") != "" || Getenv("THINKING") != "" {
		t.Fatal("用户配置不应信任项目或写入进程环境")
	}
}

func TestUserConfigPrecedence(t *testing.T) {
	clearRuntimeEnv(t)
	if err := SaveCredentials(Credentials{APIKey: "credentials-key", BaseURL: "https://credentials.example", Model: "credentials-model", Protocol: "anthropic"}); err != nil {
		t.Fatal(err)
	}
	writeUserConfig(t, `{"base_url":"https://user.example", "model":"user-model", "protocol":"openai", "thinking":"high", "timeout_seconds":35, "max_tool_rounds":9, "max_retries":0, "max_output_tokens":4096}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeySource != KeyFromCredentials || cfg.BaseURL != "https://user.example" || cfg.Model != "user-model" || cfg.Protocol != "openai" {
		t.Fatal("用户连接配置应覆盖 credentials 中的连接默认值")
	}
	ws := t.TempDir()
	t.Setenv("MIAO_WORKSPACE", ws)
	if err := os.Mkdir(filepath.Join(ws, ".miao"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".miao", "config.json"), []byte(`{"model":"project-model","max_tool_rounds":90,"timeout_seconds":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "user-model" || cfg.MaxToolRounds != 9 || cfg.Timeout != 35*time.Second {
		t.Fatal("未信任的项目不能覆盖模型或放宽用户限制")
	}
	if _, err := TrustProject(ws); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "project-model" || cfg.MaxToolRounds != 90 || cfg.Timeout != 99*time.Second {
		t.Fatal("信任后的项目设置应覆盖用户配置")
	}
	t.Setenv("MIAO_MODEL", "env-model")
	t.Setenv("MIAO_BASE_URL", "https://env.example")
	t.Setenv("MIAO_PROTOCOL", "auto")
	t.Setenv("MIAO_THINKING", "")
	t.Setenv("AGNET_THINKING", "medium")
	t.Setenv("MIAO_TIMEOUT", "12")
	t.Setenv("MIAO_MAX_TOOL_ROUNDS", "3")
	t.Setenv("MIAO_MAX_RETRIES", "2")
	t.Setenv("MIAO_MAX_OUTPUT_TOKENS", "1024")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "env-model" || cfg.BaseURL != "https://env.example" || cfg.Protocol != "auto" || cfg.Thinking != "" ||
		cfg.Timeout != 12*time.Second || cfg.MaxToolRounds != 3 || cfg.MaxRetries != 2 || cfg.MaxOutputTokens != 1024 {
		t.Fatal("真实环境变量应覆盖文件设置；显式空思考设置应抑制旧环境变量")
	}
}

func TestUserConfigValidation(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"max_retries":-1}`, "max_retries"},
		{`{"timeout_seconds":0}`, "timeout_seconds"},
		{`{"timeout_seconds":9223372036854775807}`, "timeout_seconds"},
		{`{"max_tool_rounds":0}`, "max_tool_rounds"},
		{`{"max_output_tokens":-1}`, "max_output_tokens"},
		{`{"auto_compact_percent":101}`, "auto_compact_percent"},
		{`{"auto_compact_percent":-1}`, "auto_compact_percent"},
		{`{"compact_keep_turns":0}`, "compact_keep_turns"},
		{`{"protocol":"unsupported"}`, "protocol"},
		{`{"allowed_tools":[" "]}`, "allowed_tools"},
		{`{"disabled_tools":[""]}`, "disabled_tools"},
		{`{"max_retries":"secret-value"}`, "解析用户配置"},
		{`{broken`, "解析用户配置"},
		{`null`, "JSON 对象"},
		{`[]`, "JSON 对象"},
	} {
		t.Run(tc.want+tc.body, func(t *testing.T) {
			clearRuntimeEnv(t)
			t.Setenv("MIAO_API_KEY", "test-key")
			writeUserConfig(t, tc.body)
			_, _, err := LoadWithNotes()
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestUserConfigDefaultsAndReload(t *testing.T) {
	clearRuntimeEnv(t)
	cfg, err := Load()
	if !errors.Is(err, ErrMissingAPIKey) || !cfg.MCPEnabled || !cfg.HooksEnabled || cfg.Plain || cfg.AllowedTools != nil || cfg.MaxRetries != 3 || cfg.AutoCompactPercent != 80 || cfg.CompactKeepTurns != 2 {
		t.Fatal("缺少用户配置时应保留默认值并返回缺 Key 错误")
	}
	writeUserConfig(t, `{"allowed_tools":["read_file"],"max_retries":0}`)
	cfg, err = Load()
	if !errors.Is(err, ErrMissingAPIKey) || !reflect.DeepEqual(cfg.AllowedTools, []string{"read_file"}) || cfg.MaxRetries != 0 {
		t.Fatal("无 Key 时也应能解析完整运行配置")
	}
	writeUserConfig(t, `{}`)
	cfg, _ = Load()
	if cfg.AllowedTools != nil || cfg.MaxRetries != 3 {
		t.Fatal("重新加载不应继承上一次的配置")
	}
}
