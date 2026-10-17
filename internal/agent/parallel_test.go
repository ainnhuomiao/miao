package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"agnet/internal/llm"
	"agnet/internal/tools"
)

func mkCall(id, name string) llm.ToolCall {
	return llm.ToolCall{ID: id, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: `{}`}}
}

func checkOrder(t *testing.T, calls []llm.ToolCall, msgs []llm.Message) {
	t.Helper()
	if len(msgs) != len(calls) {
		t.Fatalf("消息数 %d != 调用数 %d", len(msgs), len(calls))
	}
	for i, c := range calls {
		if msgs[i].Role != llm.RoleTool || msgs[i].ToolCallID != c.ID || msgs[i].Name != c.Function.Name {
			t.Fatalf("第 %d 条不匹配: %+v", i, msgs[i])
		}
	}
}

func noopEvents(Event) error { return nil }

func TestParallelReadOnlyFast(t *testing.T) {
	calls := []llm.ToolCall{mkCall("a", "read_file"), mkCall("b", "grep"), mkCall("c", "glob")}
	caller := func(ctx context.Context, name, args string) tools.Result {
		time.Sleep(100 * time.Millisecond)
		return tools.Result{Content: "ok:" + name}
	}
	start := time.Now()
	msgs, err := runToolCalls(context.Background(), calls, caller, noopEvents)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 250*time.Millisecond {
		t.Fatalf("并发执行耗时过长: %v", d)
	}
	checkOrder(t, calls, msgs)
	if msgs[1].Content != "ok:grep" {
		t.Fatalf("内容错位: %q", msgs[1].Content)
	}
}

func TestMixedKeepsWriteSerial(t *testing.T) {
	calls := []llm.ToolCall{mkCall("1", "read_file"), mkCall("2", "list_dir"), mkCall("3", "write_file"), mkCall("4", "read_file")}
	var mu sync.Mutex
	var log []string
	var running int
	caller := func(ctx context.Context, name, args string) tools.Result {
		mu.Lock()
		running++
		if name == "write_file" && running != 1 {
			mu.Unlock()
			return tools.Result{Content: "write 未串行", IsError: true}
		}
		log = append(log, "start:"+name)
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		running--
		log = append(log, "end:"+name)
		mu.Unlock()
		return tools.Result{Content: name}
	}
	var events []Event
	msgs, err := runToolCalls(context.Background(), calls, caller, func(e Event) error {
		events = append(events, e) // 互斥保护下调用，无需加锁
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	checkOrder(t, calls, msgs)
	for _, m := range msgs {
		if m.Content == "write 未串行" {
			t.Fatal("write_file 与其他调用并发了")
		}
	}
	// write 必须在前两个 read 结束后开始，最后一个 read 必须在 write 结束后开始。
	idx := func(s string) int {
		for i := len(log) - 1; i >= 0; i-- {
			v := log[i]
			if v == s {
				return i
			}
		}
		return -1
	}
	if idx("start:write_file") < idx("end:list_dir") || idx("start:write_file") < idx("start:list_dir") || idx("start:read_file") < idx("end:write_file") {
		t.Fatalf("顺序异常: %v", log)
	}
	if len(events) != 8 {
		t.Fatalf("事件数 %d != 8", len(events))
	}
}

func TestCancelledYieldsAllResults(t *testing.T) {
	calls := []llm.ToolCall{mkCall("a", "read_file"), mkCall("b", "read_file"), mkCall("c", "run_command"), mkCall("d", "glob")}
	ctx, cancel := context.WithCancel(context.Background())
	caller := func(ctx context.Context, name, args string) tools.Result {
		cancel()
		<-ctx.Done()
		return tools.Result{Content: ctx.Err().Error(), IsError: true}
	}
	msgs, _ := runToolCalls(ctx, calls, caller, noopEvents)
	checkOrder(t, calls, msgs)
	if msgs[2].Content != interruptedResult || msgs[3].Content != interruptedResult {
		t.Fatalf("取消后应为已中断: %q %q", msgs[2].Content, msgs[3].Content)
	}
}

func TestPanicRecovered(t *testing.T) {
	calls := []llm.ToolCall{mkCall("a", "read_file"), mkCall("b", "grep"), mkCall("c", "edit_file")}
	caller := func(ctx context.Context, name, args string) tools.Result {
		if name != "read_file" {
			panic(fmt.Sprintf("boom %s", name))
		}
		return tools.Result{Content: "ok"}
	}
	msgs, err := runToolCalls(context.Background(), calls, caller, noopEvents)
	if err != nil {
		t.Fatal(err)
	}
	checkOrder(t, calls, msgs)
	if msgs[0].Content != "ok" {
		t.Fatalf("got %q", msgs[0].Content)
	}
}

func TestEventErrorStops(t *testing.T) {
	calls := []llm.ToolCall{mkCall("a", "write_file"), mkCall("b", "run_command")}
	ran := 0
	caller := func(ctx context.Context, name, args string) tools.Result { ran++; return tools.Result{Content: "x"} }
	_, err := runToolCalls(context.Background(), calls, caller, func(e Event) error { return context.Canceled })
	if err == nil || ran > 1 {
		t.Fatalf("err=%v ran=%d", err, ran)
	}
}
