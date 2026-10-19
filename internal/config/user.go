package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ainnhuomiao/miao/internal/appdir"
)

// UserFile 是用户级 config.json 中的运行参数，与 theme/subagents/hooks 共存。
// 指针用于区分未配置和显式的 0 / false；API Key 仍只从 credentials 或环境读取。
type UserFile struct {
	BaseURL               string   `json:"base_url"`
	Model                 string   `json:"model"`
	Protocol              string   `json:"protocol"`
	FallbackModel         string   `json:"fallback_model"`
	Thinking              string   `json:"thinking"`
	System                string   `json:"system"`
	Mode                  string   `json:"mode"`
	ParallelTools         *int     `json:"parallel_tools"`
	TimeoutSec            *int     `json:"timeout_seconds"`
	MaxRetries            *int     `json:"max_retries"`
	MaxToolRounds         *int     `json:"max_tool_rounds"`
	MaxOutputTokens       *int     `json:"max_output_tokens"`
	AutoCompactPercent    *int     `json:"auto_compact_percent"`
	CompactKeepTurns      *int     `json:"compact_keep_turns"`
	AllowedTools          []string `json:"allowed_tools"`
	DisabledTools         []string `json:"disabled_tools"`
	MCPEnabled            *bool    `json:"mcp_enabled"`
	HooksEnabled          *bool    `json:"hooks_enabled"`
	SessionsEnabled       *bool    `json:"sessions_enabled"`
	ProjectMemoryEnabled  *bool    `json:"project_memory_enabled"`
	CustomCommandsEnabled *bool    `json:"custom_commands_enabled"`
	ShellEnabled          *bool    `json:"shell_enabled"`
	ImagesEnabled         *bool    `json:"images_enabled"`
	DiagnosticsEnabled    *bool    `json:"diagnostics_enabled"`
	SnapshotsEnabled      *bool    `json:"snapshots_enabled"`
	BellEnabled           *bool    `json:"bell_enabled"`
	Plain                 *bool    `json:"plain"`
}

func defaultTrue(value *bool) bool { return value == nil || *value }

// UserConfigPath 返回用户级配置路径，绝不从工作区寻找替代文件。
func UserConfigPath() (string, error) {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func loadUserFile() (UserFile, error) {
	var uf UserFile
	path, err := UserConfigPath()
	if err != nil {
		return uf, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return uf, nil
	}
	if err != nil {
		return uf, fmt.Errorf("读取用户配置 %s 失败: %w", path, err)
	}
	if trimmed := strings.TrimSpace(string(data)); len(trimmed) == 0 || trimmed[0] != '{' {
		return UserFile{}, errors.New("用户配置必须是 JSON 对象")
	}
	if err := json.Unmarshal(data, &uf); err != nil {
		// UnmarshalTypeError 仅包含字段和类型，语法错误仅包含位置，不回显配置值。
		return UserFile{}, fmt.Errorf("解析用户配置 %s 失败: %w", path, err)
	}
	if err := uf.validate(); err != nil {
		return UserFile{}, fmt.Errorf("用户配置 %s: %w", path, err)
	}
	return uf, nil
}

func (u UserFile) validate() error {
	for _, field := range []struct {
		name string
		n    *int
		min  int
	}{
		{"timeout_seconds", u.TimeoutSec, 1},
		{"max_retries", u.MaxRetries, 0},
		{"max_tool_rounds", u.MaxToolRounds, 1},
		{"max_output_tokens", u.MaxOutputTokens, 0},
		{"auto_compact_percent", u.AutoCompactPercent, 0},
		{"compact_keep_turns", u.CompactKeepTurns, 1},
		{"parallel_tools", u.ParallelTools, 1},
	} {
		if field.n != nil && *field.n < field.min {
			return fmt.Errorf("%s 必须大于等于 %d", field.name, field.min)
		}
	}
	if u.AutoCompactPercent != nil && *u.AutoCompactPercent > 100 {
		return errors.New("auto_compact_percent 必须在 0..100 之间，0 表示关闭自动压缩")
	}
	if _, err := NormalizeMode(u.Mode); err != nil {
		return err
	}
	if u.TimeoutSec != nil && int64(*u.TimeoutSec) > (1<<63-1)/1_000_000_000 {
		return errors.New("timeout_seconds 超出可用范围")
	}
	switch strings.ToLower(strings.TrimSpace(u.Protocol)) {
	case "", "auto", "openai", "anthropic":
	default:
		return errors.New("protocol 必须是 auto / openai / anthropic")
	}
	for _, field := range []struct {
		name string
		list []string
	}{{"allowed_tools", u.AllowedTools}, {"disabled_tools", u.DisabledTools}} {
		for _, name := range field.list {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("%s 中的工具名不能为空", field.name)
			}
		}
	}
	return nil
}

// NormalizeMode 返回权限模式的规范名称，供配置和各入口共用。
func NormalizeMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "ask", "default":
		return "ask", nil
	case "edits", "accept-edits", "acceptedits":
		return "edits", nil
	case "plan", "readonly", "read-only":
		return "plan", nil
	default:
		return "", errors.New("mode 必须是 ask / edits / plan")
	}
}
