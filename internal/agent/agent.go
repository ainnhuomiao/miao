// Package agent 实现多轮对话 Agent：维护历史、调用模型、执行工具。
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agnet/internal/llm"
	"agnet/internal/tools"
)

const maxToolRounds = 8

// DefaultSystemPrompt 是每次请求都会放在最前面的系统提示词。
const DefaultSystemPrompt = `你是一个运行在用户终端里的个人 AI 助手，名字叫 GoAgent。
回答默认使用简体中文，除非用户要求其他语言。
保持回答简洁、准确、可操作；涉及代码时给出可以直接运行的示例。
不确定的事情要直说，不要编造。
你可以使用工具查看工作区内的文件（read_file、list_dir），并在白名单内执行命令（run_command，无 shell）。需要文件内容或命令输出时先调用工具，再基于结果回答。`

// EventKind 标识一次回调事件类型。
type EventKind int

const (
	EventDelta EventKind = iota
	EventToolStart
	EventToolEnd
)

// Event 是 Agent 向 UI 推送的进度。
type Event struct {
	Kind       EventKind
	Delta      llm.Delta
	ToolCallID string
	ToolName   string
	ToolArgs   string
	ToolResult string
	ToolError  bool
	Duration   time.Duration
}

// EventHandler 处理进度事件；返回错误会中止本轮。
type EventHandler func(Event) error

// Agent 持有对话历史与工具。
//
// 注意：Agent 目前不是并发安全的。
type Agent struct {
	client  llm.Client
	system  string
	tools   *tools.Registry
	history []llm.Message
}

// New 创建 Agent。registry 可为 nil（无工具）。
func New(client llm.Client, system string, registry *tools.Registry) *Agent {
	return &Agent{client: client, system: system, tools: registry}
}

// Send 发送用户输入，驱动模型与工具循环。
// 失败或取消时回滚本轮写入的历史，避免半轮对话残留。
func (a *Agent) Send(ctx context.Context, userInput string, onEvent EventHandler) error {
	if strings.TrimSpace(userInput) == "" {
		return fmt.Errorf("消息不能为空")
	}
	if onEvent == nil {
		onEvent = func(Event) error { return nil }
	}

	checkpoint := len(a.history)
	a.history = append(a.history, llm.Message{
		Role:    llm.RoleUser,
		Content: userInput,
	})

	var defs []llm.ToolDef
	if a.tools != nil {
		defs = a.tools.Definitions()
	}

	rollback := func() {
		a.history = a.history[:checkpoint]
	}

	for round := 0; round < maxToolRounds; round++ {
		messages := a.requestMessages()
		result, err := a.client.StreamChat(ctx, llm.ChatRequest{
			Messages: messages,
			Tools:    defs,
		}, func(d llm.Delta) error {
			return onEvent(Event{Kind: EventDelta, Delta: d})
		})
		if err != nil {
			rollback()
			return err
		}

		assistant := llm.Message{
			Role:      llm.RoleAssistant,
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		}
		a.history = append(a.history, assistant)

		if len(result.ToolCalls) == 0 {
			return nil
		}
		if result.FinishReason == "length" {
			rollback()
			return fmt.Errorf("模型输出因长度截断，已拒绝执行本批工具调用")
		}
		if a.tools == nil {
			rollback()
			return fmt.Errorf("模型请求工具调用，但未注册任何工具")
		}

		for _, tc := range result.ToolCalls {
			name := tc.Function.Name
			args := tc.Function.Arguments
			if err := onEvent(Event{
				Kind:       EventToolStart,
				ToolCallID: tc.ID,
				ToolName:   name,
				ToolArgs:   args,
			}); err != nil {
				rollback()
				return err
			}

			start := time.Now()
			res := a.tools.Call(ctx, name, args)
			dur := time.Since(start)

			if err := onEvent(Event{
				Kind:       EventToolEnd,
				ToolCallID: tc.ID,
				ToolName:   name,
				ToolArgs:   args,
				ToolResult: res.Content,
				ToolError:  res.IsError,
				Duration:   dur,
			}); err != nil {
				rollback()
				return err
			}

			a.history = append(a.history, llm.Message{
				Role:       llm.RoleTool,
				Content:    res.Content,
				ToolCallID: tc.ID,
				Name:       name,
			})
		}
	}
	rollback()
	return fmt.Errorf("工具调用超过上限 %d 轮，已停止", maxToolRounds)
}

func (a *Agent) requestMessages() []llm.Message {
	out := make([]llm.Message, 0, len(a.history)+1)
	if a.system != "" {
		out = append(out, llm.Message{Role: llm.RoleSystem, Content: a.system})
	}
	out = append(out, a.history...)
	return out
}

// Reset 清空对话历史，开启一个新会话。
func (a *Agent) Reset() {
	a.history = a.history[:0]
}

// History 返回历史记录的副本。
func (a *Agent) History() []llm.Message {
	out := make([]llm.Message, len(a.history))
	copy(out, a.history)
	return out
}
