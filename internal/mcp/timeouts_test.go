package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ainnhuomiao/miao/internal/tools"
)

func TestPerServerTimeoutsOverrideDefaults(t *testing.T) {
	t.Setenv("MIAO_HOME", t.TempDir())
	previousConnect, previousCall := ConnectTimeout, CallTimeout
	ConnectTimeout, CallTimeout = 50*time.Millisecond, 50*time.Millisecond
	defer func() { ConnectTimeout, CallTimeout = previousConnect, previousCall }()
	workspace := t.TempDir()
	data, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"slow": map[string]any{
		"command": os.Args[0], "env": map[string]string{fakeEnv: "1", "MCP_TEST_SLOW_INIT": "1"},
		"connect_timeout_seconds": 1, "call_timeout_seconds": 1,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "mcp.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(workspace)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetApprover(func(context.Context, string, string, json.RawMessage) (bool, error) { return true, nil })
	m := StartWithConfig(reg, path)
	defer m.Close()
	m.Wait()
	if res := reg.Call(context.Background(), "slow__echo", `{"text":"configured","delay":true}`); res.IsError || !strings.Contains(res.Content, "echo:configured") {
		t.Fatalf("server-specific connect and call timeouts should override short defaults: %+v", res)
	}
}

func TestInvalidServerTimeoutsRejected(t *testing.T) {
	for _, body := range []string{
		`{"mcpServers":{"x":{"connect_timeout_seconds":-1}}}`,
		`{"mcpServers":{"x":{"call_timeout_seconds":-1}}}`,
		`{"mcpServers":{"x":{"call_timeout_seconds":9223372036854775807}}}`,
	} {
		if _, err := ParseConfig([]byte(body)); err == nil {
			t.Fatal("invalid MCP timeout should fail parsing")
		}
	}
}
