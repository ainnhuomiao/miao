// Package config 负责读取运行配置。
//
// 优先级：真实环境变量 > 当前目录下的 .env 文件 > 内置默认值。
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultBaseURL = "https://api.deepseek.com"
	defaultModel   = "deepseek-chat"
)

// Config 是程序启动所需的全部配置。
type Config struct {
	APIKey    string
	BaseURL   string
	Model     string
	Workspace string
}

// Load 从环境变量和 .env 文件加载配置。
func Load() (Config, error) {
	loadDotEnv(".env")

	ws := firstEnv("AGENT_WORKSPACE")
	if ws == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("获取工作目录失败: %w", err)
		}
		ws = cwd
	}
	abs, err := filepath.Abs(ws)
	if err != nil {
		return Config{}, fmt.Errorf("解析工作区失败: %w", err)
	}

	cfg := Config{
		APIKey: firstEnv("AGENT_API_KEY", "DEEPSEEK_API_KEY"),
		BaseURL: firstNonEmpty(
			firstEnv("AGENT_BASE_URL", "DEEPSEEK_BASE_URL"),
			defaultBaseURL,
		),
		Model: firstNonEmpty(
			firstEnv("AGENT_MODEL", "DEEPSEEK_MODEL"),
			defaultModel,
		),
		Workspace: abs,
	}

	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf(
			"缺少 API Key：请执行 `cp .env.example .env`，然后填入 AGENT_API_KEY")
	}
	return cfg, nil
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

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
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

		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}
