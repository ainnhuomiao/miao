// Command agent 是 GoAgent 的程序入口。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"agnet/internal/agent"
	"agnet/internal/config"
	"agnet/internal/headless"
	"agnet/internal/llm"
	"agnet/internal/mcp"
	"agnet/internal/repl"
	"agnet/internal/subagent"
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
	prompt := flag.String("p", "", "非交互模式：发送这段提示词，输出回答后退出（\"-\" 表示只用管道输入）")
	modeFlag := flag.String("mode", "ask", "非交互模式的权限：ask（需审批的一律拒绝）/ edits（自动同意编辑）/ plan（只读）")
	verbose := flag.Bool("v", false, "非交互模式下把工具调用过程打印到 stderr")
	flag.Parse()
	headlessMode := isFlagSet("p")

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	reg, err := tools.NewRegistry(cfg.Workspace)
	if err != nil {
		return err
	}
	tools.RegisterBuiltins(reg)
	mcpMgr := mcp.Start(reg) // 后台连接 MCP 服务器，不阻塞启动
	defer mcpMgr.Close()

	client := llm.NewOpenAI(cfg.APIKey, cfg.BaseURL, cfg.Model)
	system := fmt.Sprintf(
		"%s\n\n当前调用的模型标识是 \"%s\"。工作区根目录是 %q（工具只能访问此目录内的相对路径）。",
		agent.DefaultSystemPrompt, agent.ModelPlaceholder, cfg.Workspace)
	md, truncated, err := agent.LoadAgentsMD(cfg.Workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 %s 失败：%v\n", agent.AgentsMDName, err)
	}
	system = agent.WithAgentsMD(system, md, truncated)
	ag := agent.New(client, system, reg)
	ag.SetAgentsMDLoaded(md != "")
	ag.SetAllowedCommands(agent.AllowedCommandsFromEnv())
	if _, err := subagent.Setup(reg, client, ag.Model); err != nil {
		fmt.Fprintf(os.Stderr, "读取子代理配置失败：%v\n", err)
	}

	if headlessMode {
		mode, err := headless.ParseMode(*modeFlag)
		if err != nil {
			return err
		}
		ag.SetMode(mode)
		var stdin io.Reader
		if !isTerminal(os.Stdin) {
			stdin = os.Stdin
		}
		text, err := headless.BuildPrompt(*prompt, stdin)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		defer ag.KillJobs() // 不留下后台命令
		return headless.Run(ctx, ag, text, os.Stdout, os.Stderr, *verbose)
	}

	if *plain || !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return repl.New(ag, cfg.Model).Run(context.Background())
	}
	return tui.Run(ag, cfg.Model)
}

func isFlagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
