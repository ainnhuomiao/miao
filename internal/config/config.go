// Package config 负责读取运行配置。
//
// API Key 的查找顺序：真实环境变量 MIAO_API_KEY > ~/.miao/credentials > 当前目录 .env（旧用法）。
// 其它运行参数来自真实环境变量 > 项目配置（需遵守信任限制）> 用户配置 > 内置默认值。
// 工作区 .env 是白名单：只接受 MIAO_API_KEY，且只解析进 Config，绝不写入进程环境；
// 其它键（MIAO_HOME、MIAO_WORKSPACE、LD_PRELOAD 等）一律忽略并提示键名。
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ainnhuomiao/miao/internal/appdir"
)

const (
	defaultBaseURL = "https://api.deepseek.com"
	defaultModel   = "deepseek-chat"
)

// ErrMissingAPIKey 表示没有配置 API Key（doctor 仍可继续检查其它项）。
var ErrMissingAPIKey = errors.New("缺少 API Key：请在终端运行 `miao init` 配置，或设置环境变量 MIAO_API_KEY")

// Key 来源，见 Config.KeySource。
const (
	KeyFromEnv         = "env"
	KeyFromCredentials = "credentials"
	KeyFromDotEnv      = ".env"
)

// Config 是程序启动所需的全部配置。
type Config struct {
	APIKey                string
	KeySource             string // env / credentials / .env，没有 Key 时为空
	BaseURL               string
	Model                 string
	FallbackModel         string
	Workspace             string
	Protocol              string
	Timeout               time.Duration
	MaxToolRounds         int
	MaxRetries            int
	MaxOutputTokens       int
	AutoCompactPercent    int
	CompactKeepTurns      int
	Thinking              string
	System                string
	Mode                  string
	ParallelTools         int
	AllowedTools          []string // nil 表示不限制，空数组表示禁用全部工具
	DisabledTools         []string // 拒绝列表优先于允许列表
	MCPEnabled            bool
	HooksEnabled          bool
	SessionsEnabled       bool
	ProjectMemoryEnabled  bool
	CustomCommandsEnabled bool
	ShellEnabled          bool
	ImagesEnabled         bool
	DiagnosticsEnabled    bool
	SnapshotsEnabled      bool
	BellEnabled           bool
	Plain                 bool
	PriceIn               float64
	PriceOut              float64
	PriceCache            float64
	Project               ProjectFile
	ProjectPath           string
	ProjectHash           string
	ProjectTrusted        bool
}

// Load 从用户配置、真实环境变量、credentials 和受限制的项目配置加载配置。
func Load() (Config, error) {
	cfg, _, err := LoadWithNotes()
	return cfg, err
}

// LoadWithNotes 与 Load 相同，额外返回启动时值得告诉用户的提示（项目 JSON 错误、未信任的放宽项等）。
func LoadWithNotes() (Config, []string, error) {
	user, err := loadUserFile()
	if err != nil {
		return Config{}, nil, err
	}
	// 先记下真实环境变量，再读工作区 .env，才能分清 Key 的来源
	apiKeyNames := append(AgentEnvNames("API_KEY"), "DEEPSEEK_API_KEY")
	baseNames := append(AgentEnvNames("BASE_URL"), "DEEPSEEK_BASE_URL")
	modelNames := append(AgentEnvNames("MODEL"), "DEEPSEEK_MODEL")
	envKey := firstEnv(apiKeyNames...)
	envModel := firstEnv(modelNames...)
	dotKey, notes := loadWorkspaceDotEnv()
	creds, credErr := LoadCredentials()
	if credErr != nil {
		notes = append(notes, "读取 credentials 失败，已忽略："+credErr.Error())
		creds = Credentials{}
	}

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
		BaseURL:               firstNonEmpty(firstEnv(baseNames...), user.BaseURL, defaultBaseURL),
		Model:                 firstNonEmpty(envModel, user.Model, defaultModel),
		FallbackModel:         settingString("FALLBACK_MODEL", user.FallbackModel),
		Workspace:             abs,
		Protocol:              settingString("PROTOCOL", user.Protocol),
		Timeout:               envDurationSec("TIMEOUT", time.Duration(settingInt(user.TimeoutSec, 600))*time.Second),
		MaxToolRounds:         envInt("MAX_TOOL_ROUNDS", settingInt(user.MaxToolRounds, 32)),
		MaxRetries:            settingInt(user.MaxRetries, 3),
		MaxOutputTokens:       settingInt(user.MaxOutputTokens, 0),
		AutoCompactPercent:    settingInt(user.AutoCompactPercent, 80),
		CompactKeepTurns:      settingInt(user.CompactKeepTurns, 2),
		Thinking:              settingString("THINKING", firstNonEmpty(user.Thinking, "auto")),
		System:                user.System,
		Mode:                  settingString("MODE", user.Mode),
		ParallelTools:         settingInt(user.ParallelTools, 8),
		AllowedTools:          user.AllowedTools,
		DisabledTools:         user.DisabledTools,
		MCPEnabled:            defaultTrue(user.MCPEnabled),
		HooksEnabled:          defaultTrue(user.HooksEnabled),
		SessionsEnabled:       defaultTrue(user.SessionsEnabled),
		ProjectMemoryEnabled:  defaultTrue(user.ProjectMemoryEnabled),
		CustomCommandsEnabled: defaultTrue(user.CustomCommandsEnabled),
		ShellEnabled:          defaultTrue(user.ShellEnabled),
		ImagesEnabled:         defaultTrue(user.ImagesEnabled),
		DiagnosticsEnabled:    defaultTrue(user.DiagnosticsEnabled),
		SnapshotsEnabled:      defaultTrue(user.SnapshotsEnabled),
		BellEnabled:           defaultTrue(user.BellEnabled),
		Plain:                 user.Plain != nil && *user.Plain,
		PriceIn:               envFloat("PRICE_IN"),
		PriceOut:              envFloat("PRICE_OUT"),
		PriceCache:            envFloat("PRICE_CACHE"),
	}
	// DIAGNOSE 是旧的诊断开关；新环境变量优先于它。
	if v := strings.TrimSpace(Getenv("DIAGNOSE")); v != "" {
		cfg.DiagnosticsEnabled = v != "0"
	}
	for _, field := range []struct {
		key    string
		target *bool
	}{
		{"SESSIONS_ENABLED", &cfg.SessionsEnabled},
		{"PROJECT_MEMORY_ENABLED", &cfg.ProjectMemoryEnabled},
		{"CUSTOM_COMMANDS_ENABLED", &cfg.CustomCommandsEnabled},
		{"SHELL_ENABLED", &cfg.ShellEnabled},
		{"IMAGES_ENABLED", &cfg.ImagesEnabled},
		{"DIAGNOSTICS_ENABLED", &cfg.DiagnosticsEnabled},
		{"SNAPSHOTS_ENABLED", &cfg.SnapshotsEnabled},
		{"BELL_ENABLED", &cfg.BellEnabled},
	} {
		if v := strings.TrimSpace(Getenv(field.key)); v != "" {
			enabled, err := strconv.ParseBool(v)
			if err != nil {
				return Config{}, notes, fmt.Errorf("%s%s 必须是 true / false / 1 / 0", EnvPrefix, field.key)
			}
			*field.target = enabled
		}
	}
	for _, field := range []struct {
		key string
		dst *int
		min int
	}{{"MAX_RETRIES", &cfg.MaxRetries, 0}, {"MAX_OUTPUT_TOKENS", &cfg.MaxOutputTokens, 0}, {"PARALLEL_TOOLS", &cfg.ParallelTools, 1}} {
		if v := strings.TrimSpace(Getenv(field.key)); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < field.min {
				return Config{}, notes, fmt.Errorf("%s%s 必须是大于等于 %d 的整数", EnvPrefix, field.key, field.min)
			}
			*field.dst = n
		}
	}
	cfg.Mode, err = NormalizeMode(cfg.Mode)
	if err != nil {
		return Config{}, notes, err
	}
	switch {
	case envKey != "":
		cfg.APIKey, cfg.KeySource = envKey, KeyFromEnv
	case creds.APIKey != "":
		// Key 来自 credentials 时连接信息也用它保存的，真实环境变量仍可覆盖
		cfg.APIKey, cfg.KeySource = creds.APIKey, KeyFromCredentials
		cfg.BaseURL = firstNonEmpty(firstEnv(baseNames...), user.BaseURL, creds.BaseURL, defaultBaseURL)
		cfg.Model = firstNonEmpty(envModel, user.Model, creds.Model, defaultModel)
		cfg.Protocol = firstNonEmpty(cfg.Protocol, creds.Protocol)
	default:
		if dotKey != "" {
			cfg.APIKey, cfg.KeySource = dotKey, KeyFromDotEnv
		}
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

func settingInt(value *int, fallback int) int {
	if value != nil {
		return *value
	}
	return fallback
}

func settingString(key, fallback string) string {
	if _, ok := os.LookupEnv(EnvPrefix + key); ok {
		return strings.TrimSpace(Getenv(key))
	}
	if _, ok := os.LookupEnv(OldEnvPrefix + key); ok {
		return strings.TrimSpace(Getenv(key))
	}
	return strings.TrimSpace(fallback)
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

// dotEnvAllowed 是工作区 .env 唯一接受的键。
const dotEnvAllowed = "MIAO_API_KEY"

// loadWorkspaceDotEnv 读取当前目录的 .env，返回其中的 MIAO_API_KEY；当前目录是 $HOME 或 $MIAO_HOME 时不读，
// 那里不是项目，不能和全局配置混在一起。
func loadWorkspaceDotEnv() (string, []string) {
	if cwd, err := os.Getwd(); err == nil && appdir.IsGlobalWorkspace(cwd) {
		if _, err := os.Stat(".env"); err == nil {
			return "", []string{"当前目录是用户家目录，不当作项目，已忽略其中的 .env。"}
		}
		return "", nil
	}
	return loadDotEnv(".env")
}

// loadDotEnv 只解析 MIAO_API_KEY，不修改进程环境；其它键只在提示里列出键名，绝不回显值。
func loadDotEnv(path string) (string, []string) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil
	}
	defer func() { _ = f.Close() }()

	var apiKey string
	var ignored []string
	seen := map[string]bool{}
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
		if key == "" {
			continue
		}
		if key == dotEnvAllowed {
			apiKey = strings.TrimSpace(strings.Trim(strings.TrimSpace(value), `"'`))
			continue
		}
		if !seen[key] && validEnvName(key) {
			seen[key] = true
			ignored = append(ignored, key)
		}
	}
	if len(ignored) == 0 {
		return apiKey, nil
	}
	return apiKey, []string{"已忽略工作区 .env 中的 " + strings.Join(ignored, "、") + "（只接受 " + dotEnvAllowed + "）"}
}

// validEnvName 只让形如环境变量名的键出现在提示里，避免把怪异内容（甚至值）打到界面上。
func validEnvName(s string) bool {
	if len(s) > 64 {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
