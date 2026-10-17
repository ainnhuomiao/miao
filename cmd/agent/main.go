// Command agent 是 GoAgent 的程序入口。
//
// 它只做三件事：读配置、组装依赖、启动 REPL。
// 具体逻辑都在 internal/ 下的包里，便于学习和测试。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"agnet/internal/agent"
	"agnet/internal/config"
	"agnet/internal/llm"
	"agnet/internal/repl"
	"agnet/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	plain := flag.Bool("plain", false, "使用简单的逐行 REPL，而不是全屏 TUI")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client := llm.NewOpenAI(cfg.APIKey, cfg.BaseURL, cfg.Model)
	system := fmt.Sprintf(
		"%s\n\n当前调用的模型标识是 %q，由用户通过配置指定。",
		agent.DefaultSystemPrompt, cfg.Model)
	ag := agent.New(client, system)

	// 没有真正的终端（例如管道、重定向、CI）时自动退回逐行 REPL。
	if *plain || !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return repl.New(ag, cfg.Model).Run(context.Background())
	}
	return tui.Run(ag, cfg.Model)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
