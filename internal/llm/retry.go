package llm

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPTimeout = 10 * time.Minute
	defaultMaxRetries  = 3
)

var backoffUnit = time.Second

// TimeoutError 表示本次请求超过了客户端超时（不是用户取消）。
type TimeoutError struct{ Timeout time.Duration }

func (e *TimeoutError) Error() string {
	return "请求超时（超过 " + e.Timeout.String() + "）"
}

func (e *TimeoutError) Unwrap() error { return context.DeadlineExceeded }

func backoffDelay(attempt int, err error) time.Duration {
	var api *APIError
	if errors.As(err, &api) && api.RetryAfter > 0 {
		return api.RetryAfter
	}
	d := backoffUnit << attempt // 1s, 2s, 4s（测试里会把 backoffUnit 调小）
	if d > 8*backoffUnit {
		d = 8 * backoffUnit
	}
	return d
}

func retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var to *TimeoutError
	if errors.As(err, &to) {
		return false
	}
	var api *APIError
	if errors.As(err, &api) {
		return api.StatusCode == 429 || api.StatusCode >= 500
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") || strings.Contains(s, "EOF")
}

func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if n, err := strconv.Atoi(h); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := time.Parse(time.RFC1123, h); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func logRetry(attempt int, delay time.Duration, err error) {
	slog.Debug("llm retry", "attempt", attempt, "delay", delay.String(), "err", err)
}

// countingDelta 包一层 onDelta：有正文或思考推出去之后，连接再断就不能重试，否则界面会重复。
type countingDelta struct {
	n  int
	fn DeltaHandler
}

func (c *countingDelta) handle(d Delta) error {
	if d.Content != "" || d.Reasoning != "" || d.ToolCall {
		c.n++
	}
	return c.fn(d)
}
