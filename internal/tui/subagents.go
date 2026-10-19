package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/subagent"
)

const subagentsPrefix = "/subagents "

const (
	subRowEnabled        = "开关"
	subRowMax            = "并发上限"
	subRowModel          = "模型"
	subRowThinking       = "思考强度"
	subRowFollow         = "跟随主模型"
	subRowFollowThinking = "跟随主代理"
	subRowBack           = "‹ 返回设置"
	subRowRefresh        = "重新查询档位"
	subRowWeb            = "允许联网"
)

// subagentState 保存子代理累计用量和模型思考能力查询状态。
type subagentState struct {
	tokens         int
	thinkingLevels thinkingListState
	thinkingModel  string
}

type subagentThinkingMsg struct {
	model      string
	generation uint64
	err        error
}

func (m *Model) subagentsMode() bool {
	v := m.input.Value()
	return strings.HasPrefix(v, subagentsPrefix) && !strings.ContainsAny(v, "\n")
}

func (m *Model) subagentsPage() (page, query string) {
	rest := strings.TrimPrefix(m.input.Value(), subagentsPrefix)
	for _, name := range []string{"model", "thinking"} {
		if rest == name || strings.HasPrefix(rest, name+" ") {
			return name, strings.TrimSpace(strings.TrimPrefix(rest, name))
		}
	}
	return "", strings.TrimSpace(rest)
}

func (m *Model) openSubagentsMenu() { m.openSubagentsPage("", "") }

func (m *Model) openSubagentsPage(page, focus string) {
	v := subagentsPrefix
	if page != "" {
		v += page + " "
	}
	m.input.SetValue(v)
	m.input.CursorEnd()
	m.fitInput()
	m.syncPalette()
	for i, row := range m.subagentsMatches() {
		if row.name == focus {
			m.palette.sel = i
			m.paletteScroll()
			break
		}
	}
}

func subagentConfig() (subagent.Config, *subagent.Manager) {
	mgr := subagent.Current()
	if mgr == nil {
		return subagent.Defaults(), nil
	}
	return mgr.Config(), mgr
}

func (m *Model) subagentModel(cfg subagent.Config) string {
	if cfg.Model != "" {
		return cfg.Model
	}
	return m.modelName
}

func (m *Model) subagentThinkingInfo(cfg subagent.Config) llm.ThinkingCapabilities {
	return m.ag.ThinkingCapabilitiesForModel(m.subagentModel(cfg))
}

func (m *Model) invalidateSubagentThinking() {
	m.sub.thinkingLevels = thinkingListState{generation: m.sub.thinkingLevels.generation + 1}
	m.sub.thinkingModel = ""
}

func (m *Model) subagentsFetchCmd() tea.Cmd {
	if !m.subagentsMode() {
		return nil
	}
	page, _ := m.subagentsPage()
	if page == "model" {
		return m.fetchModelsCmd()
	}
	if page == "thinking" {
		return m.fetchSubagentThinkingCmd()
	}
	return nil
}

func (m *Model) fetchSubagentThinkingCmd() tea.Cmd {
	cfg, _ := subagentConfig()
	model := m.subagentModel(cfg)
	if m.sub.thinkingModel != model {
		m.invalidateSubagentThinking()
		m.sub.thinkingModel = model
	}
	if m.sub.thinkingLevels.fetching || m.sub.thinkingLevels.done {
		return nil
	}
	m.sub.thinkingLevels.fetching = true
	generation, ag := m.sub.thinkingLevels.generation, m.ag
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), modelsFetchTimeout)
		defer cancel()
		_, err := ag.QueryThinkingCapabilitiesForModel(ctx, model)
		return subagentThinkingMsg{model: model, generation: generation, err: err}
	}
}

func (m *Model) handleSubagentThinking(msg subagentThinkingMsg) {
	cfg, mgr := subagentConfig()
	if msg.model != m.subagentModel(cfg) || msg.generation != m.sub.thinkingLevels.generation {
		return
	}
	m.sub.thinkingLevels.fetching, m.sub.thinkingLevels.done = false, true
	if cfg.Thinking != "" && !m.subagentThinkingInfo(cfg).Supports(cfg.Thinking) && mgr != nil {
		cfg.Thinking = "auto"
		if m.saveSubagentConfig(cfg) {
			m.notice("所选子代理模型不支持原思考强度，已恢复为服务端默认 auto。")
		}
	}
	if msg.err != nil {
		m.notice("子代理模型档位查询失败，可在思考强度菜单选择“重新查询档位”。")
	}
	m.syncPalette()
}

func subMatches(query, name string) bool {
	return isSubsequence(strings.ToLower(query), strings.ToLower(name))
}

func (m *Model) subagentsMatches() []paletteCmd {
	cfg, _ := subagentConfig()
	page, query := m.subagentsPage()
	var rows []paletteCmd
	if page != "" && query == "" {
		rows = append(rows, paletteCmd{name: subRowBack, desc: "Esc / ← 返回"})
	}
	switch page {
	case "model":
		follow := paletteCmd{name: subRowFollow, desc: m.modelName}
		if cfg.Model == "" {
			follow.desc += " ✓"
		}
		if subMatches(query, follow.name) {
			rows = append(rows, follow)
		}
		names := m.availableModels()
		if cfg.Model != "" && !slices.Contains(names, cfg.Model) {
			names = append([]string{cfg.Model}, names...)
		}
		for _, name := range names {
			if !subMatches(query, name) {
				continue
			}
			desc := "Enter 选择"
			if name == cfg.Model {
				desc = "当前 ✓"
			}
			rows = append(rows, paletteCmd{name: name, desc: desc})
		}
	case "thinking":
		info := m.subagentThinkingInfo(cfg)
		follow := paletteCmd{name: subRowFollowThinking, desc: "当前 " + m.ag.Thinking()}
		if !info.Supports(m.ag.Thinking()) {
			follow.desc += "；此模型使用 auto"
		}
		if cfg.Thinking == "" {
			follow.desc += " ✓"
		}
		if subMatches(query, follow.name) {
			rows = append(rows, follow)
		}
		names := info.Levels
		if !info.Known && cfg.Thinking != "" && !slices.Contains(names, cfg.Thinking) {
			names = append(names, cfg.Thinking)
		}
		for _, name := range names {
			if !subMatches(query, name) {
				continue
			}
			desc := info.Source
			if name == "auto" {
				desc = "使用所选模型的服务端默认"
			}
			if name == "none" {
				desc = "关闭思考 · " + info.Source
			}
			if info.Budget && name != "auto" && name != "none" {
				desc += " · 预算预设"
			}
			if !info.Known {
				desc += " · 档位未知，可手动输入"
			}
			if m.sub.thinkingLevels.fetching {
				desc += " · 正在查询"
			}
			if name == cfg.Thinking {
				desc += " ✓"
			}
			rows = append(rows, paletteCmd{name: name, desc: desc})
		}
		if query == "" {
			rows = append(rows, paletteCmd{name: subRowRefresh, desc: m.subagentModel(cfg)})
		}
	default:
		on := "关 · Enter 开启"
		if cfg.Enabled {
			on = "开 · Enter 关闭"
		}
		model := cfg.Model
		if model == "" {
			model = "跟随主模型（" + m.modelName + "）"
		}
		thinking := cfg.Thinking
		if thinking == "" {
			thinking = "跟随主代理（" + m.ag.Thinking() + "）"
			if !m.subagentThinkingInfo(cfg).Supports(m.ag.Thinking()) {
				thinking = "跟随主代理（当前使用 auto）"
			}
		}
		rows = []paletteCmd{
			{name: subRowEnabled, desc: on},
			{name: subRowModel, desc: model + " ›"},
			{name: subRowThinking, desc: thinking + " ›"},
			{name: subRowMax, desc: fmt.Sprintf("%d（←/→ 或 +/- 调整，1~8）", cfg.Max)},
			subWebRow(cfg),
		}
		if query != "" {
			filtered := rows[:0]
			for _, row := range rows {
				if subMatches(query, row.name) {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
		}
	}
	// 完整名称优先，避免输入 gpt-5 时误选排在前面的 gpt-5-mini。
	if query != "" {
		for i, row := range rows {
			if strings.EqualFold(row.name, query) {
				copy(rows[1:i+1], rows[:i])
				rows[0] = row
				break
			}
		}
	}
	return rows
}

func (m *Model) saveSubagentConfig(cfg subagent.Config) bool {
	_, mgr := subagentConfig()
	if mgr == nil {
		m.notice("子代理不可用。")
		return false
	}
	if err := mgr.SetConfig(cfg); err != nil {
		m.notice("子代理设置保存失败：" + err.Error())
		return false
	}
	m.syncPalette()
	return true
}

func (m *Model) handleSubagentsKey(msg tea.KeyPressMsg, matches []paletteCmd) (bool, tea.Cmd) {
	if !m.subagentsMode() {
		return false, nil
	}
	page, query := m.subagentsPage()
	cfg, _ := subagentConfig()
	key := msg.String()
	if page != "" && (key == "esc" || key == "left" || key == "backspace" && query == "") {
		focus := subRowModel
		if page == "thinking" {
			focus = subRowThinking
		}
		m.openSubagentsPage("", focus)
		return true, nil
	}
	sel := ""
	if len(matches) > 0 {
		sel = matches[min(m.palette.sel, len(matches)-1)].name
	}
	if page == "" {
		switch key {
		case "enter", "tab", "right":
			if sel == subRowModel {
				focus := cfg.Model
				if focus == "" {
					focus = subRowFollow
				}
				m.openSubagentsPage("model", focus)
				return true, m.fetchModelsCmd()
			}
			if sel == subRowThinking {
				focus := cfg.Thinking
				if focus == "" {
					focus = subRowFollowThinking
				}
				m.openSubagentsPage("thinking", focus)
				return true, m.fetchSubagentThinkingCmd()
			}
		}
		switch key {
		case "left", "-", "right", "+", "=":
			if sel != subRowMax {
				return false, nil
			}
			if key == "left" || key == "-" {
				cfg.Max--
			} else {
				cfg.Max++
			}
		case "enter":
			switch sel {
			case subRowEnabled:
				cfg.Enabled = !cfg.Enabled
			case subRowWeb:
				cfg.AllowWeb = !cfg.AllowWeb
			default:
				return true, nil
			}
		case "tab":
			return true, nil
		default:
			return false, nil
		}
		m.saveSubagentConfig(cfg.Normalize())
		return true, nil
	}
	if key == "tab" {
		if sel != "" && sel != subRowBack && sel != subRowRefresh {
			m.input.SetValue(subagentsPrefix + page + " " + sel)
			m.input.CursorEnd()
			m.fitInput()
			m.syncPalette()
		}
		return true, nil
	}
	if key != "enter" {
		return false, nil
	}
	if sel == subRowBack {
		focus := subRowModel
		if page == "thinking" {
			focus = subRowThinking
		}
		m.openSubagentsPage("", focus)
		return true, nil
	}
	if sel == subRowRefresh {
		m.invalidateSubagentThinking()
		return true, m.fetchSubagentThinkingCmd()
	}
	if sel == "" {
		sel = query
	}
	if sel == "" {
		return true, nil
	}
	focus := subRowModel
	if page == "model" {
		cfg.Model = sel
		if sel == subRowFollow {
			cfg.Model = ""
		}
		if cfg.Thinking != "" && !m.subagentThinkingInfo(cfg).Supports(cfg.Thinking) {
			cfg.Thinking = "auto"
		}
	} else {
		focus = subRowThinking
		cfg.Thinking = ""
		if sel != subRowFollowThinking {
			level, err := llm.NormalizeThinking(sel)
			if err != nil {
				m.notice(err.Error())
				return true, nil
			}
			info := m.subagentThinkingInfo(cfg)
			if !info.Supports(level) {
				m.notice("子代理模型不支持此档位。" + info.Summary())
				return true, nil
			}
			cfg.Thinking = level
		}
	}
	if m.saveSubagentConfig(cfg) {
		if page == "model" {
			m.invalidateSubagentThinking()
		}
		m.openSubagentsPage("", focus)
		if page == "thinking" && cfg.Thinking != "" && cfg.Thinking != "auto" && !m.subagentThinkingInfo(cfg).Known {
			m.notice("子代理思考强度已保存，但此模型档位未知，手动设置尚未验证。")
		}
	}
	return true, nil
}

// subagentArgs 解析 task 调用的 description。
func subagentDesc(args string) string {
	var in struct {
		Description string `json:"description"`
	}
	_ = json.Unmarshal([]byte(args), &in)
	if d := strings.TrimSpace(in.Description); d != "" {
		return d
	}
	return "子代理"
}

// runningSubagents 返回正在运行（不含排队）的 task 工具行。
func (m *Model) runningSubagents() []chatMessage {
	var out []chatMessage
	for _, msg := range m.messages {
		if msg.role == roleTool && msg.toolName == subagent.ToolName && !msg.done &&
			msg.toolLive != "" && msg.toolLive != subagent.ActivityQueued {
			out = append(out, msg)
		}
	}
	return out
}

func (m *Model) subagentsRunning() bool { return len(m.runningSubagents()) > 0 }

// subagentActivityView 在工具行显示「⑂ 2/3 子代理 · 搜索 foo」。
// 优先级低于实时命令输出（最新工具行是带输出的非 task 工具时让位），高于任务清单。
func (m *Model) subagentActivityView(w int) (string, bool) {
	running := m.runningSubagents()
	if len(running) == 0 {
		return "", false
	}
	if rows := m.activityRows(); len(rows) > 0 && !rows[0].done && rows[0].toolName != subagent.ToolName && rows[0].toolLive != "" {
		return "", false
	}
	cfg, _ := subagentConfig()
	last := running[len(running)-1]
	act := strings.TrimSpace(firstLine(last.toolLive))
	bar := lipgloss.NewStyle().Foreground(toolColor).Render("▏")
	prefix := bar + lipgloss.NewStyle().Foreground(accentColor).Render(fmt.Sprintf("⑂ %d/%d 子代理", len(running), cfg.Max)) + mutedStyle.Render(" · ")
	right := m.activityRight(w, lipgloss.Width(prefix)+8)
	return joinActivity(w, prefix, act, lipgloss.NewStyle().Foreground(textColor), right), true
}

// subagentPanelLines 是 Ctrl+T 面板里的子代理分节。
func (m *Model) subagentPanelLines(w int) []string {
	running := m.runningSubagents()
	if len(running) == 0 {
		return nil
	}
	head := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	lines := []string{" " + head.Render(fmt.Sprintf("⑂ 子代理（%d 个运行中）", len(running)))}
	for _, r := range running {
		text := subagentDesc(r.toolArgs) + " · " + strings.TrimSpace(firstLine(r.toolLive))
		lines = append(lines, "   "+lipgloss.NewStyle().Foreground(textColor).Render(truncateWidth(text, max(1, w-4))))
	}
	return lines
}

// addSubagentUsage 把子代理用量单独累计，不混进主循环的 usage / 缓存命中。
func (m *Model) addSubagentUsage(u *llm.Usage) {
	if u == nil {
		return
	}
	m.sub.tokens += u.TotalTokens
	if m.subUsage == nil {
		c := *u
		m.subUsage = &c
		return
	}
	m.subUsage.PromptTokens += u.PromptTokens
	m.subUsage.CompletionTokens += u.CompletionTokens
	m.subUsage.TotalTokens += u.TotalTokens
	m.subUsage.PromptCacheHitTokens += u.PromptCacheHitTokens
}
