package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
)

func TestWebSearchNotParallel(t *testing.T) {
	if isReadOnlyTool("web_search") {
		t.Fatal("web_search 需审批，不能并发执行")
	}
}

func TestWebSearchRoutedThroughApprover(t *testing.T) {
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(nil, "sys", reg)
	var display string
	asked := 0
	a.SetApprover(func(_ context.Context, name, d string, _ json.RawMessage) (bool, error) {
		asked++
		display = d
		return false, nil
	})
	args := json.RawMessage(`{"query":"q1 q2"}`)
	for _, m := range []PermissionMode{ModeAsk, ModeAcceptEdits, ModePlan} {
		a.SetMode(m)
		before := asked
		ok, err := a.tools.Approver()(context.Background(), "web_search", "搜索 q1 q2", args)
		if err != nil || ok || asked != before+1 || display != "搜索 q1 q2" {
			t.Fatalf("mode %v: 应弹审批卡 ok=%v asked=%d display=%q", m, ok, asked, display)
		}
	}
	// 无审批回调（-p 模式）：拒绝。
	a.SetApprover(nil)
	if ok, _ := a.tools.Approver()(context.Background(), "web_search", "搜索 x", args); ok {
		t.Fatal("无审批回调时应拒绝")
	}
}

// toolDeltaFailClient：第一次请求先流出工具调用增量再失败；记录每次请求的模型。
type toolDeltaFailClient struct {
	captureClient
	calls int
}

func (c *toolDeltaFailClient) StreamChat(ctx context.Context, req llm.ChatRequest, onDelta llm.DeltaHandler) (llm.ChatResult, error) {
	c.calls++
	if c.calls == 1 {
		_ = onDelta(llm.Delta{ToolCall: true})
		return llm.ChatResult{}, errors.New("primary down mid tool call")
	}
	return c.captureClient.StreamChat(ctx, req, onDelta)
}

func TestFallbackSkippedAfterToolCallDelta(t *testing.T) {
	c := &toolDeltaFailClient{captureClient: captureClient{model: "primary"}}
	a := New(c, "SYS", nil)
	a.SetFallbackModel("backup")
	if err := a.Send(context.Background(), "hi", nil); err == nil {
		t.Fatal("已流出工具调用增量，应直接返回错误")
	}
	if c.calls != 1 {
		t.Fatalf("不应再用备用模型重试，calls=%d", c.calls)
	}
}

func TestFallbackUsesPerRequestModel(t *testing.T) {
	c := &failOnceClient{captureClient: captureClient{model: "primary"}}
	a := New(c, "SYS", nil)
	a.SetFallbackModel("backup")
	if err := a.Send(context.Background(), "hi", nil); err != nil {
		t.Fatal(err)
	}
	if len(c.reqs) != 1 || c.reqs[0].Model != "backup" {
		t.Fatalf("备用请求应带 Model=backup：%+v", c.reqs)
	}
	if c.model != "primary" {
		t.Fatalf("不应修改全局模型：%q", c.model)
	}
}
