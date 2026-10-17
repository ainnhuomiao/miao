package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/tools"
)

const fakeEnv = "MCP_FAKE_SERVER"

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) == "1" {
		fakeServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeServer 是一个最小 stdio MCP 服务器，供测试重新执行测试二进制使用。
func fakeServer() {
	out := json.NewEncoder(os.Stdout)
	var cancelled atomic.Int64
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	reply := func(id json.RawMessage, result any) {
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	text := func(s string, isErr bool) map[string]any {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}, "isError": isErr}
	}
	for sc.Scan() {
		var msg rpcMessage
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		fmt.Fprintln(os.Stderr, "recv", msg.Method)
		switch msg.Method {
		case "initialize":
			reply(msg.ID, map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fake"}})
		case "notifications/cancelled":
			cancelled.Add(1)
		case "tools/list":
			var p struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if p.Cursor == "" {
				reply(msg.ID, map[string]any{"tools": []any{
					map[string]any{"name": "echo", "description": "回显", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}}},
					map[string]any{"name": "fail"},
				}, "nextCursor": "p2"})
			} else {
				reply(msg.ID, map[string]any{"tools": []any{
					map[string]any{"name": "sleep"},
					map[string]any{"name": "env"},
					map[string]any{"name": "cancelled"},
					map[string]any{"name": "bad name!"},
				}})
			}
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			switch p.Name {
			case "echo":
				var a struct {
					Text string `json:"text"`
				}
				_ = json.Unmarshal(p.Arguments, &a)
				reply(msg.ID, map[string]any{"content": []any{
					map[string]any{"type": "text", "text": "echo:" + a.Text},
					map[string]any{"type": "image", "mimeType": "image/png", "data": "AAAA"},
				}})
			case "fail":
				reply(msg.ID, text("boom", true))
			case "sleep":
				go func(id json.RawMessage) { time.Sleep(5 * time.Second); reply(id, text("late", false)) }(msg.ID)
			case "env":
				reply(msg.ID, text(fmt.Sprintf("secret=%q explicit=%q path=%t", os.Getenv("MY_API_KEY")+os.Getenv("OPENAI_BASE_URL"), os.Getenv("FAKE_EXPLICIT"), os.Getenv("PATH") != ""), false))
			case "cancelled":
				reply(msg.ID, text(fmt.Sprint(cancelled.Load()), false))
			default:
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32602, "message": "unknown tool"}})
			}
		default:
			if len(msg.ID) > 0 {
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32601, "message": "nope"}})
			}
		}
	}
}

func fakeConfig(name string) ServerConfig {
	return ServerConfig{Name: name, Command: os.Args[0], Env: map[string]string{fakeEnv: "1", "FAKE_EXPLICIT": "yes"}}
}

func newFake(t *testing.T) *Client {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	c, err := Spawn(fakeConfig("fake"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestListToolsPagination(t *testing.T) {
	c := newFake(t)
	list, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range list {
		names = append(names, tl.Name)
	}
	if got := strings.Join(names, ","); got != "echo,fail,sleep,env,cancelled,bad name!" {
		t.Fatalf("tools = %s", got)
	}
}

func TestCallToolResults(t *testing.T) {
	c := newFake(t)
	raw, err := c.CallTool(context.Background(), "echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	r := FormatResult(raw)
	if r.IsError || r.Content != "echo:hi\n[image: image/png]" {
		t.Fatalf("echo = %+v", r)
	}
	raw, err = c.CallTool(context.Background(), "fail", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := FormatResult(raw); !r.IsError || r.Content != "boom" {
		t.Fatalf("fail = %+v", r)
	}
	if _, err := c.CallTool(context.Background(), "nope", nil); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("unknown tool err = %v", err)
	}
}

func TestCancelSendsNotification(t *testing.T) {
	c := newFake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.CallTool(ctx, "sleep", nil); err != context.DeadlineExceeded {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancel not prompt")
	}
	// 连接仍可用，且服务器收到了 notifications/cancelled。
	raw, err := c.CallTool(context.Background(), "cancelled", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := FormatResult(raw); r.Content != "1" {
		t.Fatalf("cancelled count = %q", r.Content)
	}
}

func TestChildEnvFiltered(t *testing.T) {
	t.Setenv("MY_API_KEY", "sk-should-not-leak")
	t.Setenv("OPENAI_BASE_URL", "http://leak")
	c := newFake(t)
	raw, err := c.CallTool(context.Background(), "env", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := FormatResult(raw); r.Content != `secret="" explicit="yes" path=true` {
		t.Fatalf("env = %s", r.Content)
	}
}

func TestBuildEnv(t *testing.T) {
	env := buildEnv([]string{"PATH=/bin", "GITHUB_TOKEN=x", "MIAO_MODEL=m", "LC_ALL=C", "XDG_DATA_HOME=/d", "RANDOM=1", "HOME=/h"},
		map[string]string{"GITHUB_TOKEN": "explicit", "HOME": "/o"})
	got := strings.Join(env, " ")
	for _, want := range []string{"PATH=/bin", "LC_ALL=C", "XDG_DATA_HOME=/d", "GITHUB_TOKEN=explicit", "HOME=/o"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	for _, bad := range []string{"GITHUB_TOKEN=x", "MIAO_MODEL", "RANDOM", "HOME=/h"} {
		if strings.Contains(got, bad) {
			t.Errorf("leaked %s in %s", bad, got)
		}
	}
}

func TestToolNameSanitize(t *testing.T) {
	taken := map[string]bool{}
	if got := toolName("my server", "bad name!", taken); got != "my_server__bad_name_" {
		t.Fatal(got)
	}
	if got := toolName("my server", "bad name?", taken); got != "my_server__bad_name__2" {
		t.Fatal(got)
	}
	if got := toolName("服务", "x", taken); got != "__x" && !strings.HasSuffix(got, "__x") {
		t.Fatal(got)
	}
	long := toolName(strings.Repeat("s", 40), strings.Repeat("t", 80), taken)
	if len(long) != 64 {
		t.Fatalf("len = %d", len(long))
	}
	long2 := toolName(strings.Repeat("s", 40), strings.Repeat("t", 80), taken)
	if len(long2) != 64 || long2 == long || !strings.HasSuffix(long2, "_2") {
		t.Fatal(long2)
	}
}

func TestParseConfig(t *testing.T) {
	cfgs, err := LoadConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || len(cfgs) != 0 {
		t.Fatalf("missing file: %v %v", cfgs, err)
	}
	cfgs, err = ParseConfig([]byte(`{"mcpServers":{"b":{"command":"x","args":["1"],"env":{"K":"v"}},"a":{"command":"y","disabled":true},"h":{"url":"http://x"}}}`))
	if err != nil || len(cfgs) != 3 || cfgs[0].Name != "a" || !cfgs[0].Disabled || cfgs[1].Args[0] != "1" || cfgs[1].Env["K"] != "v" {
		t.Fatalf("%+v %v", cfgs, err)
	}
	if _, err := ParseConfig([]byte(`{bad`)); err == nil {
		t.Fatal("want error")
	}
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if ConfigPath() != "/cfg/miao/mcp.json" {
		t.Fatal(ConfigPath())
	}
}

func TestFormatResultTruncate(t *testing.T) {
	big := strings.Repeat("中", maxResultBytes)
	raw, _ := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": big}}})
	r := FormatResult(raw)
	if len(r.Content) > maxResultBytes+200 || !strings.Contains(r.Content, "已截断") {
		t.Fatalf("len=%d", len(r.Content))
	}
}

func TestManagerRegistry(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	cfg := map[string]any{"mcpServers": map[string]any{
		"fake":   map[string]any{"command": os.Args[0], "env": map[string]string{fakeEnv: "1"}},
		"off":    map[string]any{"command": "nonexistent", "disabled": true},
		"broken": map[string]any{"command": "/nonexistent/miao-mcp"},
		"web":    map[string]any{"url": "http://localhost:1"},
	}}
	data, _ := json.Marshal(cfg)
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	var approved []string
	reg.SetApprover(func(ctx context.Context, name, display string, args json.RawMessage) (bool, error) {
		approved = append(approved, name+"|"+display)
		return name != "fake__fail", nil
	})
	m := StartWithConfig(reg, path)
	defer m.Close()
	m.Wait()

	defs := map[string]string{}
	for _, d := range reg.Definitions() {
		defs[d.Function.Name] = string(d.Function.Parameters)
	}
	if defs["fake__sleep"] != `{"type":"object"}` || !strings.Contains(defs["fake__echo"], "properties") {
		t.Fatalf("defs = %v", defs)
	}
	if _, ok := defs["fake__bad_name_"]; !ok {
		t.Fatalf("sanitized tool missing: %v", defs)
	}
	r := reg.Call(context.Background(), "fake__echo", `{"text":"yo"}`)
	if r.IsError || !strings.HasPrefix(r.Content, "echo:yo") {
		t.Fatalf("call = %+v", r)
	}
	if len(approved) != 1 || !strings.Contains(approved[0], "MCP fake · echo") || !strings.Contains(approved[0], `"text": "yo"`) {
		t.Fatalf("approval = %v", approved)
	}
	if r := reg.Call(context.Background(), "fake__fail", `{}`); !r.IsError || !strings.Contains(r.Content, "拒绝") {
		t.Fatalf("denied = %+v", r)
	}

	states := map[string]string{}
	for _, s := range m.Statuses() {
		states[s.Name] = s.State
	}
	if states["fake"] != StateConnected || states["off"] != StateDisabled || states["broken"] != StateFailed || states["web"] != StateFailed {
		t.Fatalf("states = %v", states)
	}
	var notes []string
	for len(m.Notices()) > 0 {
		notes = append(notes, <-m.Notices())
	}
	all := strings.Join(notes, "\n")
	if !strings.Contains(all, "已连接 MCP：fake（6 个工具）") || !strings.Contains(all, "MCP 服务器 broken 启动失败") {
		t.Fatalf("notices = %s", all)
	}
	if !strings.Contains(m.Summary(), "fake  已连接（6 个工具）") {
		t.Fatal(m.Summary())
	}
}

func TestCallTimeout(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	old := CallTimeout
	CallTimeout = 200 * time.Millisecond
	defer func() { CallTimeout = old }()
	dir := t.TempDir()
	data, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fake": map[string]any{"command": os.Args[0], "env": map[string]string{fakeEnv: "1"}}}})
	path := filepath.Join(dir, "mcp.json")
	_ = os.WriteFile(path, data, 0o600)
	reg, _ := tools.NewRegistry(dir)
	reg.SetApprover(func(context.Context, string, string, json.RawMessage) (bool, error) { return true, nil })
	m := StartWithConfig(reg, path)
	defer m.Close()
	m.Wait()
	if r := reg.Call(context.Background(), "fake__sleep", `{}`); !r.IsError || !strings.Contains(r.Content, "超时") {
		t.Fatalf("timeout = %+v", r)
	}
}

func TestNoApproverRefuses(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	data, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fake": map[string]any{"command": os.Args[0], "env": map[string]string{fakeEnv: "1"}}}})
	path := filepath.Join(dir, "mcp.json")
	_ = os.WriteFile(path, data, 0o600)
	reg, _ := tools.NewRegistry(dir)
	m := StartWithConfig(reg, path)
	defer m.Close()
	m.Wait()
	if r := reg.Call(context.Background(), "fake__echo", `{}`); !r.IsError {
		t.Fatalf("want refusal: %+v", r)
	}
}

func TestPlanModeRefusesMCP(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	data, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fake": map[string]any{"command": os.Args[0], "env": map[string]string{fakeEnv: "1"}}}})
	path := filepath.Join(dir, "mcp.json")
	_ = os.WriteFile(path, data, 0o600)
	reg, _ := tools.NewRegistry(dir)
	ag := agent.New(nil, "", reg)
	asked := false
	ag.SetApprover(func(context.Context, string, string, json.RawMessage) (bool, error) { asked = true; return true, nil })
	ag.SetMode(agent.ModePlan)
	m := StartWithConfig(reg, path)
	defer m.Close()
	m.Wait()
	if r := reg.Call(context.Background(), "fake__echo", `{}`); !r.IsError || asked {
		t.Fatalf("plan mode should refuse without asking: %+v asked=%v", r, asked)
	}
	ag.SetMode(agent.ModeAcceptEdits)
	if r := reg.Call(context.Background(), "fake__echo", `{}`); r.IsError || !asked {
		t.Fatalf("accept-edits should still ask: %+v asked=%v", r, asked)
	}
}
