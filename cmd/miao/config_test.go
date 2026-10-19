package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ainnhuomiao/miao/internal/config"
)

// 子进程隔离全局 flag、终端判断和可选扩展的实际启动过程。
func TestRuntimeCommandProcess(t *testing.T) {
	if os.Getenv("MIAO_TEST_RUNTIME_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			os.Args = append([]string{args[0]}, args[i+1:]...)
			break
		}
	}
	flag.CommandLine = flag.NewFlagSet("miao", flag.ContinueOnError)
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runtimeCommand(t *testing.T, home string, args ...string) ([]byte, []byte, error) {
	t.Helper()
	return runtimeCommandIn(t, home, t.TempDir(), args...)
}

func runtimeCommandIn(t *testing.T, home, workspace string, args ...string) ([]byte, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, exe, append([]string{"-test.run=^TestRuntimeCommandProcess$", "--"}, args...)...)
	cmd.Dir = workspace
	// 排除开发者的真实凭据和旧 XDG 配置，测试只使用自己的临时文件。
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name == "HOME" || strings.HasPrefix(name, "MIAO_") || strings.HasPrefix(name, "AGENT_") ||
			strings.HasPrefix(name, "AGNET_") || strings.HasPrefix(name, "DEEPSEEK_") || strings.HasPrefix(name, "XDG_") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "MIAO_TEST_RUNTIME_PROCESS=1", "MIAO_HOME="+home, "HOME="+filepath.Dir(home))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func runtimeHome(t *testing.T, body string) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), ".miao")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestConfigCommandWithoutCredentialsAndWithOverrides(t *testing.T) {
	home := runtimeHome(t, `{"model":"file-model", "max_retries":3, "max_output_tokens":1024, "plain":true, "mcp_enabled":false, "hooks_enabled":false}`)
	out, stderr, err := runtimeCommand(t, home, "-model", "cli-model", "-retries=0", "-max-output-tokens=2048", "-plain=false", "-mcp=true", "-hooks=true", "-allowed-tools=", "-disabled-tools=web_fetch", "-auto-compact=0", "-compact-keep-turns=4", "config")
	if err != nil {
		t.Fatalf("config failed: %v: %s", err, stderr)
	}
	var got struct {
		config.UserFile
		APIKeyConfigured bool `json:"api_key_configured"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("invalid config JSON: %v: %s", err, out)
	}
	if got.APIKeyConfigured || got.Model != "cli-model" || got.MaxRetries == nil || *got.MaxRetries != 0 ||
		got.MaxOutputTokens == nil || *got.MaxOutputTokens != 2048 || got.Plain == nil || *got.Plain ||
		got.MCPEnabled == nil || !*got.MCPEnabled || got.HooksEnabled == nil || !*got.HooksEnabled ||
		got.AllowedTools == nil || len(got.AllowedTools) != 0 || len(got.DisabledTools) != 1 || got.DisabledTools[0] != "web_fetch" ||
		got.AutoCompactPercent == nil || *got.AutoCompactPercent != 0 || got.CompactKeepTurns == nil || *got.CompactKeepTurns != 4 {
		t.Fatal("CLI overrides or explicit empty values were lost")
	}
}

func TestPrintConfigRedactsCredentials(t *testing.T) {
	t.Setenv("MIAO_HOME", t.TempDir())
	var out bytes.Buffer
	err := printConfig(&out, config.Config{
		APIKey: "private-key", KeySource: config.KeyFromEnv,
		BaseURL: "https://user:password@example.com/v1?token=other-secret#fragment-secret",
		System:  "API key: private-key, token=abcdefghijklmnop", Model: "test-model",
	})
	if err != nil || !json.Valid(out.Bytes()) {
		t.Fatalf("err=%v, output=%s", err, out.String())
	}
	for _, secret := range []string{"private-key", "password", "other-secret", "fragment-secret", "abcdefghijklmnop"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("config leaked %q", secret)
		}
	}
}

func TestHeadlessRuntimeConfigDisablesExtensions(t *testing.T) {
	type chatRequest struct {
		Model     string            `json:"model"`
		MaxTokens int               `json:"max_tokens"`
		Tools     []json.RawMessage `json:"tools"`
		Messages  []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	requests := make(chan chatRequest, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests <- request
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"configured answer\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	home := runtimeHome(t, fmt.Sprintf(`{"base_url":%q,"model":"local-model","max_retries":0,"max_output_tokens":1234,"allowed_tools":[],"mcp_enabled":false,"hooks_enabled":false,"system":"runtime instructions","hooks":{"stop":[{"command":"exit 1"}]}}`, srv.URL))
	marker := filepath.Join(home, "mcp-started")
	mcpJSON, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"disabled-test": map[string]any{
		"command": "sh", "args": []string{"-c", `printf started > "$1"; exit 1`, "sh", marker},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), mcpJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "credentials"), []byte("api_key=test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := runtimeCommand(t, home, "-p", "hi")
	if err != nil || !strings.Contains(string(out), "configured answer") {
		t.Fatalf("headless run failed: %v, stdout=%s, stderr=%s", err, out, stderr)
	}
	if len(requests) != 1 {
		t.Fatalf("stop hook must not run or cause extra requests, got %d requests", len(requests))
	}
	request := <-requests
	if request.Model != "local-model" || request.MaxTokens != 1234 || len(request.Tools) != 0 {
		t.Fatal("runtime model, output limit or disabled tools were not applied")
	}
	found := false
	for _, m := range request.Messages {
		found = found || strings.Contains(m.Content, "runtime instructions")
	}
	if !found || strings.Contains(string(stderr), "MCP") || strings.Contains(string(stderr), "hook") {
		t.Fatalf("instructions or extension switches failed: stderr=%s", stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("disabled MCP command must not start: %v", err)
	}
}
