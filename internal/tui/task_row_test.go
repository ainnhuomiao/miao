package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"agnet/internal/subagent"
)

func doneTask(desc, result string, isErr bool, d time.Duration) chatMessage {
	m := toolMsg("task", `{"description":"`+desc+`","prompt":"请调研配置与环境变量"}`, result, true, isErr)
	m.toolDur = d
	return m
}

const taskResult32 = "## 结论\n" + "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\nq\nr\ns\nt\nu\nv\nw\nx\ny\nz\n1\n2\n3\n4\n5"

func TestTaskRowFinished(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	got := stripANSI(m.toolCardLine(doneTask("项目概览", taskResult32, false, 15116*time.Millisecond), 80))
	if got != "⑂ 项目概览 · ✓ 15.1s · 32 行" {
		t.Fatalf("完成行格式不对：%q", got)
	}
	if strings.Contains(got, "##") || strings.Contains(got, "prompt") || strings.Contains(got, "…") {
		t.Fatalf("不应泄露结果/参数：%q", got)
	}
}

func TestTaskRowFailed(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	got := stripANSI(m.toolCardLine(doneTask("项目概览", "超时\n详情", true, 3200*time.Millisecond), 80))
	if got != "⑂ 项目概览 · ✗ 3.2s · 超时" {
		t.Fatalf("失败行格式不对：%q", got)
	}
}

func TestTaskRowWidthCJK(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	long := strings.Repeat("配置与环境变量", 10)
	for _, w := range []int{12, 20, 30, 40} {
		got := m.toolCardLine(doneTask(long, taskResult32, false, time.Second), w)
		if lipgloss.Width(got) > w {
			t.Fatalf("宽 %d 超出：%d %q", w, lipgloss.Width(got), stripANSI(got))
		}
	}
}

func TestTaskLiveRowClearedAfterDone(t *testing.T) {
	setupSubMgr(t, subagent.Config{Enabled: true, Max: 3})
	m := newTestModel(t, 100, 30, "ok")
	m.messages = append(m.messages, taskMsg("项目概览", "读取 a.go"))
	if v := stripANSI(m.activityView()); !strings.Contains(v, "⑂ 1/3 子代理") {
		t.Fatalf("运行中应显示子代理状态：%q", v)
	}
	m.messages[len(m.messages)-1] = doneTask("项目概览", taskResult32, false, 29435*time.Millisecond)
	v := stripANSI(m.activityView())
	if strings.Contains(v, "子代理 ·") || strings.Contains(v, "description") || strings.Contains(v, "prompt") || strings.Contains(v, "##") {
		t.Fatalf("完成后不应残留实时行或原始参数：%q", v)
	}
	if !strings.Contains(v, "⑂ 项目概览 · ✓ 29.4s · 32 行") {
		t.Fatalf("完成后工具行应为摘要格式：%q", v)
	}
	if lipgloss.Width(m.activityView()) > m.columnWidth()+m.columnMargin() {
		t.Fatalf("工具行超宽")
	}
}

func TestTaskDetailsInCtrlT(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.messages = append(m.messages, doneTask("项目概览", taskResult32, false, time.Second))
	m.toggleTodoPanel()
	v := stripANSI(m.todoPanelView())
	if !strings.Contains(v, "请调研配置与环境变量") || !strings.Contains(v, "## 结论") {
		t.Fatalf("Ctrl+T 应显示 prompt 与结果：%q", v)
	}
}
