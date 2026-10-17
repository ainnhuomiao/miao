// Command agent 是 GoAgent 的程序入口。
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
	"agnet/internal/tools"
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

	reg, err := tools.NewRegistry(cfg.Workspace)
	if err != nil {
		return err
	}
	tools.RegisterBuiltins(reg)

	client := llm.NewOpenAI(cfg.APIKey, cfg.BaseURL, cfg.Model)
	system := fmt.Sprintf(
		"%s\n\n当前调用的模型标识是 %q。工作区根目录是 %q（工具只能访问此目录内的相对路径）。",
		agent.DefaultSystemPrompt, cfg.Model, cfg.Workspace)
	md, truncated, err := agent.LoadAgentsMD(cfg.Workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 %s 失败：%v\n", agent.AgentsMDName, err)
	}
	system = agent.WithAgentsMD(system, md, truncated)
	ag := agent.New(client, system, reg)
	ag.SetAgentsMDLoaded(md != "")

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
