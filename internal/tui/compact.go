package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ainnhuomiao/miao/internal/agent"
)

// compactKeepTurns 是压缩时保留原文的最近轮数。
const compactKeepTurns = 2

// autoCompactPct 是自动压缩阈值（上下文用量百分比）。
const autoCompactPct = 80

type compactDoneMsg struct {
	res  agent.CompactResult
	err  error
	auto bool
}

// needAutoCompact 判断本轮结束后是否该自动压缩：窗口已知且用量 ≥ 80%。
func (m *Model) needAutoCompact() bool {
	total := contextWindow(m.modelName)
	return total > 0 && m.ctxTokens*100/total >= autoCompactPct
}

// startCompact 在后台压缩上下文；工具行显示「✻ 压缩上下文…」，Esc 可取消。
func (m *Model) startCompact(auto bool) tea.Cmd {
	if m.streaming {
		m.notice("正在生成，稍后再压缩。")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.streaming, m.compacting = true, true
	m.streamStart = time.Now()
	m.cancel = cancel
	ag := m.ag
	run := func() tea.Msg {
		res, err := ag.Compact(ctx, compactKeepTurns)
		return compactDoneMsg{res: res, err: err, auto: auto}
	}
	return tea.Batch(run, m.spinner.Tick)
}

func (m *Model) handleCompactDone(msg compactDoneMsg) (tea.Model, tea.Cmd) {
	m.streaming, m.compacting = false, false
	m.cancel = nil
	switch {
	case errors.Is(msg.err, context.Canceled):
		m.notice("已取消压缩，上下文保持不变。")
	case msg.err != nil:
		if !msg.auto { // 自动触发时对话太短之类的失败不打扰用户
			m.notice("压缩失败：" + msg.err.Error())
		}
	default:
		m.resetRewindPoints() // 历史已被摘要替换，旧下标失效
		before := max(m.ctxTokens, msg.res.Before)
		m.ctxTokens = msg.res.After
		prefix := "已压缩"
		if msg.auto {
			prefix = "上下文已用 80%，自动压缩"
		}
		m.notice(fmt.Sprintf("%s %s → %s", prefix, formatTokens(before), formatTokens(msg.res.After)))
		m.saveCurrentSession()
	}
	m.refreshViewport(true)
	return m, nil
}
