package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigManagementWithoutCredentials(t *testing.T) {
	home := runtimeHome(t, `{"max_retries":-1,"theme":"mocha","subagents":{"enabled":false,"timeout_seconds":45}}`)
	for _, args := range [][]string{
		{"config", "set", "max_retries", "0"},
		{"config", "set", "model", "local-model"},
		{"config", "set", "allowed_tools", "[]"},
		{"config", "set", "sessions_enabled", "false"},
		{"config", "unset", "model"},
		{"config", "check"},
	} {
		if _, stderr, err := runtimeCommand(t, home, args...); err != nil {
			t.Fatalf("%v failed without credentials: %v, %s", args, err, stderr)
		}
	}
	data, err := os.ReadFile(filepath.Join(home, "config.json"))
	var got map[string]json.RawMessage
	if err != nil || json.Unmarshal(data, &got) != nil {
		t.Fatal("config manager did not save valid JSON")
	}
	if string(got["max_retries"]) != "0" || string(got["sessions_enabled"]) != "false" || string(got["allowed_tools"]) != "[]" || got["model"] != nil || got["theme"] == nil || got["subagents"] == nil {
		t.Fatalf("overrides, defaults or other sections were lost: %s", data)
	}
	out, stderr, err := runtimeCommand(t, home, "config", "path")
	if err != nil || strings.TrimSpace(string(out)) != filepath.Join(home, "config.json") {
		t.Fatalf("path failed: %q, %s, %v", out, stderr, err)
	}
	out, stderr, err = runtimeCommand(t, home, "config", "keys")
	if err != nil || !strings.Contains(string(out), "sessions_enabled\tbool") || !strings.Contains(string(out), "model\tstring") {
		t.Fatalf("keys failed: %q, %s, %v", out, stderr, err)
	}
}

func TestConfigManagementFailuresDoNotChangeFileOrPrintValues(t *testing.T) {
	const original = `{"theme":"mocha","max_retries":3}`
	home := runtimeHome(t, original)
	for _, args := range [][]string{
		{"config", "set", "api_key", "secret-command-value"},
		{"config", "set", "max_retries", "-1"},
		{"config", "set", "thinking", "secret-command-value"},
		{"config", "set", "sessions_enabled", `"secret-command-value"`},
		{"config", "set", "model"},
		{"config", "unknown"},
	} {
		out, stderr, err := runtimeCommand(t, home, args...)
		if err == nil || strings.Contains(string(out)+string(stderr), "secret-command-value") {
			t.Fatalf("invalid command should fail without printing values: %v, %s, %s", args[:2], out, stderr)
		}
		if data, _ := os.ReadFile(filepath.Join(home, "config.json")); string(data) != original {
			t.Fatal("invalid command overwrote config")
		}
	}
}

func TestConfigCheckNeverStartsExtensions(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	// 命令字符串不会交给 shell；若校验意外启动扩展，marker 就会出现。
	command := "touch " + marker
	data, err := json.Marshal(map[string]any{"hooks": map[string]any{"stop": []any{map[string]string{"command": command}}}})
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(t, string(data))
	mcpConfig, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"test": map[string]any{"command": "touch", "args": []string{marker}}}})
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), mcpConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := runtimeCommand(t, home, "config", "check")
	if err != nil || !strings.Contains(string(out), "配置校验通过") {
		t.Fatalf("check failed: %v, %s", err, stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("checking configuration started an extension")
	}
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), []byte(`{"mcpServers":{"x":{"call_timeout_seconds":-1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtimeCommand(t, home, "config", "check"); err == nil {
		t.Fatal("check must reject invalid MCP settings")
	}
}
