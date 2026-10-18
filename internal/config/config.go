// Package config 负责读取运行配置。
//
// 优先级：真实环境变量 > 当前目录下的 .env 文件（不含连接地址）> 内置默认值。
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.deepseek.com"
	defaultModel   = "deepseek-chat"
)

// ErrMissingAPIKey 表示没有配置 API Key（doctor 仍可继续检查其它项）。
var ErrMissingAPIKey = errors.New("缺少 API Key：请执行 `cp .env.example .env`，然后填入 MIAO_API_KEY")

// Config 是程序启动所需的全部配置。
type Config struct {
	APIKey         string
	BaseURL        string
	Model          string
	FallbackModel  string
	Workspace      string
	Protocol       string
	Timeout        time.Duration
	MaxToolRounds  int
	PriceIn        float64
	PriceOut       float64
	PriceCache     float64
	Project        ProjectFile
	ProjectPath    string
	ProjectHash    string
	ProjectTrusted bool
}

// Load 从环境变量和 .env 文件加载配置。
func Load() (Config, error) {
	cfg, _, err := LoadWithNotes()
	return cfg, err
}

// LoadWithNotes 与 Load 相同，额外返回启动时值得告诉用户的提示（项目 JSON 错误、未信任的放宽项等）。
func LoadWithNotes() (Config, []string, error) {
	notes := loadDotEnv(".env")

	ws := firstEnv(AgentEnvNames("WORKSPACE")...)
	if ws == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Config{}, notes, fmt.Errorf("获取工作目录失败: %w", err)
		}
		ws = cwd
	}
	abs, err := filepath.Abs(ws)
	if err != nil {
		return Config{}, notes, fmt.Errorf("解析工作区失败: %w", err)
	}

	cfg := Config{
		APIKey: firstEnv(append(AgentEnvNames("API_KEY"), "DEEPSEEK_API_KEY")...),
		BaseURL: firstNonEmpty(
			firstEnv(append(AgentEnvNames("BASE_URL"), "DEEPSEEK_BASE_URL")...),
			defaultBaseURL,
		),
		Model: firstNonEmpty(
			firstEnv(append(AgentEnvNames("MODEL"), "DEEPSEEK_MODEL")...),
			defaultModel,
		),
		FallbackModel: strings.TrimSpace(Getenv("FALLBACK_MODEL")),
		Workspace:     abs,
		Protocol:      strings.TrimSpace(Getenv("PROTOCOL")),
		Timeout:       envDurationSec("TIMEOUT", 10*time.Minute),
		MaxToolRounds: envInt("MAX_TOOL_ROUNDS", 32),
		PriceIn:       envFloat("PRICE_IN"),
		PriceOut:      envFloat("PRICE_OUT"),
		PriceCache:    envFloat("PRICE_CACHE"),
	}

	pf, path, hash, err := loadProjectFile(abs)
	switch {
	case err != nil:
		notes = append(notes, "项目配置 "+path+" 解析失败，已忽略："+err.Error())
	default:
		cfg.Project, cfg.ProjectPath, cfg.ProjectHash = pf, path, hash
		cfg.ProjectTrusted = hash != "" && projectTrusted(path, hash)
		notes = append(notes, applyProject(&cfg, pf, cfg.ProjectTrusted)...)
	}

	if cfg.APIKey == "" {
		return cfg, notes, ErrMissingAPIKey
	}
	return cfg, notes, nil
}

func applyProject(cfg *Config, pf ProjectFile, trusted bool) []string {
	var notes []string
	if pf.Sandbox != nil && *pf.Sandbox && Getenv("SANDBOX") == "" {
		_ = os.Setenv("MIAO_SANDBOX", "1")
	}
	if pf.Sandbox != nil && !*pf.Sandbox {
		notes = append(notes, "项目配置想关闭沙箱，已忽略（项目配置只能收紧权限）。")
	}
	if pf.MaxToolRounds > 0 && Getenv("MAX_TOOL_ROUNDS") == "" {
		if pf.MaxToolRounds < cfg.MaxToolRounds {
			cfg.MaxToolRounds = pf.MaxToolRounds
		} else if pf.MaxToolRounds > cfg.MaxToolRounds && !trusted {
			notes = append(notes, fmt.Sprintf("项目配置把工具轮次上限提到 %d，未信任时已忽略。", pf.MaxToolRounds))
		} else if trusted {
			cfg.MaxToolRounds = pf.MaxToolRounds
		}
	}
	if pf.TimeoutSec > 0 && Getenv("TIMEOUT") == "" {
		d := time.Duration(pf.TimeoutSec) * time.Second
		if d < cfg.Timeout {
			cfg.Timeout = d
		} else if d > cfg.Timeout && !trusted {
			notes = append(notes, "项目配置想延长请求超时，未信任时已忽略。")
		} else if trusted {
			cfg.Timeout = d
		}
	}

	loosens := len(pf.AllowCommands) > 0 || pf.Model != "" || pf.Protocol != ""
	if loosens && !trusted {
		notes = append(notes, "项目 .miao/config.json 未信任，已忽略 allow_commands / model / protocol。确认仓库可信后输入 /trust-project。")
		return notes
	}
	if pf.Model != "" && firstEnv(append(AgentEnvNames("MODEL"), "DEEPSEEK_MODEL")...) == "" {
		cfg.Model = pf.Model
	}
	if pf.Protocol != "" && Getenv("PROTOCOL") == "" {
		cfg.Protocol = pf.Protocol
	}
	if len(pf.AllowCommands) > 0 && Getenv("ALLOW_COMMANDS") == "" {
		_ = os.Setenv("MIAO_ALLOW_COMMANDS", strings.Join(pf.AllowCommands, ","))
	}
	return notes
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// workspaceEnvBlocked 是工作区 .env 里不允许设置的连接项，防止不可信仓库把 API Key 发到别处。
var workspaceEnvBlocked = map[string]bool{
	"MIAO_BASE_URL": true, "AGENT_BASE_URL": true, "DEEPSEEK_BASE_URL": true,
	"MIAO_PROTOCOL": true, "AGNET_PROTOCOL": true,
	"AGENT_PROTOCOL": true,
}

func loadDotEnv(path string) []string {
	var notes []string
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" {
			continue
		}
		if workspaceEnvBlocked[key] {
			if _, exists := os.LookupEnv(key); !exists {
				notes = append(notes, "已忽略工作区 .env 中的 "+key+"（连接地址只能来自真实环境变量或 -base-url / -protocol）。")
			}
			continue
		}

		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
	return notes
}
