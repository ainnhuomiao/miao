package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func paletteNames(m *Model) []string {
	var out []string
	for _, c := range m.paletteMatches() {
		out = append(out, c.name)
	}
	return out
}

func TestPaletteSlashShowsAll(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	typeText(m, "/")
	if !m.paletteOpen() {
		t.Fatal("输入 / 后菜单应打开")
	}
	if got := len(m.paletteMatches()); got != len(m.paletteCommands()) {
		t.Fatalf("匹配 %d 条，想要全部 %d 条", got, len(m.paletteCommands()))
	}
	view := ansi.Strip(m.View())
	for i, c := range m.paletteCommands() {
		if i >= paletteMaxRows { // 超出 8 行的部分靠滚动显示
			break
		}
		if !strings.Contains(view, c.name) {
			t.Fatalf("渲染中缺少 %s", c.name)
		}
	}
	if !strings.Contains(view, "↓") {
		t.Fatal("命令超过 8 条时最后一行应有灰字 ↓")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp}) // 第一项按 ↑ 跳到最后一项
	if v := ansi.Strip(m.View()); !strings.Contains(v, "/exit") {
		t.Fatal("↑ 回到最后一项后 /exit 应滚出来")
	}
	if strings.Contains(view, "工具待命") {
		t.Fatal("菜单应替代工具状态行")
	}
	if os.Getenv("PALETTE_PREVIEW") != "" {
		t.Log("\n" + view)
	}
}

func TestPaletteFilterPrefix(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	typeText(m, "/se")
	if got := paletteNames(m); len(got) != 1 || got[0] != "/sessions" {
		t.Fatalf("/se 匹配 %v，想要 [/sessions]", got)
	}
}

func TestPaletteFuzzyAndNoMatch(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	typeText(m, "/rsn")
	if got := paletteNames(m); len(got) != 1 || got[0] != "/reasoning" {
		t.Fatalf("/rsn 匹配 %v", got)
	}
	typeText(m, "zzz")
	if !strings.Contains(ansi.Strip(m.View()), "没有匹配的命令") {
		t.Fatal("无匹配时应显示提示")
	}
	if n := len(strings.Split(m.View(), "\n")); n != 20 {
		t.Fatalf("无匹配时渲染 %d 行，想要 20", n)
	}
}

func TestPaletteArrowSelect(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	typeText(m, "/")
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.palette.sel != 2 {
		t.Fatalf("sel=%d，想要 2", m.palette.sel)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.palette.sel != 1 {
		t.Fatalf("sel=%d，想要 1", m.palette.sel)
	}
	if !strings.Contains(ansi.Strip(m.View()), "› /new") {
		t.Fatal("选中行应显示 › /new")
	}
}

func TestPaletteTabComplete(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	typeText(m, "/lo")
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if v := m.input.Value(); v != "/load " {
		t.Fatalf("补全后=%q，想要 \"/load \"", v)
	}
	if m.paletteOpen() {
		t.Fatal("补全后菜单应关闭")
	}
}

func TestPaletteEnterLoadCompletes(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	typeText(m, "/load")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if v := m.input.Value(); v != "/load " {
		t.Fatalf("/load Enter 后=%q，想要补全", v)
	}
}

func TestPaletteEnterRunsNew(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	if m.sess == nil {
		t.Fatal("会话存储不可用")
	}
	old := m.sess.ID
	typeText(m, "/ne")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.sess.ID == old {
		t.Fatal("Enter 执行 /new 后会话 id 应变化")
	}
	if m.input.Value() != "" || m.paletteOpen() {
		t.Fatal("执行后输入应清空、菜单关闭")
	}
}

func TestPaletteEscCloses(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	typeText(m, "/h")
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.paletteOpen() {
		t.Fatal("Esc 后菜单应关闭")
	}
	if m.input.Value() != "/h" {
		t.Fatalf("Esc 应保留输入，得到 %q", m.input.Value())
	}
	typeText(m, "e")
	if !m.paletteOpen() {
		t.Fatal("内容变化后菜单应重新打开")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.input.Value() != "" || m.paletteOpen() {
		t.Fatal("删掉 / 后菜单应关闭")
	}
}

func TestPaletteHiddenDuringApproval(t *testing.T) {
	m := newTestModel(t, 80, 20, "ok")
	typeText(m, "/")
	m.Update(approvalRequestMsg{toolName: "run_command", display: "ls", resp: make(chan bool, 1)})
	if m.paletteOpen() {
		t.Fatal("审批卡打开时不应显示菜单")
	}
}

func TestPaletteViewFitsHeight(t *testing.T) {
	for _, h := range []int{14, 20, 30} {
		m := newTestModel(t, 80, h, "ok")
		for _, s := range []string{"/", "/se", "/x"} {
			m.input.Reset()
			m.syncPalette()
			typeText(m, s)
			if n := len(strings.Split(m.View(), "\n")); n != h {
				t.Fatalf("高 %d 输入 %q 渲染 %d 行", h, s, n)
			}
		}
		m.input.Reset()
		m.syncPalette()
		if n := len(strings.Split(m.View(), "\n")); n != h {
			t.Fatalf("高 %d 关闭菜单后渲染 %d 行", h, n)
		}
	}
}

func TestPaletteScrollKeepsSelectionVisible(t *testing.T) {
	m := newTestModel(t, 80, 14, "ok")
	typeText(m, "/")
	win := m.paletteWindow()
	if win >= len(m.paletteCommands()) {
		t.Skipf("窗口 %d 行放得下全部命令", win)
	}
	for i := 0; i < len(m.paletteCommands())-1; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if !strings.Contains(ansi.Strip(m.View()), "› /exit") {
		t.Fatal("滚动后选中的 /exit 应可见")
	}
	if n := len(strings.Split(m.View(), "\n")); n != 14 {
		t.Fatalf("渲染 %d 行，想要 14", n)
	}
}
