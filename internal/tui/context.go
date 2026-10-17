package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// contextWindows 是常见模型的上下文窗口（token），按模型名包含关系匹配，越具体越靠前。
var contextWindows = []struct {
	key  string
	size int
}{
	{"gpt-4.1", 1_000_000},
	{"gpt-4o", 128_000},
	{"claude", 200_000},
	{"gemini", 1_000_000},
	{"deepseek", 128_000},
	{"qwen", 128_000},
	{"kimi", 128_000},
	{"glm", 128_000},
}

// contextWindow 返回模型上下文窗口；AGNET_CONTEXT_WINDOW 优先，查不到返回 0。
func contextWindow(model string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("AGNET_CONTEXT_WINDOW"))); err == nil && n > 0 {
		return n
	}
	name := strings.ToLower(model)
	for _, c := range contextWindows {
		if strings.Contains(name, c.key) {
			return c.size
		}
	}
	return 0
}

// pacmanMeter 画吃豆人进度：身后吃过的留空，前面是没吃的豆子。
//
//	ᗧ•••••••••••••••  0%     ·······ᗧ••••••••  48%
func pacmanMeter(used, total, cells int, mouthClosed bool) string {
	if total <= 0 || cells < 2 {
		return ""
	}
	pct := float64(used) / float64(total)
	pos := min(cells-1, max(0, int(pct*float64(cells-1)+0.5)))
	color := pacYellowColor
	switch {
	case pct > 0.95:
		color = errorColor
	case pct > 0.8:
		color = toolColor
	}
	pac := "ᗧ"
	if mouthClosed {
		pac = "●"
	}
	return strings.Repeat(" ", pos) +
		lipgloss.NewStyle().Foreground(color).Bold(true).Render(pac) +
		mutedStyle.Render(strings.Repeat("•", cells-1-pos))
}

// contextView 是顶栏右侧：ᗧ•••• 42% · 54k/128k；未知模型只显示 54k tok。
func (m *Model) contextView() string {
	total := contextWindow(m.modelName)
	if total == 0 {
		return mutedStyle.Render(formatTokens(m.ctxTokens) + " tok")
	}
	closed := m.streaming && time.Since(m.streamStart)/(250*time.Millisecond)%2 == 1
	pct := min(100, m.ctxTokens*100/total)
	return pacmanMeter(m.ctxTokens, total, 16, closed) +
		mutedStyle.Render(fmt.Sprintf("  %d%% · %s/%s", pct, formatTokens(m.ctxTokens), formatTokens(total)))
}
