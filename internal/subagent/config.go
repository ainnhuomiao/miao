// Package subagent 实现可配置的只读子代理（task 工具）。
package subagent

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// 并发上限的默认值与范围。
const (
	DefaultMax = 3
	MinMax     = 1
	MaxMax     = 8
)

// Config 是 config.json 里的 "subagents" 字段。
type Config struct {
	Enabled bool   `json:"enabled"`
	Max     int    `json:"max"`
	Model   string `json:"model"`
	// AllowWeb 为 true 时子代理额外获得 web_fetch（默认关闭，防止数据外传）。
	AllowWeb bool `json:"allow_web"`
}

// Defaults 返回默认配置：关闭、并发 3、跟随主模型。
func Defaults() Config { return Config{Max: DefaultMax} }

// Normalize 把 Max 限制在 1..8；0 视为默认值。
func (c Config) Normalize() Config {
	switch {
	case c.Max == 0:
		c.Max = DefaultMax
	case c.Max < MinMax:
		c.Max = MinMax
	case c.Max > MaxMax:
		c.Max = MaxMax
	}
	return c
}

// ConfigPath 返回 $XDG_CONFIG_HOME/agnet/config.json（与 /theme 同一个文件）。
func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agnet", "config.json"), nil
}

// LoadConfig 读取 subagents 字段；文件或字段缺失时返回默认值。
func LoadConfig() (Config, error) {
	cfg := Defaults()
	p, err := ConfigPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	var raw struct {
		Subagents *Config `json:"subagents"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, err
	}
	if raw.Subagents != nil {
		cfg = *raw.Subagents
	}
	return cfg.Normalize(), nil
}

// SaveConfig 只改 subagents 字段，保留配置文件里的其它键。
func SaveConfig(c Config) error {
	p, err := ConfigPath()
	if err != nil {
		return err
	}
	all := map[string]any{}
	if data, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(data, &all)
	}
	all["subagents"] = c.Normalize()
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
