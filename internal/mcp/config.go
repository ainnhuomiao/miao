// Package mcp 实现 Model Context Protocol 客户端（stdio 传输，JSON-RPC 2.0），
// 把 MCP 服务器提供的工具注册进 miao 的工具注册表。只用标准库。
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ainnhuomiao/miao/internal/appdir"
)

// ServerConfig 是 mcp.json 中一个服务器的配置（兼容 Claude Desktop / Cursor 格式）。
type ServerConfig struct {
	Name     string            `json:"-"`
	Command  string            `json:"command"`
	Args     []string          `json:"args"`
	Env      map[string]string `json:"env"`
	URL      string            `json:"url"`
	Disabled bool              `json:"disabled"`
}

type fileConfig struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// ConfigPath 返回 $XDG_CONFIG_HOME/miao/mcp.json（默认 ~/.config/miao/mcp.json）。
func ConfigPath() string {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "mcp.json")
}

// LoadConfig 读取配置；文件不存在时返回空列表。结果按名称排序（含 disabled 的服务器）。
func LoadConfig(path string) ([]ServerConfig, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseConfig(data)
}

// ParseConfig 解析 mcp.json 内容。
func ParseConfig(data []byte) ([]ServerConfig, error) {
	if strings.TrimSpace(string(data)) == "" {
		return nil, nil
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("解析 mcp.json 失败: %w", err)
	}
	out := make([]ServerConfig, 0, len(fc.MCPServers))
	for name, sc := range fc.MCPServers {
		sc.Name = name
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
