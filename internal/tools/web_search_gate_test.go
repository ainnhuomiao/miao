package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestWebSearchRequiresApproval(t *testing.T) {
	r, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registerWebSearch(r)
	// 未配置审批（-p 无审批）：直接拒绝，不发请求。
	res := r.Call(context.Background(), "web_search", `{"query":"secret token abc"}`)
	if !res.IsError || !strings.Contains(res.Content, "需要审批") {
		t.Fatalf("无审批时应拒绝：%+v", res)
	}
	var gotName, gotDisplay string
	r.SetApprover(func(_ context.Context, name, display string, _ json.RawMessage) (bool, error) {
		gotName, gotDisplay = name, display
		return false, nil
	})
	q := "golang context 取消 传播 很长的查询串 xyz"
	res = r.Call(context.Background(), "web_search", `{"query":"`+q+`"}`)
	if !res.IsError || !strings.Contains(res.Content, "用户拒绝搜索") {
		t.Fatalf("拒绝后应报错：%+v", res)
	}
	if gotName != "web_search" || gotDisplay != "搜索 "+q {
		t.Fatalf("审批卡应显示完整查询串：%q %q", gotName, gotDisplay)
	}
}
