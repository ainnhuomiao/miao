package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/ainnhuomiao/miao/internal/session"
)

// cursorLine 返回真实光标所在行（去掉颜色）以及光标左边的内容（按显示列截取，中文算 2 列）。
func cursorLine(t *testing.T, m *Model) (line, left string, c *tea.Cursor) {
	t.Helper()
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("输入框聚焦时应返回真实光标")
	}
	lines := strings.Split(stripANSI(v.Content), "\n")
	c = v.Cursor
	if c.Y < 0 || c.Y >= len(lines) {
		t.Fatalf("光标行 %d 超出画面 %d 行", c.Y, len(lines))
	}
	line = lines[c.Y]
	return line, xansi.Truncate(line, c.X, ""), c
}

func typeInput(m *Model, s string) {
	for _, r := range s {
		if r == '\n' {
			m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
			continue
		}
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestInputCursorEmpty(t *testing.T) {
	m := newTestModel(t, 100, 30, "")
	line, left, _ := cursorLine(t, m)
	if !strings.HasSuffix(left, "│ › ") || !strings.Contains(line, "输入消息") {
		t.Fatalf("空输入时光标应在提示符后：left=%q line=%q", left, line)
	}
}

func TestInputCursorCJK(t *testing.T) {
	m := newTestModel(t, 100, 30, "")
	typeInput(m, "你好ab")
	_, left, c := cursorLine(t, m)
	if !strings.HasSuffix(left, "│ › 你好ab") {
		t.Fatalf("光标应紧跟“你好ab”（中文 2 列）：%q", left)
	}
	// 左移两格（越过 a b），再左移一格越过“好”：列数减 1+1+2。
	x := c.X
	for range 3 {
		m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	}
	if _, left, c := cursorLine(t, m); c.X != x-4 || !strings.HasSuffix(left, "› 你") {
		t.Fatalf("左移后列应为 %d：%d %q", x-4, c.X, left)
	}
}

func TestInputCursorWrapAndMultiline(t *testing.T) {
	m := newTestModel(t, 60, 30, "")
	w := m.input.Width()
	long := strings.Repeat("中", w/2+3) // 超过一行，软折行
	typeInput(m, long)
	line, left, _ := cursorLine(t, m)
	if strings.Contains(line, "›") || !strings.HasSuffix(left, "│   中中中") {
		t.Fatalf("折行后光标应在第二个可视行的“中中中”之后：left=%q line=%q", left, line)
	}
	typeInput(m, "\nxy")
	_, left, _ = cursorLine(t, m)
	if !strings.HasSuffix(left, "│   xy") {
		t.Fatalf("换行后光标应在新行的 xy 之后：%q", left)
	}
}

func TestInputCursorScrolled(t *testing.T) {
	m := newTestModel(t, 80, 40, "")
	for i := range inputMaxLines + 4 {
		typeInput(m, string(rune('a'+i))+"\n")
	}
	typeInput(m, "end")
	_, left, _ := cursorLine(t, m)
	if !strings.HasSuffix(left, "│   end") {
		t.Fatalf("输入框滚动后光标应在最后一行 end 之后：%q", left)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	_, left, _ = cursorLine(t, m)
	if !strings.HasSuffix(left, "│   "+string(rune('a'+inputMaxLines+3))) {
		t.Fatalf("上移一行后光标应在上一行：%q", left)
	}
}

func TestInputCursorWithOverlays(t *testing.T) {
	m := newTestModel(t, 120, 40, "")
	typeInput(m, "/")
	if !m.paletteOpen() {
		t.Fatal("输入 / 应打开命令菜单")
	}
	if _, left, _ := cursorLine(t, m); !strings.HasSuffix(left, "│ › /") {
		t.Fatalf("命令菜单打开时光标仍应在输入框：%q", left)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.input.Reset()
	m.picker = pickerState{open: true, all: []session.Meta{{ID: "a", Title: "调试"}, {ID: "b", Title: "nix"}}}
	if _, left, _ := cursorLine(t, m); !strings.HasSuffix(left, "│ › ") {
		t.Fatalf("会话列表打开时光标仍应在输入框：%q", left)
	}
	m.picker.open = false
	m.toggleSidebar()
	if !m.sidebarVisible() {
		t.Skip("当前环境不显示文件树")
	}
	// 文件树打开时按键交给文件树，这里只看光标是否跟着输入框右移。
	if _, left, c := cursorLine(t, m); !strings.HasSuffix(left, "│ › ") || c.X <= m.sidebarCols() {
		t.Fatalf("文件树打开时光标应右移到输入框：%d %q", c.X, left)
	}
}
