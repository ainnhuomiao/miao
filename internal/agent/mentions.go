package agent

import "context"

// ListFiles 列出工作区文件，供输入框 @ 补全使用。
func (a *Agent) ListFiles(ctx context.Context, limit int) ([]string, error) {
	if a == nil || a.tools == nil {
		return nil, nil
	}
	return a.tools.ListFiles(ctx, limit)
}

// ExpandMentions 把用户消息里 @引用的文件内容附上，见 tools.Registry.ExpandMentions。
func (a *Agent) ExpandMentions(text string) (string, []string, []string) {
	if a == nil || a.tools == nil {
		return text, nil, nil
	}
	return a.tools.ExpandMentions(text)
}
