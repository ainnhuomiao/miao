package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const todoArgs5 = `{"todos":[{"content":"读代码","status":"completed"},{"content":"设计","status":"completed"},{"content":"实现","status":"completed"},{"content":"写测试","status":"in_progress"},{"content":"提交","status":"pending"}]}`

func withTodos(m *Model, args string) {
	m.messages = append(m.messages, toolMsg("todo_write", args, "ok", true, false))
	m.layout()
}

func TestTodoActivityRow(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	withTodos(m, todoArgs5)
	v := m.activityView()
	if !strings.Contains(v, "☐ 3/5 写测试") || !strings.Contains(v, "工具 1") {
		t.Fatalf("工具行应显示 ☐ 3/5 写测试：%q", v)
	}
	// 有工具正在执行时让位给工具
	m.messages = append(m.messages, toolMsg("run_command", `{"command":"go test"}`, "", false, false))
	if v := m.activityView(); strings.Contains(v, "☐") {
		t.Fatalf("工具执行中不应显示清单：%q", v)
	}
	// 无进行中则显示下一项待办
	m.messages = m.messages[:len(m.messages)-1]
	withTodos(m, `{"todos":[{"content":"a","status":"completed"},{"content":"下一步","status":"pending"}]}`)
	if v := m.activityView(); !strings.Contains(v, "☐ 1/2 下一步") {
		t.Fatalf("应显示下一项待办：%q", v)
	}
	withTodos(m, `{"todos":[{"content":"a","status":"completed"}]}`)
	if v := m.activityView(); !strings.Contains(v, "☑ 1/1 完成") {
		t.Fatalf("全部完成应显示 ☑：%q", v)
	}
	m.messages = append(m.messages, toolMsg("read_file", `{"path":"x"}`, "x", true, false))
	if v := m.activityView(); strings.Contains(v, "☑") {
		t.Fatalf("全部完成后再有工具应回到普通显示：%q", v)
	}
}

func TestTodoCtrlTTogglesPanel(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.todoPanel || m.todoPanelView() != "" {
		t.Fatal("无清单时 Ctrl+T 不应展开")
	}
	withTodos(m, todoArgs5)
	h := m.viewport.Height
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	p := m.todoPanelView()
	for _, want := range []string{"☑ 读代码", "▶ 写测试", "☐ 提交"} {
		if !strings.Contains(p, want) {
			t.Fatalf("面板缺 %q：%q", want, p)
		}
	}
	if m.viewport.Height != h-5 {
		t.Fatalf("面板应占 5 行：%d -> %d", h, m.viewport.Height)
	}
	if n := len(strings.Split(m.View(), "\n")); n != 30 {
		t.Fatalf("展开后整屏 %d 行，应为 30", n)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.todoPanelView() != "" {
		t.Fatal("再按 Ctrl+T 应收起")
	}
}

func TestTodoPanelOverflow(t *testing.T) {
	m := newTestModel(t, 80, 40, "ok")
	var items []string
	for i := 0; i < 15; i++ {
		items = append(items, `{"content":"任务","status":"pending"}`)
	}
	withTodos(m, `{"todos":[`+strings.Join(items, ",")+`]}`)
	m.todoPanel = true
	lines := m.todoPanelLines()
	if len(lines) != todoPanelMaxLines || !strings.Contains(lines[len(lines)-1], "还有 6 项") {
		t.Fatalf("溢出应折叠：%v", lines)
	}
}

func TestTodoClearedOnNew(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	withTodos(m, todoArgs5)
	m.todoPanel = true
	m.runCommand("/new")
	if len(m.currentTodos()) != 0 || m.todoPanelView() != "" || strings.Contains(m.activityView(), "☐") {
		t.Fatal("/new 后清单应清空")
	}
}

func TestTodoNarrowWidth(t *testing.T) {
	long := strings.Repeat("很长的任务描述", 20)
	for _, w := range []int{40, 80} {
		m := newTestModel(t, w, 24, "ok")
		withTodos(m, `{"todos":[{"content":"`+long+`","status":"in_progress"}]}`)
		m.todoPanel = true
		m.layout()
		if lw := lipgloss.Width(m.activityView()); lw > w {
			t.Errorf("宽 %d：工具行宽 %d 超出", w, lw)
		}
		if strings.Contains(m.activityView(), "\n") {
			t.Errorf("宽 %d：工具行换行了", w)
		}
		for _, l := range strings.Split(m.todoPanelView(), "\n") {
			if lipgloss.Width(l) > w {
				t.Errorf("宽 %d：面板行宽 %d 超出", w, lipgloss.Width(l))
			}
		}
		if n := len(strings.Split(m.View(), "\n")); n != 24 {
			t.Errorf("宽 %d：整屏 %d 行", w, n)
		}
	}
}

func TestActivityThreeStates(t *testing.T) {
	m := newTestModel(t, 80, 24, "ok")
	m.streaming = true
	if v := m.activityView(); !strings.Contains(v, "工具待命") || !strings.Contains(v, "esc 中断") {
		t.Fatalf("空闲应为待命 + 右侧思考：%q", v)
	}
	withTodos(m, todoArgs5)
	if v := m.activityView(); !strings.Contains(v, "☐ 3/5 写测试") || !strings.Contains(v, "esc 中断") {
		t.Fatalf("应显示清单进度 + 右侧思考：%q", v)
	}
	run := toolMsg("run_command", `{"command":"go test ./..."}`, "", false, false)
	run.toolLive = "ok  miao/internal/tui  0.1s"
	m.messages = append(m.messages, run)
	v := m.activityView()
	if !strings.Contains(v, "▎") || !strings.Contains(v, "ok  miao/internal/tui") || strings.Contains(v, "☐") {
		t.Fatalf("运行中应显示实时输出且隐藏清单：%q", v)
	}
	if !strings.Contains(v, "工具 2") {
		t.Fatalf("右侧工具计数应保留：%q", v)
	}
	for _, w := range []int{40, 80} {
		m.width = w
		m.layout()
		m.messages[len(m.messages)-1].toolLive = strings.Repeat("长输出", 60)
		if lw := lipgloss.Width(m.activityView()); lw > w {
			t.Errorf("宽 %d：实时输出行宽 %d 超出", w, lw)
		}
	}
}
