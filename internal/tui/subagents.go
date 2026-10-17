package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"miao/internal/llm"
	"miao/internal/subagent"
)

const subagentsPrefix = "/subagents "

// 菜单行名。
const (
	subRowEnabled = "开关"
	subRowMax     = "并发上限"
	subRowFollow  = "跟随主模型"
	subRowWeb     = "允许联网"
)

// subagentState 记录子代理的累计用量（不计入 ctxTokens）。
type subagentState struct {
	tokens int // 本会话子代理累计 tokens
}

// subagentsMode 判断是否在 /subagents 设置菜单里。
func (m *Model) subagentsMode() bool {
	v := m.input.Value()
	return strings.HasPrefix(v, subagentsPrefix) && !strings.ContainsAny(v, "\n")
}

// openSubagentsMenu 把输入框切到 /subagents 菜单。
func (m *Model) openSubagentsMenu() {
	m.input.SetValue(subagentsPrefix)
	m.input.CursorEnd()
	m.fitInput()
	m.syncPalette()
}

func subagentConfig() (subagent.Config, *subagent.Manager) {
	mgr := subagent.Current()
	if mgr == nil {
		return subagent.Defaults(), nil
	}
	return mgr.Config(), mgr
}

// subagentsMatches 是菜单行：开关、并发上限、模型列表。
func (m *Model) subagentsMatches() []paletteCmd {
	cfg, _ := subagentConfig()
	on := "关（task 工具不暴露给模型）· Enter 切换"
	if cfg.Enabled {
		on = "开 · Enter 切换"
	}
	rows := []paletteCmd{
		{name: subRowEnabled, desc: on},
		{name: subRowMax, desc: fmt.Sprintf("%d（←/→ 或 +/- 调整，1~8）", cfg.Max)},
	}
	rows = append(rows, subWebRow(cfg))
	follow := paletteCmd{name: subRowFollow, desc: "子代理模型：使用主代理当前模型（" + m.modelName + "）"}
	if cfg.Model == "" {
		follow.desc += " ✓"
	}
	rows = append(rows, follow)
	for _, n := range m.availableModels() {
		c := paletteCmd{name: n, desc: "子代理模型"}
		if n == cfg.Model {
			c.desc += " ✓"
		}
		rows = append(rows, c)
	}
	return rows
}

// handleSubagentsKey 处理菜单里的 Enter / ←→ / +-；其余按键交给通用菜单逻辑。
func (m *Model) handleSubagentsKey(msg tea.KeyMsg, matches []paletteCmd) (bool, tea.Cmd) {
	if !m.subagentsMode() || len(matches) == 0 {
		return false, nil
	}
	sel := matches[min(m.palette.sel, len(matches)-1)].name
	cfg, mgr := subagentConfig()
	switch msg.String() {
	case "left", "-", "right", "+", "=":
		if sel != subRowMax {
			return msg.String() != "left" && msg.String() != "right", nil
		}
		if msg.String() == "left" || msg.String() == "-" {
			cfg.Max--
		} else {
			cfg.Max++
		}
		cfg.Max = max(subagent.MinMax, min(subagent.MaxMax, cfg.Max))
	case "enter":
		switch sel {
		case subRowEnabled:
			cfg.Enabled = !cfg.Enabled
		case subRowMax:
			return true, nil
		case subRowFollow:
			cfg.Model = ""
		case subRowWeb:
			cfg.AllowWeb = !cfg.AllowWeb
		default:
			cfg.Model = sel
		}
	case "tab":
		return true, nil
	default:
		return false, nil
	}
	if mgr == nil {
		m.notice("子代理不可用。")
		return true, nil
	}
	if err := mgr.SetConfig(cfg); err != nil {
		m.notice("子代理设置已生效，但保存失败：" + err.Error())
	}
	return true, m.fetchModelsCmd()
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

// addSubagentUsage 把子代理用量计入本轮用量（底栏），不改 ctxTokens。
func (m *Model) addSubagentUsage(u *llm.Usage) {
	if u == nil {
		return
	}
	m.sub.tokens += u.TotalTokens
	if m.usage == nil || m.usageFresh {
		c := *u
		m.usage = &c
		m.usageFresh = false
		return
	}
	m.usage.PromptTokens += u.PromptTokens
	m.usage.CompletionTokens += u.CompletionTokens
	m.usage.TotalTokens += u.TotalTokens
	m.usage.PromptCacheHitTokens += u.PromptCacheHitTokens
}
