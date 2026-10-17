package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func toolMsg(name, args, result string, done, isErr bool) chatMessage {
	return chatMessage{role: roleTool, toolName: name, toolArgs: args, toolResult: result,
		done: done, toolError: isErr, toolDur: 8 * time.Millisecond}
}

func TestActivityHiddenWithoutTools(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	if v := m.activityView(); v != "" {
		t.Fatalf("没有工具调用时状态条应为空，得到 %q", v)
	}
	before := len(strings.Split(m.View(), "\n"))
	if before != 30 {
		t.Fatalf("空闲时整屏 %d 行，应为 30（状态条占位）", before)
	}
	m.messages = append(m.messages, toolMsg("list_dir", `{"path":"."}`, "a", true, false))
	m.layout()
	if after := len(strings.Split(m.View(), "\n")); after != before {
		t.Fatalf("出现工具调用后行数从 %d 变成 %d，输入框会跳", before, after)
	}
}

func TestActivityIsOneLineShowingLatest(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.messages = append(m.messages,
		toolMsg("list_dir", `{"path":"."}`, "a\nb", true, false),
		toolMsg("read_file", `{"path":"go.mod"}`, "x", true, false),
		toolMsg("run_command", `{"command":"go version"}`, "go1", true, false),
	)
	v := m.activityView()
	if n := strings.Count(v, "\n") + 1; n != 1 {
		t.Fatalf("状态条应只有 1 行，得到 %d 行：%q", n, v)
	}
	if !strings.Contains(v, "run_command") || strings.Contains(v, "list_dir") {
		t.Fatalf("应只显示最近一次调用：%q", v)
	}
	if !strings.Contains(v, "工具 3") {
		t.Fatalf("右侧应显示累计次数 工具 3：%q", v)
	}
	if !strings.Contains(v, "✓") || !strings.Contains(v, "8ms") {
		t.Fatalf("完成的调用应显示 ✓ 和耗时：%q", v)
	}
}

func TestActivityRunningAndError(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.messages = append(m.messages, toolMsg("run_command", `{"command":"go test ./..."}`, "", false, false))
	v := m.activityView()
	if strings.Contains(v, "✓") || strings.Contains(v, "ms  ") {
		t.Fatalf("正在跑的调用不该显示 ✓ 或耗时：%q", v)
	}
	m.messages[len(m.messages)-1] = toolMsg("read_file", `{"path":"nope"}`, "文件不存在", true, true)
	v = m.activityView()
	if !strings.Contains(v, "✗") || !strings.Contains(v, "文件不存在") {
		t.Fatalf("失败的调用应显示 ✗ 和错误首行：%q", v)
	}
}

func TestActivityFitsWidthAndLayout(t *testing.T) {
	for _, w := range []int{40, 80, 140} {
		m := newTestModel(t, w, 24, "ok")
		m.messages = append(m.messages,
			toolMsg("run_command", `{"command":"`+strings.Repeat("x", 300)+`"}`, "", true, false))
		m.layout()
		if lw := lipgloss.Width(m.activityView()); lw > w {
			t.Errorf("宽 %d：状态条宽 %d 超出", w, lw)
		}
		if n := len(strings.Split(m.View(), "\n")); n != 24 {
			t.Errorf("宽 %d：有状态条时渲染 %d 行，想要 24", w, n)
		}
	}
}
