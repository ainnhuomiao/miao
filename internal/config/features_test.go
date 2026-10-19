package config

import (
	"strings"
	"testing"
)

func featureStates(cfg Config) map[string]bool {
	return map[string]bool{
		"sessions": cfg.SessionsEnabled, "project_memory": cfg.ProjectMemoryEnabled,
		"custom_commands": cfg.CustomCommandsEnabled, "shell": cfg.ShellEnabled,
		"images": cfg.ImagesEnabled, "diagnostics": cfg.DiagnosticsEnabled,
		"snapshots": cfg.SnapshotsEnabled, "bell": cfg.BellEnabled,
	}
}

func TestFeatureSwitchDefaultsFileAndEnvironment(t *testing.T) {
	clearRuntimeEnv(t)
	t.Setenv("MIAO_API_KEY", "test-key")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for name, enabled := range featureStates(cfg) {
		if !enabled {
			t.Fatalf("%s should remain enabled by default", name)
		}
	}
	writeUserConfig(t, `{"sessions_enabled":false,"project_memory_enabled":false,"custom_commands_enabled":false,"shell_enabled":false,"images_enabled":false,"diagnostics_enabled":false,"snapshots_enabled":false,"bell_enabled":false}`)
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	for name, enabled := range featureStates(cfg) {
		if enabled {
			t.Fatalf("file must be able to disable %s", name)
		}
		t.Setenv("MIAO_"+strings.ToUpper(name)+"_ENABLED", "true")
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	for name, enabled := range featureStates(cfg) {
		if !enabled {
			t.Fatalf("real environment should override file for %s", name)
		}
	}
	t.Setenv("MIAO_SESSIONS_ENABLED", "invalid-secret-value")
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "MIAO_SESSIONS_ENABLED") || strings.Contains(err.Error(), "invalid-secret-value") {
		t.Fatalf("invalid environment should report only its name: %v", err)
	}
}

func TestDiagnosticsEnvironmentCompatibility(t *testing.T) {
	clearRuntimeEnv(t)
	t.Setenv("MIAO_API_KEY", "test-key")
	t.Setenv("MIAO_DIAGNOSE", "0")
	cfg, err := Load()
	if err != nil || cfg.DiagnosticsEnabled {
		t.Fatalf("legacy disable flag must remain effective: %v", err)
	}
	t.Setenv("MIAO_DIAGNOSTICS_ENABLED", "1")
	cfg, err = Load()
	if err != nil || !cfg.DiagnosticsEnabled {
		t.Fatalf("new flag should override the legacy flag: %v", err)
	}
}
