// Package agent 实现多轮对话 Agent：维护历史、调用模型、执行工具。
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ainnhuomiao/miao/internal/hooks"
	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
)

// DefaultMaxToolRounds 是工具循环的默认上限；可用 MIAO_MAX_TOOL_ROUNDS 或 SetMaxToolRounds 覆盖。
const DefaultMaxToolRounds = 32

// ErrMaxToolRounds 表示本轮工具调用次数用尽。历史会保留，发送「继续」即可接着干。
var ErrMaxToolRounds = errors.New("工具调用超过上限")

// MaxToolRoundsError 带上限数字，便于提示。
type MaxToolRoundsError struct{ Limit int }

func (e *MaxToolRoundsError) Error() string {
	return fmt.Sprintf("工具调用超过上限 %d 轮。本轮对话和文件改动都还在，发送「继续」即可接着干。", e.Limit)
}

func (e *MaxToolRoundsError) Is(target error) bool { return target == ErrMaxToolRounds }

// DefaultSystemPrompt 是每次请求都会放在最前面的系统提示词。
const DefaultSystemPrompt = `你是一个运行在用户终端里的个人 AI 助手，名字叫 miao。
回答默认使用简体中文，除非用户要求其他语言。
保持回答简洁、准确、可操作；涉及代码时给出可以直接运行的示例。
不确定的事情要直说，不要编造。
可用工具：
- read_file / list_dir / grep / glob / repo_map：查看工作区（不跟随指向工作区外的符号链接，不读 .env 等敏感文件）；repo_map 生成 Go 符号索引
- write_file / edit_file：修改文件，需用户确认
- run_command：在工作区执行命令（无 shell）。白名单内的只读用法直接执行；写文件、执行任意程序、访问工作区外路径都要确认。go test / go build 会编译并运行仓库里的代码
- web_fetch / web_search：抓取公开网页、搜索资料
- todo_write：任务清单；task：派出只读子代理做调研；command_output：查看或结束后台命令
需要文件内容或命令输出时先调用工具，再基于结果回答。`

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
	// EventSubagentUsage 推送子代理一次模型请求的用量（计入会话用量，不影响上下文大小）。
	EventSubagentUsage
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
// 历史由 mu 保护：TUI 在 UI 线程读 History，Send 在后台 goroutine 写。
// 不要在持锁时调用模型或执行工具，以免和审批回调死锁。
type Agent struct {
	mu      sync.Mutex
	client  llm.Client
	system  string
	tools   *tools.Registry
	history []llm.Message

	agentsMD      bool // 启动时是否加载了 AGENTS.md
	perm          permissions
	maxToolRounds int

	hooks       *hooks.Runner  // 用户配置的 hook，可为 nil
	rawApprover tools.Approver // 未经权限包装的审批回调（项目 hook 信任确认用）

	pendingImages []llm.Image // 下一条用户消息附带的图片，见 images.go
	fallbackModel string
}

// New 创建 Agent。registry 可为 nil（无工具）。
func New(client llm.Client, system string, registry *tools.Registry) *Agent {
	return &Agent{client: client, system: system, tools: registry, maxToolRounds: DefaultMaxToolRounds}
}

// SetFallbackModel 设置主模型失败且尚未产生任何输出时改用的备用模型。
func (a *Agent) SetFallbackModel(name string) {
	a.mu.Lock()
	a.fallbackModel = strings.TrimSpace(name)
	a.mu.Unlock()
}

// SetMaxToolRounds 设置工具循环上限；小于 1 时视为 1。
func (a *Agent) SetMaxToolRounds(n int) {
	if n < 1 {
		n = 1
	}
	a.mu.Lock()
	a.maxToolRounds = n
	a.mu.Unlock()
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

	a.clearRoundAllows()
	a.mu.Lock()
	checkpoint := len(a.history)
	a.history = append(a.history, llm.Message{
		Role:    llm.RoleUser,
		Content: userInput,
		Images:  a.takeImagesLocked(),
	})
	limit := a.maxToolRounds
	if limit < 1 {
		limit = DefaultMaxToolRounds
	}
	a.mu.Unlock()

	rollback := func() {
		a.mu.Lock()
		a.history = a.history[:checkpoint]
		a.mu.Unlock()
	}
	stopRetries := 0

	for round := 0; round < limit; round++ {
		var defs []llm.ToolDef
		if a.tools != nil {
			defs = a.tools.Definitions() // 每次请求重新取：/subagents 开关、MCP 工具即时生效
		}
		messages := a.requestMessages()
		result, err := a.streamWithFallback(ctx, llm.ChatRequest{
			Messages: messages,
			Tools:    defs,
		}, onEvent)
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
		a.mu.Lock()
		a.history = append(a.history, assistant)
		a.mu.Unlock()

		if len(result.ToolCalls) == 0 {
			if stopRetries < maxStopHookRetries {
				if msg, failed := a.runStopHook(ctx, round, onEvent); failed {
					stopRetries++
					a.mu.Lock()
					a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: msg})
					a.mu.Unlock()
					continue
				}
			}
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

		toolMsgs, err := runToolCalls(ctx, result.ToolCalls, a.callTool, onEvent)
		if err != nil {
			rollback()
			return err
		}
		a.mu.Lock()
		a.history = append(a.history, toolMsgs...)
		a.mu.Unlock()
	}
	return &MaxToolRoundsError{Limit: limit}
}

func (a *Agent) streamWithFallback(ctx context.Context, req llm.ChatRequest, onEvent EventHandler) (llm.ChatResult, error) {
	n := 0
	result, err := a.client.StreamChat(ctx, req, func(d llm.Delta) error {
		if d.Content != "" || d.Reasoning != "" {
			n++
		}
		return onEvent(Event{Kind: EventDelta, Delta: d})
	})
	if err == nil || n > 0 || ctx.Err() != nil {
		return result, err
	}
	a.mu.Lock()
	fb := a.fallbackModel
	a.mu.Unlock()
	if fb == "" || fb == a.Model() {
		return result, err
	}
	slog.Warn("主模型失败，改用备用模型", "fallback", fb, "err", err)
	orig := a.Model()
	a.SetModel(fb)
	res2, err2 := a.client.StreamChat(ctx, req, func(d llm.Delta) error {
		return onEvent(Event{Kind: EventDelta, Delta: d})
	})
	if err2 != nil {
		a.SetModel(orig)
		return result, err
	}
	return res2, nil
}

func (a *Agent) requestMessages() []llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]llm.Message, 0, len(a.history)+1)
	if a.system != "" {
		out = append(out, llm.Message{Role: llm.RoleSystem, Content: a.systemPrompt()})
	}
	out = append(out, a.history...)
	return a.visionFilter(out)
}

// SetApprover 把审批回调交给工具注册表（例如 TUI 确认卡）。
func (a *Agent) SetApprover(fn tools.Approver) {
	a.rawApprover = fn
	if a.tools != nil {
		a.tools.SetApprover(a.wrapApprover(fn))
	}
}

// Reset 清空对话历史，开启一个新会话。
func (a *Agent) Reset() {
	a.mu.Lock()
	a.history = a.history[:0]
	a.mu.Unlock()
	a.ClearSessionAllows()
}

// History 返回历史记录的副本。
func (a *Agent) History() []llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]llm.Message, len(a.history))
	copy(out, a.history)
	return out
}

// RecordShell 把用户直通执行的 ! 命令及其输出写入历史，供后续提问使用。
func (a *Agent) RecordShell(cmd, output string, runErr error) {
	user := llm.Message{Role: llm.RoleUser, Content: "!" + cmd}
	body := strings.TrimSpace(output)
	if body == "" {
		body = "(无输出)"
	}
	if runErr != nil {
		body += "\n" + runErr.Error()
	}
	asst := llm.Message{Role: llm.RoleAssistant, Content: "命令输出：\n" + body}
	a.mu.Lock()
	a.history = append(a.history, user, asst)
	a.mu.Unlock()
}

// SetHistory 用给定消息替换对话历史（例如从会话文件恢复）。
func (a *Agent) SetHistory(msgs []llm.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
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
