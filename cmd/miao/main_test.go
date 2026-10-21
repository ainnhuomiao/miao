package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
)

// 测试中的假客户端仍满足 llm.Client，但不会真正发送 HTTP 请求。
type fakeClient struct {
	requests [][]llm.Message
	failNext bool
}

func (f *fakeClient) Chat(_ context.Context, messages []llm.Message, onText func(string) error) (string, error) {
	f.requests = append(f.requests, messages)
	if f.failNext {
		f.failNext = false
		return "", errors.New("临时网络错误")
	}
	if err := onText("测试回答"); err != nil {
		return "", err
	}
	return "测试回答", nil
}

func TestTerminalCommandsAndConversation(t *testing.T) {
	client := &fakeClient{}
	var output bytes.Buffer
	// 一份字符串模拟用户依次输入；最后没有换行，检查 EOF 的处理。
	input := strings.NewReader("\n/help\n/unknown\n第一问\n第二问\n/clear\n新问题")
	if err := run(context.Background(), client, input, &output); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 3 || len(client.requests[1]) != 4 || len(client.requests[2]) != 2 {
		t.Fatalf("命令或历史处理错误：%+v", client.requests)
	}
	for _, text := range []string{"测试回答", "对话已清空", "未知命令"} {
		if !strings.Contains(output.String(), text) {
			t.Fatalf("终端没有显示 %q", text)
		}
	}
}

func TestTerminalCanContinueAfterNetworkFailure(t *testing.T) {
	client := &fakeClient{failNext: true}
	var output bytes.Buffer
	if err := run(context.Background(), client, strings.NewReader("失败问题\n重试问题\n/exit\n不会发送\n"), &output); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 2 || len(client.requests[1]) != 2 {
		t.Fatalf("失败消息进入历史，或退出后继续请求：%+v", client.requests)
	}
	if !strings.Contains(output.String(), "本轮未加入历史") {
		t.Fatal("网络失败没有明确提示")
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("MIAO_MODEL", "")
	if got := envOr("MIAO_MODEL", "default"); got != "default" {
		t.Fatalf("默认值=%q", got)
	}
	t.Setenv("MIAO_MODEL", " custom ")
	if got := envOr("MIAO_MODEL", "default"); got != "custom" {
		t.Fatalf("环境覆盖=%q", got)
	}
}
