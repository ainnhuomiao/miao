package main

import (
	"encoding/json"
	"io"
	"net/url"
	"strings"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/session"
)

// printConfig 不创建客户端或启动工具，也不要求 API Key。
func printConfig(w io.Writer, cfg config.Config) error {
	path, err := config.UserConfigPath()
	if err != nil {
		return err
	}
	redact := func(s string) string {
		if cfg.APIKey != "" {
			s = strings.ReplaceAll(s, cfg.APIKey, "***")
		}
		return session.RedactSecrets(s)
	}
	baseURL := cfg.BaseURL
	if u, err := url.Parse(baseURL); err == nil {
		u.User = nil
		if u.RawQuery != "" {
			u.RawQuery = "redacted"
		}
		u.Fragment = ""
		baseURL = u.String()
	}
	seconds := int(cfg.Timeout.Seconds())
	redactList := func(names []string) []string {
		if names == nil {
			return nil
		}
		out := make([]string, len(names))
		for i, name := range names {
			out[i] = redact(name)
		}
		return out
	}
	view := struct {
		config.UserFile
		ConfigFile       string `json:"config_file"`
		Workspace        string `json:"workspace"`
		APIKeyConfigured bool   `json:"api_key_configured"`
		APIKeySource     string `json:"api_key_source"`
		ProjectPath      string `json:"project_path,omitempty"`
		ProjectTrusted   bool   `json:"project_trusted"`
	}{
		UserFile: config.UserFile{
			BaseURL: redact(baseURL), Model: redact(cfg.Model), Protocol: redact(cfg.Protocol),
			FallbackModel: redact(cfg.FallbackModel), Thinking: redact(cfg.Thinking), System: redact(cfg.System),
			Mode: cfg.Mode, ParallelTools: &cfg.ParallelTools,
			TimeoutSec: &seconds, MaxRetries: &cfg.MaxRetries, MaxToolRounds: &cfg.MaxToolRounds,
			MaxOutputTokens: &cfg.MaxOutputTokens, AllowedTools: redactList(cfg.AllowedTools), DisabledTools: redactList(cfg.DisabledTools),
			AutoCompactPercent: &cfg.AutoCompactPercent, CompactKeepTurns: &cfg.CompactKeepTurns,
			MCPEnabled: &cfg.MCPEnabled, HooksEnabled: &cfg.HooksEnabled, Plain: &cfg.Plain,
			SessionsEnabled: &cfg.SessionsEnabled, ProjectMemoryEnabled: &cfg.ProjectMemoryEnabled,
			CustomCommandsEnabled: &cfg.CustomCommandsEnabled, ShellEnabled: &cfg.ShellEnabled,
			ImagesEnabled: &cfg.ImagesEnabled, DiagnosticsEnabled: &cfg.DiagnosticsEnabled,
			SnapshotsEnabled: &cfg.SnapshotsEnabled, BellEnabled: &cfg.BellEnabled,
		},
		ConfigFile: redact(path), Workspace: redact(cfg.Workspace),
		APIKeyConfigured: cfg.APIKey != "", APIKeySource: cfg.KeySource,
		ProjectPath: redact(cfg.ProjectPath), ProjectTrusted: cfg.ProjectTrusted,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(view)
}
