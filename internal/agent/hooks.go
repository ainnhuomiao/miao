package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ainnhuomiao/miao/internal/hooks"
	"github.com/ainnhuomiao/miao/internal/tools"
)

// maxStopHookRetries 是回合结束 hook 失败后让模型继续修的最多次数，防止来回死循环。
const maxStopHookRetries = 2

// SetHooks 设置 hook；nil 表示不用 hook。
func (a *Agent) SetHooks(r *hooks.Runner) { a.hooks = r }

// Hooks 返回当前 hook（可能为 nil）。
func (a *Agent) Hooks() *hooks.Runner { return a.hooks }

// hookApprove 用原始审批回调（不经过权限模式和会话白名单）询问是否信任项目级 hook。
func (a *Agent) hookApprove() hooks.ApproveFunc {
	fn := a.rawApprover
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, display string) (bool, error) {
		return fn(ctx, "hook", display, json.RawMessage(`{}`))
	}
}

// HookApprover 供 hooks.Load 使用：每次需要时取当前的审批回调。
func (a *Agent) HookApprover() func() hooks.ApproveFunc { return a.hookApprove }

// callTool 执行工具，前后各跑一次匹配的 hook。
func (a *Agent) callTool(ctx context.Context, name, args string) tools.Result {
	if !a.hooks.Empty() {
		if pre := a.hooks.PreTool(ctx, name, args); pre.Failed {
			return tools.Result{Content: "工具调用前的 hook 阻止了这次调用：\n" + pre.Output, IsError: true}
		}
	}
	res := a.tools.Call(ctx, name, args)
	if !a.hooks.Empty() && ctx.Err() == nil {
		if post := a.hooks.PostTool(ctx, name, args, res.Content); post.Failed {
			res.Content += "\n\n[工具调用后的 hook 检查未通过]\n" + post.Output
		}
	}
	return res
}

// runStopHook 在模型给出最终回复时运行；失败时返回要交给模型的消息。
func (a *Agent) runStopHook(ctx context.Context, round int, onEvent EventHandler) (string, bool) {
	if a.hooks.Empty() || ctx.Err() != nil {
		return "", false
	}
	id := fmt.Sprintf("hook-stop-%d", round)
	res := a.hooks.Stop(ctx)
	if res.Ran == 0 || !res.Failed {
		return "", false
	}
	_ = onEvent(Event{Kind: EventToolStart, ToolCallID: id, ToolName: "hook", ToolArgs: `{"event":"stop"}`})
	_ = onEvent(Event{Kind: EventToolEnd, ToolCallID: id, ToolName: "hook", ToolResult: res.Output, ToolError: true})
	return "[回合结束的 hook 检查未通过，请根据输出修复后再结束]\n" + res.Output, true
}
