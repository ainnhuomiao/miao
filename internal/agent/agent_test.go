package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
)

// 函数类型也可以拥有方法。这个适配器让测试用一个函数替代真实模型。
type clientFunc func(context.Context, []llm.Message, func(string) error) (string, error)

func (f clientFunc) Chat(ctx context.Context, messages []llm.Message, onText func(string) error) (string, error) {
	return f(ctx, messages, onText)
}

// 测试不访问网络：检查第二次请求是否确实包含第一轮的问答。
func TestSendCarriesHistoryAndClearResetsIt(t *testing.T) {
	var requests [][]llm.Message
	client := clientFunc(func(_ context.Context, messages []llm.Message, onText func(string) error) (string, error) {
		requests = append(requests, messages)
		if onText != nil {
			if err := onText("回答"); err != nil {
				return "", err
			}
		}
		return "回答", nil
	})
	a := New(client)
	var shown string
	if err := a.Send(context.Background(), "第一问", func(part string) error { shown += part; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(context.Background(), "第二问", nil); err != nil {
		t.Fatal(err)
	}
	want := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "第一问"},
		{Role: "assistant", Content: "回答"},
		{Role: "user", Content: "第二问"},
	}
	if !reflect.DeepEqual(requests[1], want) || shown != "回答" {
		t.Fatalf("第二次请求=%+v，显示=%q", requests[1], shown)
	}
	a.Reset()
	if err := a.Send(context.Background(), "重新开始", nil); err != nil {
		t.Fatal(err)
	}
	if len(requests[2]) != 2 || requests[2][1].Content != "重新开始" {
		t.Fatalf("清空后仍携带旧历史：%+v", requests[2])
	}
}

// 已经显示一部分回答后网络失败，也不能把半轮消息保存进历史。
func TestSendFailurePreservesCompletedHistory(t *testing.T) {
	networkErr := errors.New("断开连接")
	a := New(clientFunc(func(context.Context, []llm.Message, func(string) error) (string, error) {
		return "半句回答", networkErr
	}))
	a.history = append(a.history, llm.Message{Role: "user", Content: "旧问题"}, llm.Message{Role: "assistant", Content: "旧回答"})
	before := append([]llm.Message(nil), a.history...)
	if err := a.Send(context.Background(), "新问题", nil); !errors.Is(err, networkErr) {
		t.Fatalf("没有保留原始错误：%v", err)
	}
	if !reflect.DeepEqual(a.history, before) {
		t.Fatalf("失败修改了历史：%+v", a.history)
	}
	if err := a.Send(context.Background(), "  ", nil); err == nil {
		t.Fatal("空消息应该被拒绝")
	}
}
