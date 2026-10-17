package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"miao/internal/config"
)

// modelsFetchTimeout 是 GET /models 的超时。
const modelsFetchTimeout = 5 * time.Second

// knownModels 是 /models 拉取失败时的兜底候选（与上下文窗口表对应的常见名）。
var knownModels = []string{
	"deepseek-chat",
	"deepseek-reasoner",
	"gpt-4.1",
	"gpt-4o",
	"claude-sonnet-4",
	"gemini-2.5-pro",
	"qwen-max",
	"kimi-k2",
	"glm-4.6",
}

// modelListState 缓存可选模型列表。
type modelListState struct {
	fetched  []string // 服务端返回的列表；nil 表示未拿到
	fetching bool
	done     bool // 已尝试过拉取（成功或失败）
}

// modelsMsg 是异步拉取模型列表的结果。
type modelsMsg struct {
	list []string
	err  error
}

// envModels 解析 MIAO_MODELS（逗号分隔），未设置返回 nil。
func envModels() []string {
	var out []string
	for _, s := range strings.Split(config.Getenv("MODELS"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// fetchModelsCmd 异步拉取模型列表；已拉取、正在拉取或 MIAO_MODELS 已设置时返回 nil。
func (m *Model) fetchModelsCmd() tea.Cmd {
	if m.models.done || m.models.fetching || envModels() != nil {
		return nil
	}
	m.models.fetching = true
	ag := m.ag
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), modelsFetchTimeout)
		defer cancel()
		list, err := ag.ListModels(ctx)
		return modelsMsg{list: list, err: err}
	}
}

func (m *Model) handleModelsMsg(msg modelsMsg) {
	m.models.fetching = false
	m.models.done = true
	if msg.err == nil && len(msg.list) > 0 {
		m.models.fetched = msg.list
	}
	m.syncPalette()
}

// availableModels 返回候选模型：MIAO_MODELS > 服务端列表 > 兜底列表；当前模型总在其中。
func (m *Model) availableModels() []string {
	base := envModels()
	if base == nil {
		base = m.models.fetched
	}
	if base == nil {
		base = knownModels
	}
	out := []string{m.modelName}
	for _, n := range base {
		if n != m.modelName {
			out = append(out, n)
		}
	}
	return out
}

// switchModel 切换模型；生成中拒绝。
func (m *Model) switchModel(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if m.streaming {
		m.notice("正在生成回复，暂不能切换模型。")
		return
	}
	if name == m.modelName {
		m.notice("当前已是 " + name)
		return
	}
	m.applyModel(name)
	m.notice("已切换到 " + name)
}

// applyModel 更新 Agent、顶栏模型名与当前会话记录；上下文窗口由 contextWindow(modelName) 重新查表。
func (m *Model) applyModel(name string) {
	m.ag.SetModel(name)
	m.modelName = name
	if m.sess != nil {
		m.sess.Model = name
	}
	m.layout()
	m.refreshViewport(false)
}

// modelMode 判断输入是否处于 /model 二级列表（"/model " 之后，单行）。
func (m *Model) modelMode() (query string, ok bool) {
	v := m.input.Value()
	if !strings.HasPrefix(v, "/model ") || strings.ContainsAny(v, "\n") {
		return "", false
	}
	return strings.TrimLeft(strings.TrimPrefix(v, "/model "), " \t"), true
}

// modelMatches 按完全匹配、前缀、子序列筛选模型，作为菜单行。
func (m *Model) modelMatches(query string) []paletteCmd {
	q := strings.ToLower(query)
	var exact, prefix, fuzzy []paletteCmd
	for _, n := range m.availableModels() {
		c := paletteCmd{name: n}
		if n == m.modelName {
			c.desc = "当前"
		}
		ln := strings.ToLower(n)
		switch {
		case ln == q:
			exact = append(exact, c)
		case strings.HasPrefix(ln, q):
			prefix = append(prefix, c)
		case isSubsequence(q, ln):
			fuzzy = append(fuzzy, c)
		}
	}
	return append(append(exact, prefix...), fuzzy...)
}
