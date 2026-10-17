// Package tui 实现全屏终端界面。
package tui

import (
	"context"
	"encoding/json"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"agnet/internal/agent"
	"agnet/internal/mcp"
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
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
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
