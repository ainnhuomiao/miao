// Package agent 管理一段只存在于内存中的对话。
// 第一版还没有工具：它只负责把历史发给模型，再记住完整的回答。
package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ainnhuomiao/miao/internal/llm"
)

// systemPrompt 是发给模型的角色说明；用户在终端里看不到这条消息。
// const 表示常量，程序运行时不能修改它。
const systemPrompt = "你是 miao，一个终端对话助手。使用用户的语言回答。"

// Agent 把相关状态放在一起，而不是放进包级全局变量。
// 小写字段只允许本包访问，其他包通过 New、Send、Reset 使用它。
// 一个 Agent 由终端循环依次调用，不支持同时发送多条消息。
type Agent struct {
	client  llm.Client    // 负责与模型通信；接口允许测试时换成假客户端。
	history []llm.Message // [] 表示切片，这里保存按时间排列的消息。
}

// New 创建并返回一个 Agent。*Agent 是指针，调用方操作的是同一份状态。
func New(client llm.Client) *Agent {
	a := &Agent{client: client}
	a.Reset()
	return a
}

// Reset 清空用户对话，保留最前面的系统说明。
// (a *Agent) 是方法接收者，表示这个函数属于 Agent，且可以修改它。
func (a *Agent) Reset() {
	a.history = []llm.Message{{Role: "system", Content: systemPrompt}}
}

// Send 请求一次回答。onText 会在每次收到一小段文字时被调用。
// context 传递取消信号；error 为 nil 表示成功，非 nil 表示失败。
// 失败时不记录本轮，已经显示的部分文字不会成为下一次请求的历史。
func (a *Agent) Send(ctx context.Context, input string, onText func(string) error) error {
	if strings.TrimSpace(input) == "" {
		return fmt.Errorf("消息不能为空")
	}

	// 先复制历史，构造本次请求。请求成功前不改变正式历史。
	// make 创建切片；copy 复制已有元素；append 在末尾添加消息。
	messages := make([]llm.Message, len(a.history), len(a.history)+1)
	copy(messages, a.history)
	messages = append(messages, llm.Message{Role: "user", Content: input})

	answer, err := a.client.Chat(ctx, messages, onText)
	if err != nil {
		return err
	}

	// 只有完整回答才一起保存：用户消息和助手消息总是成对加入。
	a.history = append(messages, llm.Message{Role: "assistant", Content: answer})
	return nil
}
