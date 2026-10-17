// Package config 负责读取运行配置。
//
// 优先级：真实环境变量 > 当前目录下的 .env 文件 > 内置默认值。
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	defaultBaseURL = "https://api.deepseek.com"
	defaultModel   = "deepseek-chat"
)

// Config 是程序启动所需的全部配置。
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
}

// Load 从环境变量和 .env 文件加载配置。
func Load() (Config, error) {
	loadDotEnv(".env")

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
	}

	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf(
			"缺少 API Key：请执行 `cp .env.example .env`，然后填入 AGENT_API_KEY")
	}
	return cfg, nil
}

// firstEnv 返回第一个存在且非空的环境变量。
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

// loadDotEnv 读取一个简单的 KEY=VALUE 文件。
//
// 它故意只支持最常用的子集：空行、# 注释、可选的 export 前缀、
// 以及成对的引号。已经存在的环境变量不会被覆盖。
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // 没有 .env 就只用环境变量，这不是错误
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=") // Go 1.18+ 的字符串切割
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
