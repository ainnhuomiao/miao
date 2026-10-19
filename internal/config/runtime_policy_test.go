package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimePolicySourcesAndUntrustedProject(t *testing.T) {
	clearRuntimeEnv(t)
	t.Setenv("MIAO_API_KEY", "test-key")
	writeUserConfig(t, `{"mode":"read-only","parallel_tools":1}`)
	cfg, err := Load()
	if err != nil || cfg.Mode != "plan" || cfg.ParallelTools != 1 {
		t.Fatalf("user policy not applied: %s %d %v", cfg.Mode, cfg.ParallelTools, err)
	}
	workspace := t.TempDir()
	t.Setenv("MIAO_WORKSPACE", workspace)
	if err := os.Mkdir(filepath.Join(workspace, ".miao"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".miao", "config.json"), []byte(`{"mode":"edits","parallel_tools":100}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil || cfg.Mode != "plan" || cfg.ParallelTools != 1 {
		t.Fatal("workspace config must not change the user-only startup policy")
	}
	t.Setenv("MIAO_MODE", "edits")
	t.Setenv("MIAO_PARALLEL_TOOLS", "12")
	cfg, err = Load()
	if err != nil || cfg.Mode != "edits" || cfg.ParallelTools != 12 {
		t.Fatal("real environment must override user policy")
	}
}

func TestRuntimePolicyValidation(t *testing.T) {
	for _, body := range []string{`{"mode":"invalid"}`, `{"parallel_tools":0}`, `{"parallel_tools":-1}`} {
		t.Run(body, func(t *testing.T) {
			clearRuntimeEnv(t)
			writeUserConfig(t, body)
			if _, err := Load(); err == nil {
				t.Fatal("invalid user policy should fail explicitly")
			}
		})
	}
}
