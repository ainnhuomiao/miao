package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestConfiguredModeControlsActualFileWrites(t *testing.T) {
	for _, tc := range []struct {
		mode, override string
		write          bool
	}{{"plan", "", false}, {"plan", "edits", true}, {"edits", "ask", false}, {"edits", "", true}} {
		t.Run(tc.mode+"/"+tc.override, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if requests.Add(1) == 1 {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"w1\",\"type\":\"function\",\"function\":{\"name\":\"write_file\",\"arguments\":\"{\\\"path\\\":\\\"result.txt\\\",\\\"content\\\":\\\"configured\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
				} else {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}
			}))
			defer srv.Close()
			home := runtimeHome(t, fmt.Sprintf(`{"base_url":%q,"mode":%q,"parallel_tools":1,"mcp_enabled":false,"hooks_enabled":false,"snapshots_enabled":false,"diagnostics_enabled":false}`, srv.URL, tc.mode))
			if err := os.WriteFile(filepath.Join(home, "credentials"), []byte("api_key=test-key\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			workspace := t.TempDir()
			args := []string{"-p", "write result"}
			if tc.override != "" {
				args = append(args, "-mode", tc.override)
			}
			_, stderr, err := runtimeCommandIn(t, home, workspace, args...)
			if err != nil {
				t.Fatalf("runtime failed: %v: %s", err, stderr)
			}
			data, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
			if tc.write && (err != nil || string(data) != "configured") || !tc.write && !os.IsNotExist(err) {
				t.Fatalf("configured mode write=%v, result=%q, err=%v", tc.write, data, err)
			}
		})
	}
}

func TestPolicyFlagsInEffectiveConfig(t *testing.T) {
	home := runtimeHome(t, `{"mode":"plan","parallel_tools":1}`)
	out, stderr, err := runtimeCommand(t, home, "-mode", "edits", "-parallel-tools", "12", "config")
	if err != nil {
		t.Fatalf("config failed: %v: %s", err, stderr)
	}
	var got struct {
		Mode          string `json:"mode"`
		ParallelTools int    `json:"parallel_tools"`
	}
	if err := json.Unmarshal(out, &got); err != nil || got.Mode != "edits" || got.ParallelTools != 12 {
		t.Fatalf("effective policy=%+v: %v", got, err)
	}
}
