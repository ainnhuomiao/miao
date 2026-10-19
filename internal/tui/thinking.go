package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type thinkingListState struct {
	generation uint64
	fetching   bool
	done       bool
}

type thinkingCapabilitiesMsg struct {
	model      string
	generation uint64
	err        error
}

func (m *Model) invalidateThinking() {
	m.thinkingLevels = thinkingListState{generation: m.thinkingLevels.generation + 1}
	m.invalidateSubagentThinking()
}

func (m *Model) fetchThinkingCmd() tea.Cmd {
	if m.thinkingLevels.fetching || m.thinkingLevels.done {
		return nil
	}
	m.thinkingLevels.fetching = true
	model, generation, ag := m.modelName, m.thinkingLevels.generation, m.ag
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), modelsFetchTimeout)
		defer cancel()
		_, err := ag.QueryThinkingCapabilities(ctx)
		return thinkingCapabilitiesMsg{model: model, generation: generation, err: err}
	}
}

func (m *Model) handleThinkingCapabilities(msg thinkingCapabilitiesMsg) {
	if msg.model != m.modelName || msg.generation != m.thinkingLevels.generation {
		return
	}
	m.thinkingLevels.fetching, m.thinkingLevels.done = false, true
	info := m.ag.ThinkingCapabilities()
	if !m.streaming && !info.Supports(m.ag.Thinking()) {
		if err := m.ag.SetThinking("auto"); err == nil {
			m.notice("当前档位不受此模型支持，已恢复 auto。")
		}
	}
	message := "模型 " + m.modelName + " · " + info.Summary()
	if msg.err != nil {
		message += " 能力接口查询失败，可用 /thinking refresh 重试。"
	}
	m.notice(message)
	m.syncPalette()
}

func (m *Model) thinkingMode() (string, bool) {
	v := m.input.Value()
	if !strings.HasPrefix(v, "/thinking ") || strings.ContainsAny(v, "\n") {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(v, "/thinking "))), true
}

func (m *Model) thinkingMatches(query string) []paletteCmd {
	info := m.ag.ThinkingCapabilities()
	names := info.Levels
	if !info.Known && m.ag.Thinking() != "auto" {
		names = append(names, m.ag.Thinking())
	}
	descs := map[string]string{"auto": "服务端默认", "none": "关闭思考", "minimal": "最少思考", "low": "较低强度", "medium": "中等强度", "high": "较高强度", "xhigh": "更高强度", "max": "最高强度"}
	var matches []paletteCmd
	for _, name := range names {
		if !strings.HasPrefix(name, query) {
			continue
		}
		desc := descs[name]
		if info.Known {
			desc += " · " + info.Source
			if info.Budget && name != "auto" && name != "none" {
				desc += " · 预算预设"
			}
		} else {
			desc += " · 档位未知，可手动输入"
		}
		if m.thinkingLevels.fetching {
			desc += " · 正在查询"
		}
		if name == m.ag.Thinking() {
			desc = "当前 · " + desc
		}
		matches = append(matches, paletteCmd{name: name, desc: desc})
	}
	return matches
}

func (m *Model) changeThinking(args []string) {
	if len(args) == 0 {
		m.notice("当前思考强度：" + m.ag.Thinking() + "。" + m.ag.ThinkingCapabilities().Summary())
		return
	}
	if m.streaming {
		m.notice("正在生成回复，暂不能切换思考强度。")
		return
	}
	if len(args) != 1 {
		m.notice("用法：/thinking <级别> 或 /thinking refresh。" + m.ag.ThinkingCapabilities().Summary())
		return
	}
	if err := m.ag.SetThinking(args[0]); err != nil {
		m.notice("切换思考强度失败：" + err.Error())
		return
	}
	message := "思考强度已设为 " + m.ag.Thinking() + "，将用于后续请求。"
	if !m.ag.ThinkingCapabilities().Known {
		message += "此模型未提供档位元数据，手动设置尚未验证。"
	}
	m.notice(message)
}
