// Package tools 提供 Agent 可调用的本地工具。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"agnet/internal/llm"
)

// Result 是工具执行结果。
type Result struct {
	Content string
	IsError bool
}

// Tool 是一个可注册的工具。
type Tool struct {
	Def llm.ToolDef
	Run func(ctx context.Context, args json.RawMessage) Result
}

// Approver 在执行需确认的工具前询问用户。
// allow=false 表示拒绝；err 非 nil 表示取消/中断。
type Approver func(ctx context.Context, toolName, display string, args json.RawMessage) (allow bool, err error)

// Registry 管理工具集合与工作区路径约束。
type Registry struct {
	mu        sync.RWMutex // 保护 tools/order：MCP 工具会在启动后动态注册
	workspace string
	tools     map[string]Tool
	order     []string
	approver  Approver
}

// NewRegistry 创建工具注册表。workspace 必须是绝对路径。
func NewRegistry(workspace string) (*Registry, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("解析工作区路径失败: %w", err)
	}
	return &Registry{
		workspace: abs,
		tools:     make(map[string]Tool),
	}, nil
}

// Workspace 返回锁定的工作区根目录。
func (r *Registry) Workspace() string { return r.workspace }

// SetApprover 设置需确认工具的审批回调；nil 表示名单外一律拒绝。
func (r *Registry) SetApprover(a Approver) { r.approver = a }

// Approver 返回当前审批回调。
func (r *Registry) Approver() Approver { return r.approver }

// Register 注册工具；同名覆盖并保持顺序。
func (r *Registry) Register(t Tool) {
	name := t.Def.Function.Name
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		r.order = append(r.order, name)
	}
	r.tools[name] = t
}

// Definitions 返回发给模型的工具定义列表。
func (r *Registry) Definitions() []llm.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]llm.ToolDef, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name].Def)
	}
	return out
}

// Call 按名称执行工具。
func (r *Registry) Call(ctx context.Context, name string, argsJSON string) Result {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return Result{Content: fmt.Sprintf("未知工具: %s", name), IsError: true}
	}
	raw := json.RawMessage(strings.TrimSpace(argsJSON))
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if !json.Valid(raw) {
		return Result{Content: fmt.Sprintf("参数不是合法 JSON: %s", argsJSON), IsError: true}
	}
	return t.Run(ctx, raw)
}

// ResolvePath 将相对路径锁定在工作区内，拒绝越界。
func (r *Registry) ResolvePath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	cleaned := filepath.Clean(rel)
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("只允许相对工作区的路径")
	}
	full := filepath.Join(r.workspace, cleaned)
	relToRoot, err := filepath.Rel(r.workspace, full)
	if err != nil {
		return "", fmt.Errorf("路径解析失败: %w", err)
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越出工作区: %s", rel)
	}
	return full, nil
}
