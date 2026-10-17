package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ---- A：顶栏 logo「=^･ω･^= miao」，思考时偶尔眨眼 ----

const (
	catFace     = "=^･ω･^="
	catBlink    = "=^-ω-^="
	blinkEvery  = 3500 * time.Millisecond
	blinkLength = 150 * time.Millisecond
)

// catBlinking 生成中每 3.5 秒眨一次眼，每次约 150ms；靠 spinner 的 tick 重绘。
func (m *Model) catBlinking() bool {
	if !m.streaming {
		return false
	}
	t := time.Since(m.streamStart) % blinkEvery
	return t >= blinkEvery-blinkLength
}

// catLogo 按档位返回顶栏左侧：2 猫+miao，1 只有猫，0 都不要。
func (m *Model) catLogo(level int) string {
	face := catFace
	if m.catBlinking() {
		face = catBlink
	}
	switch level {
	case 2:
		return " " + lipgloss.NewStyle().Foreground(accentColor).Render(face) + " " + mutedStyle.Render("miao")
	case 1:
		return " " + lipgloss.NewStyle().Foreground(accentColor).Render(face)
	}
	return ""
}

// ---- B：空会话时居中的启动小猫 ----

var welcomeCat = []string{
	" /\\_/\\ ",
	"( ･ω･ )",
	" > ^ < ",
}

const welcomeHint = "输入 / 看命令 · ? 看快捷键"

// isEmptySession 还没有任何用户或模型消息（启动、/new 之后）。
func (m *Model) isEmptySession() bool {
	for _, msg := range m.messages {
		if msg.role == roleUser || msg.role == roleAssistant || msg.role == roleTool {
			return false
		}
	}
	return true
}

// welcomeView 在对话区居中画小猫；终端矮于 20 行只留提示。
// 启动时的其它灰色提示（如目录迁移、已加载 AGENTS.md）排在下面。
func (m *Model) welcomeView(w, h int) string {
	catStyle := lipgloss.NewStyle().Foreground(accentColor)
	nameStyle := lipgloss.NewStyle().Foreground(textColor)
	var lines []string
	if m.height >= 20 {
		lines = append(lines,
			catStyle.Render(welcomeCat[0]),
			catStyle.Render(welcomeCat[1])+"   "+nameStyle.Render("miao"),
			catStyle.Render(welcomeCat[2])+"   "+mutedStyle.Render(welcomeHint),
		)
	} else {
		lines = append(lines, mutedStyle.Render(welcomeHint))
	}
	block := lipgloss.JoinVertical(lipgloss.Left, lines...)
	var notes []string
	for _, msg := range m.messages {
		if msg.role == roleNotice && !strings.HasPrefix(msg.content, "已就绪") {
			notes = append(notes, mutedStyle.Render(truncateWidth(msg.content, max(1, w-2))))
		}
	}
	if len(notes) > 0 {
		block = lipgloss.JoinVertical(lipgloss.Center, block, "", strings.Join(notes, "\n"))
	}
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, block)
}
