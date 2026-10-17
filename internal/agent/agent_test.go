package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agnet/internal/llm"
)

// fakeClient 是 llm.Client 的假实现。
// 因为 Agent 依赖接口而不是具体的 OpenAIClient，测试时可以不联网。
type fakeClient struct {
	reply   string
	err     error
	gotMsgs []llm.Message
}

func (f *fakeClient) Stream(ctx context.Context, msgs []llm.Message, onDelta llm.DeltaHandler) error {
	f.gotMsgs = append([]llm.Message(nil), msgs...)
	if f.err != nil {
		return f.err
	}
	for _, r := range f.reply { // 按 rune 拆分，模拟流式片段
		if err := onDelta(llm.Delta{Content: string(r)}); err != nil {
			return err
		}
	}
	return nil
}

func TestSendRecordsHistory(t *testing.T) {
	fake := &fakeClient{reply: "你好"}
	ag := New(fake, "你是测试助手")

	var got strings.Builder
	err := ag.Send(context.Background(), "hi", func(d llm.Delta) error {
		got.WriteString(d.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("Send 返回错误: %v", err)
	}

	if want := "你好"; got.String() != want {
		t.Errorf("流式输出 = %q, 想要 %q", got.String(), want)
	}

	// 发给模型的消息应是 system + user
	if len(fake.gotMsgs) != 2 {
		t.Fatalf("请求消息数 = %d, 想要 2", len(fake.gotMsgs))
	}
	if fake.gotMsgs[0].Role != llm.RoleSystem || fake.gotMsgs[0].Content != "你是测试助手" {
		t.Errorf("第一条应是 system 消息，实际为 %+v", fake.gotMsgs[0])
	}
	if fake.gotMsgs[1].Role != llm.RoleUser || fake.gotMsgs[1].Content != "hi" {
		t.Errorf("第二条应是 user 消息，实际为 %+v", fake.gotMsgs[1])
	}

	// 历史应是 user + assistant
	history := ag.History()
	if len(history) != 2 {
		t.Fatalf("历史长度 = %d, 想要 2", len(history))
	}
	if history[1].Role != llm.RoleAssistant || history[1].Content != "你好" {
		t.Errorf("历史中的回答不正确: %+v", history[1])
	}
}

func TestSendRollsBackOnError(t *testing.T) {
	fake := &fakeClient{err: errors.New("boom")}
	ag := New(fake, "")

	if err := ag.Send(context.Background(), "hi", nil); err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	if n := len(ag.History()); n != 0 {
		t.Errorf("失败后历史长度 = %d, 想要 0", n)
	}
}

func TestHistoryReturnsCopy(t *testing.T) {
	fake := &fakeClient{reply: "ok"}
	ag := New(fake, "")
	if err := ag.Send(context.Background(), "hi", nil); err != nil {
		t.Fatalf("Send 返回错误: %v", err)
	}

	history := ag.History()
	history[0].Content = "被外部修改"
	if ag.History()[0].Content != "hi" {
		t.Error("History 应返回副本，外部修改不能影响内部状态")
	}
}
