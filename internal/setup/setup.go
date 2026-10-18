// Package setup 是首次配置的共享逻辑：服务商列表、连接测试、保存 credentials。
// TUI 向导、`miao init` 和 /login 都用它，界面各自实现。
package setup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/ainnhuomiao/miao/internal/appdir"
	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/llm"
)

// Provider 是可选的服务商。
type Provider struct {
	ID       string // 写进 credentials 的 provider 字段
	Name     string // 给人看的名字
	Protocol string // openai / anthropic
	BaseURL  string // 内置地址；为空表示需要用户填写
	Model    string // 默认模型；为空时连接测试成功后取服务端列表里的第一个
}

// NeedsBaseURL 报告是否需要用户填写接口地址（只有 OpenAI 兼容需要）。
func (p Provider) NeedsBaseURL() bool { return p.BaseURL == "" }

// Providers 是向导里列出的服务商，顺序即显示顺序。
var Providers = []Provider{
	{ID: "deepseek", Name: "DeepSeek", Protocol: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat"},
	{ID: "anthropic", Name: "Anthropic", Protocol: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-4-5"},
	{ID: "openai", Name: "OpenAI 兼容", Protocol: "openai"},
}

// ProviderByID 按 ID 找服务商，找不到返回第一个和 false。
func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Providers[0], false
}

// Credentials 按服务商、Key 和（OpenAI 兼容时的）地址拼出要保存的连接信息。
func Credentials(p Provider, key, baseURL string) config.Credentials {
	c := config.Credentials{Provider: p.ID, Protocol: p.Protocol, BaseURL: p.BaseURL, Model: p.Model, APIKey: strings.TrimSpace(key)}
	if p.NeedsBaseURL() {
		c.BaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}
	return c
}

// ValidateBaseURL 检查用户填的地址是不是 http(s) URL。
func ValidateBaseURL(s string) error {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("地址要以 http:// 或 https:// 开头")
	}
	return nil
}

// TestTimeout 是连接测试的总超时。
const TestTimeout = 10 * time.Second

// Tester 测试连接是否可用；成功时可以补全 Model（例如从模型列表里选第一个）。
type Tester func(ctx context.Context, c config.Credentials) (config.Credentials, error)

// ErrKind 区分连接测试失败的原因。
type ErrKind int

const (
	// KindOther 是其它错误（服务端 5xx、余额不足等）。
	KindOther ErrKind = iota
	// KindAuth 是 Key 错了（401/403）。
	KindAuth
	// KindNetwork 是网络不通（连不上、DNS 失败、超时）。
	KindNetwork
)

// TestError 是带分类的连接测试错误。
type TestError struct {
	Kind ErrKind
	Err  error
}

func (e *TestError) Error() string {
	switch e.Kind {
	case KindAuth:
		return "key 错了，服务端拒绝了（" + short(e.Err) + "）"
	case KindNetwork:
		return "网络不通，连不上服务（" + short(e.Err) + "）"
	}
	return short(e.Err)
}

func (e *TestError) Unwrap() error { return e.Err }

func short(err error) string {
	if err == nil {
		return "未知错误"
	}
	s := strings.TrimSpace(err.Error())
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}

// Classify 把连接测试的错误分成 key 错了 / 网络不通 / 其它。
func Classify(err error) *TestError {
	if err == nil {
		return nil
	}
	var te *TestError
	if errors.As(err, &te) {
		return te
	}
	var api *llm.APIError
	if errors.As(err, &api) {
		if api.StatusCode == 401 || api.StatusCode == 403 {
			return &TestError{Kind: KindAuth, Err: err}
		}
		return &TestError{Kind: KindOther, Err: err}
	}
	var nerr net.Error
	var operr *net.OpError
	var dnserr *net.DNSError
	var to *llm.TimeoutError
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &to) || errors.As(err, &operr) ||
		errors.As(err, &dnserr) || (errors.As(err, &nerr) && nerr.Timeout()) {
		return &TestError{Kind: KindNetwork, Err: err}
	}
	return &TestError{Kind: KindOther, Err: err}
}

// DefaultTester 先免费地列出模型；服务端不支持（404/405/501 或协议本身不支持）时退回发 1 个 token 的补全。
func DefaultTester(ctx context.Context, c config.Credentials) (config.Credentials, error) {
	ctx, cancel := context.WithTimeout(ctx, TestTimeout)
	defer cancel()
	client := llm.NewClient(c.Protocol, c.APIKey, c.BaseURL, c.Model)
	llm.ApplySettings(client, TestTimeout, 0, false)

	if lister, ok := client.(llm.ModelLister); ok {
		names, err := lister.ListModels(ctx)
		if err == nil {
			if c.Model == "" && len(names) > 0 {
				c.Model = names[0]
			}
			return c, nil
		}
		if !listUnsupported(err) {
			return c, Classify(err)
		}
	}
	if c.Model == "" {
		return c, &TestError{Kind: KindOther, Err: errors.New("服务端不支持列出模型，请用 MIAO_MODEL 指定模型名后再试")}
	}
	_, err := client.StreamChat(ctx, llm.ChatRequest{
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		MaxTokens: 1,
	}, func(llm.Delta) error { return nil })
	if err != nil {
		return c, Classify(err)
	}
	return c, nil
}

func listUnsupported(err error) bool {
	var api *llm.APIError
	if errors.As(err, &api) {
		return api.StatusCode == 404 || api.StatusCode == 405 || api.StatusCode == 501
	}
	return false
}

// Save 保存 credentials，并返回给用户看的一句话（不含 Key）。
func Save(c config.Credentials) (string, error) {
	if err := config.SaveCredentials(c); err != nil {
		return "", fmt.Errorf("保存失败：%w", err)
	}
	p, _ := config.CredentialsPath()
	return "已保存到 " + tildePath(p), nil
}

func tildePath(p string) string {
	return appdir.Tilde(p)
}
