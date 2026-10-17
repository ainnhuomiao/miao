// Command agent 是 GoAgent 的程序入口。
//
// 它只做三件事：读配置、组装依赖、启动 REPL。
// 具体逻辑都在 internal/ 下的包里，便于学习和测试。
package main

import (
	"context"
	"fmt"
	"os"

	"agnet/internal/agent"
	"agnet/internal/config"
	"agnet/internal/llm"
	"agnet/internal/repl"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client := llm.NewOpenAI(cfg.APIKey, cfg.BaseURL, cfg.Model)
	ag := agent.New(client, agent.DefaultSystemPrompt)

	return repl.New(ag, cfg.Model).Run(context.Background())
}
