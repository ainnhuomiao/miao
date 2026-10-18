// Package tui 实现全屏终端界面。
package tui

import (
	"context"
	"encoding/json"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/mcp"
)

// Run 启动 TUI，直到用户退出。
func Run(ag *agent.Agent, modelName string) error {
	initMarkdown()
	termDark = mdDark
	fillOut = os.Stdout
	setTheme("auto")
	loadSavedTheme()
	defer resetFill()
	m := New(ag, modelName)
	// 备用屏和鼠标模式在 v2 里由 View() 的字段声明。
	p := tea.NewProgram(m,
		tea.WithFPS(30), // 与流式合并刷新的 33ms 对齐，降低空闲 CPU
	)

	ag.SetApprover(func(ctx context.Context, toolName, display string, args json.RawMessage) (bool, error) {
		resp := make(chan bool, 1)
		p.Send(approvalRequestMsg{
			toolName: toolName,
			display:  display,
			args:     args,
			resp:     resp,
		})
		select {
		case allow := <-resp:
			return allow, nil
		case <-ctx.Done():
			p.Send(approvalClearMsg{})
			return false, ctx.Err()
		}
	})

	forwardMCPNotices(p)
	_, err := p.Run()
	m.ag.KillJobs()
	mcp.Default().Close()
	m.Close()
	return err
}
