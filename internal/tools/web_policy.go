package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// WebPolicy 判断 web_fetch 的目标地址能否免审批访问；重定向的每一跳也用它重新检查。
type WebPolicy func(u *url.URL) bool

type webPolicyKey struct{}

// SetWebPolicy 设置 web_fetch 放行规则；nil 表示不限制（如用户显式允许联网的子代理）。
func (r *Registry) SetWebPolicy(p WebPolicy) { r.webPolicy = p }

// gateWebFetch 在抓取前走审批回调（由上层按 WebPolicy 自动放行或弹卡），
// 并把放行规则放进 ctx，供重定向检查使用。ok=false 时返回错误结果。
func (r *Registry) gateWebFetch(ctx context.Context, raw string, args json.RawMessage) (context.Context, Result, bool) {
	if r.webPolicy != nil {
		ctx = context.WithValue(ctx, webPolicyKey{}, r.webPolicy)
	}
	approver := r.Approver()
	if approver == nil {
		if r.webPolicy == nil {
			return ctx, Result{}, true
		}
		return ctx, Result{Content: "web_fetch 需要审批但未配置审批: " + raw, IsError: true}, false
	}
	allow, err := approver(ctx, "web_fetch", "抓取 "+raw, args)
	if err != nil {
		return ctx, Result{Content: "审批中断: " + err.Error(), IsError: true}, false
	}
	if !allow {
		auditEvent(r.workspace, "web_fetch", raw, "reject")
		return ctx, Result{Content: "用户拒绝抓取: " + raw, IsError: true}, false
	}
	auditEvent(r.workspace, "web_fetch", raw, "allow")
	return ctx, Result{}, true
}

// checkRedirectPolicy 对重定向目标重跑放行规则：哪怕原地址是审批卡单独批准的，跳转也必须满足规则。
func checkRedirectPolicy(req *http.Request) error {
	p, _ := req.Context().Value(webPolicyKey{}).(WebPolicy)
	if p == nil || p(req.URL) {
		return nil
	}
	return fmt.Errorf("重定向到未放行的地址：%s，需重新审批", req.URL)
}
