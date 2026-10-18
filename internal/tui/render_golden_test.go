package tui

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ainnhuomiao/miao/internal/agent"
)

var updateGolden = flag.Bool("update-golden", false, "重写 testdata 下的渲染金样")

const goldenMarkdown = "## 小结\n\n改动如下：\n\n- 第一项 **加粗**\n- 第二项 `code`\n  - 嵌套\n\n```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n\n结束。"

// bigToolOutput 生成 10–30KB 的确定性工具输出。
func bigToolOutput(i int) string {
	var b strings.Builder
	n := 10*1024 + (i*7919)%(20*1024)
	for line := 0; b.Len() < n; line++ {
		fmt.Fprintf(&b, "file_%03d.go:%d: \tmatch 中文内容 %d\n", i, line, line*i)
	}
	return b.String()
}

// fillHistory 按 用户 → 助手(思考) → 工具 → 助手(markdown) 的轮次堆出 n 条消息。
func fillHistory(m *Model, n int) {
	msgs := m.messages[:0:0]
	msgs = append(msgs, m.messages...)
	for i := 0; len(msgs) < n; i++ {
		msgs = append(msgs, chatMessage{role: roleUser, content: fmt.Sprintf("第 %d 个问题：帮我看看这个函数为什么慢？\twith tab and a long line that should wrap around the column width nicely", i), done: true})
		msgs = append(msgs, chatMessage{role: roleAssistant, content: "先看一下代码。", reasoning: fmt.Sprintf("用户问第 %d 个问题，我需要读文件。", i), done: true})
		switch i % 3 {
		case 0:
			msgs = append(msgs, chatMessage{role: roleTool, toolName: "read_file", toolArgs: `{"path":"main.go"}`, toolResult: bigToolOutput(i), toolDur: 12 * time.Millisecond, done: true})
		case 1:
			msgs = append(msgs, chatMessage{role: roleTool, toolName: "run_command", toolArgs: `{"command":"go test ./..."}`, toolResult: "exit 1\nFAIL", toolError: true, toolDur: 3 * time.Second, done: true})
		default:
			msgs = append(msgs, chatMessage{role: roleTool, toolName: "grep", toolArgs: `{"pattern":"foo"}`, toolResult: bigToolOutput(i), toolDur: 40 * time.Millisecond, done: true})
		}
		msgs = append(msgs, chatMessage{role: roleAssistant, content: goldenMarkdown + fmt.Sprintf("\n\n第 %d 轮。", i), done: true})
		if i%10 == 4 {
			msgs = append(msgs, chatMessage{role: roleNotice, content: "已压缩上下文\n第二行", done: true})
		}
		if i%17 == 9 {
			msgs = append(msgs, chatMessage{role: roleError, content: "请求失败: boom", done: true})
		}
	}
	m.messages = msgs
}

// startMidStream 追加一轮进行中的回答：已完成的工具 + 正在流式的助手文本。
func startMidStream(m *Model) {
	m.messages = append(m.messages,
		chatMessage{role: roleUser, content: "继续", done: true},
		chatMessage{role: roleAssistant, reasoning: "想一想", done: true},
		chatMessage{role: roleTool, toolName: "list_dir", toolArgs: `{"path":"."}`, toolResult: "a\nb\nc", done: true, toolDur: time.Millisecond},
		chatMessage{role: roleTool, toolName: "run_command", toolArgs: `{"command":"make"}`},
		chatMessage{role: roleAssistant, content: "正在输出的 **markdown** 文本，\n- 列表一\n- 列表二 还没写完"},
	)
}

func newBenchModel(tb testing.TB, width, height int) *Model {
	tb.Setenv("XDG_DATA_HOME", tb.TempDir())
	ag := agent.New(&fakeClient{}, "测试助手", nil)
	m := New(ag, "test-model")
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

// fullViewport 返回视口里的全部内容（不只是可见窗口）。
func fullViewport(m *Model) string {
	v := m.viewport
	v.Height = max(1, v.TotalLineCount())
	v.GotoTop()
	return v.View()
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "render", name+".golden.gz")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, _ = zw.Write([]byte(got))
		_ = zw.Close()
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读金样失败（用 -update-golden 生成）：%v", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	want, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		wl, gl := strings.Split(string(want), "\n"), strings.Split(got, "\n")
		for i := 0; i < min(len(wl), len(gl)); i++ {
			if wl[i] != gl[i] {
				t.Fatalf("%s 第 %d 行不同:\nwant %q\ngot  %q", name, i+1, wl[i], gl[i])
			}
		}
		t.Fatalf("%s 行数不同: want %d got %d", name, len(wl), len(gl))
	}
}

func TestRenderGolden(t *testing.T) {
	for _, w := range []int{80, 140} {
		for _, reasoning := range []bool{false, true} {
			name := fmt.Sprintf("w%d_reason%v", w, reasoning)
			t.Run(name, func(t *testing.T) {
				m := newBenchModel(t, w, 40)
				checkGolden(t, name+"_empty_view", m.View())
				fillHistory(m, 200)
				m.showReasoning = reasoning
				m.refreshViewport(true)
				checkGolden(t, name+"_done_full", fullViewport(m))
				checkGolden(t, name+"_done_view", m.View())

				startMidStream(m)
				m.refreshViewport(true)
				checkGolden(t, name+"_stream_full", fullViewport(m))

				// 流式追加若干 delta，冲刷后与一次性渲染一致
				for i := 0; i < 20; i++ {
					m.Update(streamEventMsg{kind: agent.EventDelta, content: fmt.Sprintf(" 片段%d", i)})
				}
				m.refreshViewport(false)
				checkGolden(t, name+"_stream2_full", fullViewport(m))

				// 收尾：工具结束、助手完成
				m.messages[len(m.messages)-2].done = true
				m.messages[len(m.messages)-2].toolResult = "ok"
				m.messages[len(m.messages)-1].done = true
				m.refreshViewport(false)
				checkGolden(t, name+"_end_full", fullViewport(m))

				// 切换思考显示、改窗口尺寸后与金样一致（缓存必须失效）
				m.showReasoning = !reasoning
				m.refreshViewport(false)
				checkGolden(t, name+"_toggled_full", fullViewport(m))
				m.showReasoning = reasoning
				m.Update(tea.WindowSizeMsg{Width: w + 7, Height: 30})
				m.Update(tea.WindowSizeMsg{Width: w, Height: 40})
				checkGolden(t, name+"_end_full", fullViewport(m))
			})
		}
	}
}

func benchStream(b *testing.B, n int) {
	m := newBenchModel(b, 120, 40)
	fillHistory(m, n)
	m.messages = append(m.messages, chatMessage{role: roleUser, content: "继续", done: true}, chatMessage{role: roleAssistant})
	m.streaming = true
	m.events = make(chan streamEventMsg)
	m.refreshViewport(true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%100 == 0 { // 回答长度保持在 ~3KB，不随 b.N 无限增长
			m.messages[len(m.messages)-1].content = ""
		}
		m.Update(streamEventMsg{kind: agent.EventDelta, content: "一些流式输出的文字 some words "})
		_ = m.View()
	}
}

func BenchmarkStreamShort(b *testing.B) { benchStream(b, 4) }
func BenchmarkStreamLong(b *testing.B)  { benchStream(b, 200) }

// BenchmarkStreamLongFlush 每个 chunk 都强制刷新视口（相当于合并刷新关闭时的最坏情况）。
func BenchmarkStreamLongFlush(b *testing.B) {
	m := newBenchModel(b, 120, 40)
	fillHistory(m, 200)
	m.messages = append(m.messages, chatMessage{role: roleUser, content: "继续", done: true}, chatMessage{role: roleAssistant})
	m.streaming = true
	m.events = make(chan streamEventMsg)
	m.refreshViewport(true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%100 == 0 { // 回答长度保持在 ~3KB，不随 b.N 无限增长
			m.messages[len(m.messages)-1].content = ""
		}
		m.Update(streamEventMsg{kind: agent.EventDelta, content: "一些流式输出的文字 some words "})
		m.refreshViewport(false)
		_ = m.View()
	}
}

func BenchmarkView200(b *testing.B) {
	m := newBenchModel(b, 120, 40)
	fillHistory(m, 200)
	m.refreshViewport(true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}
