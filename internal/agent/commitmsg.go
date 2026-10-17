package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ainnhuomiao/miao/internal/llm"
)

const commitPrompt = `你是提交信息助手。根据给出的 git diff 写一条提交信息：
- 第一行是简短的总结（不超过 72 个字符），可以用 feat:/fix:/refactor: 这类前缀
- 如果改动较多，空一行后用几条 "- " 列出要点
- 用中文，只输出提交信息本身，不要代码块、不要解释`

// CommitMessage 让模型根据 diff 写一条提交信息（不写入对话历史）。
func (a *Agent) CommitMessage(ctx context.Context, diff string) (string, error) {
	req := llm.ChatRequest{Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: commitPrompt},
		{Role: llm.RoleUser, Content: diff},
	}}
	res, err := a.client.StreamChat(ctx, req, func(llm.Delta) error { return ctx.Err() })
	if err != nil {
		return "", err
	}
	msg := strings.TrimSpace(res.Content)
	msg = strings.TrimPrefix(msg, "```")
	msg = strings.TrimSuffix(msg, "```")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", fmt.Errorf("模型没有返回提交信息")
	}
	return msg, nil
}
