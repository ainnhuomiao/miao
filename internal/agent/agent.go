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
	// EventUsage 在每次模型请求结束后推送本次用量（服务端返回时）。
	EventUsage
	// EventToolProgress 推送工具运行中的最新一行输出（放在 ToolResult）。
	EventToolProgress
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
	Usage      *llm.Usage
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

	agentsMD bool // 启动时是否加载了 AGENTS.md
	perm     permissions
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
		if result.Usage != nil {
			if err := onEvent(Event{Kind: EventUsage, Usage: result.Usage}); err != nil {
				rollback()
				return err
			}
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

		toolMsgs, err := runToolCalls(ctx, result.ToolCalls, a.tools.Call, onEvent)
		if err != nil {
			rollback()
			return err
		}
		a.history = append(a.history, toolMsgs...)
	}
	rollback()
	return fmt.Errorf("工具调用超过上限 %d 轮，已停止", maxToolRounds)
}

func (a *Agent) requestMessages() []llm.Message {
	out := make([]llm.Message, 0, len(a.history)+1)
	if a.system != "" {
		out = append(out, llm.Message{Role: llm.RoleSystem, Content: a.systemPrompt()})
	}
	out = append(out, a.history...)
	return out
}

// SetApprover 把审批回调交给工具注册表（例如 TUI 确认卡）。
func (a *Agent) SetApprover(fn tools.Approver) {
	if a.tools != nil {
		a.tools.SetApprover(a.wrapApprover(fn))
	}
}

// Reset 清空对话历史，开启一个新会话。
func (a *Agent) Reset() {
	a.history = a.history[:0]
	a.ClearSessionAllows()
}

// History 返回历史记录的副本。
func (a *Agent) History() []llm.Message {
	out := make([]llm.Message, len(a.history))
	copy(out, a.history)
	return out
}

// SetHistory 用给定消息替换对话历史（例如从会话文件恢复）。
func (a *Agent) SetHistory(msgs []llm.Message) {
	a.history = make([]llm.Message, len(msgs))
	copy(a.history, msgs)
}

// Workspace 返回工具工作区根目录；没有工具时为空。
func (a *Agent) Workspace() string {
	if a == nil || a.tools == nil {
		return ""
	}
	return a.tools.Workspace()
}
