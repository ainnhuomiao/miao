// Package subagent 实现可配置的只读子代理（task 工具）。
package subagent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/llm"
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
	// Thinking 为空时跟随主代理；auto 表示使用子代理模型的服务端默认。
	Thinking string `json:"thinking,omitempty"`
	// AllowWeb 为 true 时子代理额外获得 web_fetch（默认关闭，防止数据外传）。
	AllowWeb bool `json:"allow_web"`
	// 0 沿用默认的 5 分钟超时；工具轮次为 0 时不限次数。
	TimeoutSec    int `json:"timeout_seconds,omitempty"`
	MaxToolRounds int `json:"max_tool_rounds,omitempty"`
}

// Defaults 返回默认配置：关闭、并发 3、跟随主模型。
func Defaults() Config { return Config{Max: DefaultMax} }

// Normalize 规范化思考强度别名，把 Max 限制在 1..8；0 视为默认值。
func (c Config) Normalize() Config {
	c.Thinking = strings.ToLower(strings.TrimSpace(c.Thinking))
	if c.Thinking != "" {
		if level, err := llm.NormalizeThinking(c.Thinking); err == nil {
			c.Thinking = level
		}
	}
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

// ConfigPath 返回 ~/.miao/config.json（与 /theme 同一个文件）。
func ConfigPath() (string, error) {
	return config.UserConfigPath()
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
	cfg = cfg.Normalize()
	if err := cfg.validate(); err != nil {
		return Defaults(), err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.TimeoutSec < 0 || int64(c.TimeoutSec) > (1<<63-1)/1_000_000_000 {
		return fmt.Errorf("subagents.timeout_seconds 必须是可表示的非负秒数")
	}
	if c.MaxToolRounds < 0 {
		return fmt.Errorf("subagents.max_tool_rounds 必须是非负整数")
	}
	return validateThinking(c.Thinking)
}

func validateThinking(level string) error {
	if level == "" {
		return nil
	}
	_, err := llm.NormalizeThinking(level)
	return err
}

// SaveConfig 只改 subagents 字段，保留配置文件里的其它键。
func SaveConfig(c Config) error {
	if err := c.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(c.Normalize())
	if err != nil {
		return err
	}
	return config.UpdateUserConfig(func(all map[string]json.RawMessage) error {
		all["subagents"] = data
		return nil
	})
}
