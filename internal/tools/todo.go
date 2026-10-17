package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// 任务清单状态。
const (
	TodoPending    = "pending"
	TodoInProgress = "in_progress"
	TodoCompleted  = "completed"
)

// MaxTodos 是单次 todo_write 允许的最多条目数。
const MaxTodos = 50

// TodoItem 是任务清单中的一项。
type TodoItem struct {
	ID      string `json:"id,omitempty"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// TodoStore 保存当前会话的任务清单（并发安全）。
type TodoStore struct {
	mu       sync.Mutex
	items    []TodoItem
	onChange func([]TodoItem)
}

// NewTodoStore 创建空清单。
func NewTodoStore() *TodoStore { return &TodoStore{} }

// Set 整体替换清单并触发变更回调。
func (s *TodoStore) Set(items []TodoItem) {
	s.mu.Lock()
	s.items = append([]TodoItem(nil), items...)
	cb := s.onChange
	cp := append([]TodoItem(nil), s.items...)
	s.mu.Unlock()
	if cb != nil {
		cb(cp)
	}
}

// List 返回清单副本。
func (s *TodoStore) List() []TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]TodoItem(nil), s.items...)
}

// OnChange 设置变更回调。
func (s *TodoStore) OnChange(f func([]TodoItem)) {
	s.mu.Lock()
	s.onChange = f
	s.mu.Unlock()
}

// ParseTodos 解析并校验 todo_write 参数。
func ParseTodos(args []byte) ([]TodoItem, error) {
	var in struct {
		Todos []TodoItem `json:"todos"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("参数解析失败: %w", err)
	}
	if len(in.Todos) > MaxTodos {
		return nil, fmt.Errorf("任务过多：%d 项，最多 %d 项", len(in.Todos), MaxTodos)
	}
	inProgress := 0
	for i := range in.Todos {
		t := &in.Todos[i]
		t.Content = strings.TrimSpace(t.Content)
		if t.Content == "" {
			return nil, fmt.Errorf("第 %d 项 content 为空", i+1)
		}
		switch t.Status {
		case TodoPending, TodoCompleted:
		case TodoInProgress:
			inProgress++
		default:
			return nil, fmt.Errorf("第 %d 项 status 无效：%q（应为 pending/in_progress/completed）", i+1, t.Status)
		}
	}
	if inProgress > 1 {
		return nil, fmt.Errorf("同时只能有一项 in_progress，当前 %d 项", inProgress)
	}
	return in.Todos, nil
}

// TodoSummary 返回清单计数摘要。
func TodoSummary(items []TodoItem) string {
	var p, ip, c int
	for _, t := range items {
		switch t.Status {
		case TodoPending:
			p++
		case TodoInProgress:
			ip++
		case TodoCompleted:
			c++
		}
	}
	if len(items) == 0 {
		return "任务清单已清空。"
	}
	return fmt.Sprintf("任务清单已更新：共 %d 项，已完成 %d，进行中 %d，待办 %d。", len(items), c, ip, p)
}

const todoWriteDesc = "维护当前任务的待办清单（每次传入完整清单，整体替换）。" +
	"对复杂的多步任务（3 步及以上），先列出全部步骤；同一时间只把一项标为 in_progress；" +
	"每完成一项立刻标为 completed，再开始下一项。简单的单步任务不要使用。"

// NewTodoWriteTool 返回绑定到 store 的 todo_write 工具。
func NewTodoWriteTool(store *TodoStore) Tool {
	return Tool{
		Def: toolDef("todo_write", todoWriteDesc, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type":        "array",
					"description": "完整任务清单（最多 50 项）",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":      map[string]any{"type": "string", "description": "可选的任务标识"},
							"content": map[string]any{"type": "string", "description": "任务描述"},
							"status": map[string]any{
								"type": "string",
								"enum": []string{TodoPending, TodoInProgress, TodoCompleted},
							},
						},
						"required": []string{"content", "status"},
					},
				},
			},
			"required": []string{"todos"},
		}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			items, err := ParseTodos(args)
			if err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			store.Set(items)
			return Result{Content: TodoSummary(items)}
		},
	}
}

func registerTodoWrite(r *Registry) { r.Register(NewTodoWriteTool(NewTodoStore())) }
