package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"agnet/internal/llm"
	"agnet/internal/tools"
)

// maxParallelTools 是同一批只读工具的最大并发数。
const maxParallelTools = 8

// interruptedResult 是被取消（Esc 中断）的工具调用的占位结果。
const interruptedResult = "已中断"

// readOnlyTools 是可以并发执行的只读工具；不在此集合内的（写、命令、未知/MCP 工具）一律串行。
var readOnlyTools = map[string]bool{
	"read_file": true,
	"list_dir":  true,
	"grep":      true,
	"glob":      true,
	"web_fetch": true,
}

// isReadOnlyTool 报告工具是否可并发执行。
func isReadOnlyTool(name string) bool { return readOnlyTools[name] }

// toolCaller 抽象工具执行，便于测试。
type toolCaller func(ctx context.Context, name, args string) tools.Result

// safeCall 执行工具，取消时返回“已中断”，panic 恢复为错误结果。
func safeCall(ctx context.Context, call toolCaller, name, args string) (res tools.Result) {
	if ctx.Err() != nil {
		return tools.Result{Content: interruptedResult, IsError: true}
	}
	defer func() {
		if p := recover(); p != nil {
			res = tools.Result{Content: fmt.Sprintf("工具 %s 发生 panic: %v", name, p), IsError: true}
		}
	}()
	res = call(ctx, name, args)
	if ctx.Err() != nil && res.IsError {
		res.Content = interruptedResult + ": " + res.Content
	}
	return res
}

// runToolCalls 按顺序执行 tool_calls：连续的只读调用组成一批并发执行，其余逐个串行。
// 返回的 tool 消息与 calls 顺序一一对应。onEvent 的调用被互斥保护，不会并发进入。
// onEvent 返回错误时停止后续调用并返回该错误（已返回的消息仍覆盖全部 id）。
func runToolCalls(ctx context.Context, calls []llm.ToolCall, call toolCaller, onEvent EventHandler) ([]llm.Message, error) {
	out := make([]llm.Message, len(calls))
	var evMu sync.Mutex
	var evErr error
	emit := func(e Event) {
		evMu.Lock()
		defer evMu.Unlock()
		if evErr != nil {
			return
		}
		evErr = onEvent(e)
	}
	failed := func() bool {
		evMu.Lock()
		defer evMu.Unlock()
		return evErr != nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	exec := func(i int) {
		tc := calls[i]
		name, args := tc.Function.Name, tc.Function.Arguments
		start := time.Now()
		pctx := tools.WithProgress(runCtx, func(line string) {
			emit(Event{Kind: EventToolProgress, ToolCallID: tc.ID, ToolName: name, ToolResult: line})
		})
		res := safeCall(pctx, call, name, args)
		emit(Event{
			Kind:       EventToolEnd,
			ToolCallID: tc.ID,
			ToolName:   name,
			ToolArgs:   args,
			ToolResult: res.Content,
			ToolError:  res.IsError,
			Duration:   time.Since(start),
		})
		out[i] = llm.Message{Role: llm.RoleTool, Content: res.Content, ToolCallID: tc.ID, Name: name}
	}
	startEv := func(i int) {
		tc := calls[i]
		emit(Event{Kind: EventToolStart, ToolCallID: tc.ID, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments})
	}

	for i := 0; i < len(calls); {
		if failed() {
			cancel()
		}
		if !isReadOnlyTool(calls[i].Function.Name) {
			startEv(i)
			exec(i)
			i++
			continue
		}
		j := i
		for j < len(calls) && isReadOnlyTool(calls[j].Function.Name) {
			j++
		}
		for k := i; k < j; k++ {
			startEv(k)
		}
		sem := make(chan struct{}, maxParallelTools)
		var wg sync.WaitGroup
		for k := i; k < j; k++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(k int) {
				defer wg.Done()
				defer func() { <-sem }()
				exec(k)
			}(k)
		}
		wg.Wait()
		i = j
	}
	return out, evErr
}
