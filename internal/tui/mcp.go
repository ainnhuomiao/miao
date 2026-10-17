package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"agnet/internal/mcp"
)

// mcpNoticeMsg 是 MCP 后台连接产生的灰色提示。
type mcpNoticeMsg string

// forwardMCPNotices 把 MCP 管理器的提示转发进 TUI；管理器未启动时什么也不做。
func forwardMCPNotices(p *tea.Program) {
	mgr := mcp.Default()
	if mgr == nil {
		return
	}
	go func() {
		for n := range mgr.Notices() {
			p.Send(mcpNoticeMsg(n))
		}
	}()
}

// mcpSummary 是 /mcp 命令的输出。
func mcpSummary() string {
	mgr := mcp.Default()
	if mgr == nil {
		return "MCP 未启用。"
	}
	return mgr.Summary()
}
