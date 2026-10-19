package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ainnhuomiao/miao/internal/config"
)

func TestConfigCommandFeatureOverrides(t *testing.T) {
	for _, value := range []bool{false, true} {
		body := map[string]bool{}
		for _, name := range []string{"sessions", "project_memory", "custom_commands", "shell", "images", "diagnostics", "snapshots", "bell"} {
			body[name+"_enabled"] = !value
		}
		data, _ := json.Marshal(body)
		home := runtimeHome(t, string(data))
		args := []string{}
		for _, name := range []string{"sessions", "project-memory", "custom-commands", "shell", "images", "diagnostics", "snapshots", "bell"} {
			setting := "false"
			if value {
				setting = "true"
			}
			args = append(args, "-"+name+"="+setting)
		}
		out, stderr, err := runtimeCommand(t, home, append(args, "config")...)
		if err != nil {
			t.Fatalf("config failed: %v, %s", err, stderr)
		}
		got := map[string]json.RawMessage{}
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatal(err)
		}
		for name := range body {
			var enabled bool
			if err := json.Unmarshal(got[name], &enabled); err != nil || enabled != value {
				t.Fatalf("command line must override %s: %s, %v", name, got[name], err)
			}
		}
	}
}

func TestProjectMemoryCanBeDisabled(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("private project instructions"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Workspace: workspace, ProjectMemoryEnabled: true}
	if content, _, err := loadProjectMemory(cfg); err != nil || content != "private project instructions" {
		t.Fatalf("enabled project context should load: %q, %v", content, err)
	}
	cfg.ProjectMemoryEnabled = false
	if content, truncated, err := loadProjectMemory(cfg); err != nil || content != "" || truncated {
		t.Fatal("disabled project context must not be loaded")
	}
}
