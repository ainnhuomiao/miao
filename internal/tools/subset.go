package tools

import "context"

// Subset 返回只含指定且已启用工具的新注册表（同一工作区、无审批回调）。
// 复制的工具闭包仍引用原注册表做路径约束与审批，因此应在无审批回调的注册表上调用。
func (r *Registry) Subset(names ...string) *Registry {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	out := &Registry{workspace: r.workspace, tools: make(map[string]Tool)}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, n := range r.order {
		if want[n] && r.allowedLocked(n) {
			out.order = append(out.order, n)
			out.tools[n] = r.tools[n]
		}
	}
	return out
}

// Unregister 移除工具；不存在时为 noop。
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return
	}
	delete(r.tools, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i:i], r.order[i+1:]...)
			break
		}
	}
}

// Has 报告工具是否已注册。
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// ReportProgress 向 ctx 里的进度回调推送一行；没有回调时忽略。
func ReportProgress(ctx context.Context, line string) {
	if fn := progressFrom(ctx); fn != nil {
		fn(line)
	}
}
