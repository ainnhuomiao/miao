// Package tui 实现全屏终端界面。
//
// 它基于 Bubble Tea 的 Elm 架构：
//
//	Model   —— 程序的全部状态
//	Update  —— 收到消息后更新状态，并返回新的命令
//	View    —— 根据状态渲染界面
//
// 真正的网络请求在 goroutine 里进行，通过 channel 把增量交给 Update，
// 所以界面永远不会被网络阻塞。
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"agnet/internal/agent"
)

// Run 启动 TUI，直到用户退出。
// 返回后一定会取消可能仍在进行的流式请求。
func Run(ag *agent.Agent, modelName string) error {
	m := New(ag, modelName)
	p := tea.NewProgram(m,
		tea.WithAltScreen(),       // 使用备用屏幕，退出后恢复原终端内容
		tea.WithMouseCellMotion(), // 支持鼠标滚轮
	)
	_, err := p.Run()
	m.Close()
	return err
}
