package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// keyBinding 是一条快捷键说明。keys 是 msg.String() 的写法（多个用逗号分隔），
// label 是显示给用户的写法。速查卡、/help 底部的表都从 keyBindings 生成；
// keys_test.go 会扫描源码里出现的 "ctrl+x" 等按键，漏登记就测试失败。
type keyBinding struct {
	keys, label, desc string
}

var keyBindings = []keyBinding{
	{"enter", "Enter", "发送"},
	{"ctrl+j", "Ctrl+J", "换行"},
	{"ctrl+v", "Ctrl+V", "粘贴剪贴板里的图片"},
	{"ctrl+b", "Ctrl+B", "文件树：看改动、预览、@ 引用、u 撤销"},
	{"ctrl+t", "Ctrl+T", "任务清单与子代理结果"},
	{"ctrl+r", "Ctrl+R", "展开或收起思考过程"},
	{"shift+tab", "Shift+Tab", "切换权限模式"},
	{"esc", "Esc", "关闭菜单 · 取消审批 · 取消生成"},
	{"esc", "Esc Esc", "回到之前的某条消息重新编辑"},
	{"ctrl+p,ctrl+n", "Ctrl+P/N", "回退列表里上下移动（同 ↑↓）"},
	{"ctrl+d", "Ctrl+D", "会话列表里删除会话（按两次确认）"},
	{"pgup,pgdown", "PgUp/PgDn", "滚动对话"},
	{"?", "?", "快捷键速查（输入框为空时）"},
	{"ctrl+c", "Ctrl+C", "取消生成，空闲时退出"},
}

// keymapText 是 /help 底部的快捷键表（纯文本）。
func keymapText() string {
	w := 0
	for _, k := range keyBindings {
		w = max(w, lipgloss.Width(k.label))
	}
	var b strings.Builder
	b.WriteString("快捷键:")
	for _, k := range keyBindings {
		b.WriteString("\n  " + k.label + strings.Repeat(" ", w-lipgloss.Width(k.label)+2) + k.desc)
	}
	return b.String()
}

// keysCardView 是按 ? 弹出的速查卡：无底色，键用强调色、说明用灰字，左对齐。
func (m *Model) keysCardView() string {
	w := max(10, m.columnWidth())
	pad := lipgloss.NewStyle().MarginLeft(m.columnMargin())
	keyStyle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	kw := 0
	for _, k := range keyBindings {
		kw = max(kw, lipgloss.Width(k.label))
	}
	lines := []string{titleStyle.Render("快捷键")}
	for _, k := range keyBindings {
		key := keyStyle.Render(k.label + strings.Repeat(" ", kw-lipgloss.Width(k.label)))
		lines = append(lines, "  "+key+"  "+mutedStyle.Render(truncateWidth(k.desc, max(0, w-kw-4))))
	}
	lines = append(lines, mutedStyle.Render("  按任意键关闭"))
	return pad.Render(strings.Join(lines, "\n"))
}

func keysCardHeight() int { return len(keyBindings) + 2 }
