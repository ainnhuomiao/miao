package subagent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ainnhuomiao/miao/internal/llm"
)

type deadlineRecorder struct{ remaining time.Duration }

func (r *deadlineRecorder) StreamChat(ctx context.Context, _ llm.ChatRequest, _ llm.DeltaHandler) (llm.ChatResult, error) {
	if deadline, ok := ctx.Deadline(); ok {
		r.remaining = time.Until(deadline)
	}
	return llm.ChatResult{Content: "done"}, nil
}

func TestConfiguredTaskTimeoutAndParentDeadline(t *testing.T) {
	c := &deadlineRecorder{}
	m, reg := newMgr(t, c, Config{Enabled: true, TimeoutSec: 7})
	res := reg.Call(context.Background(), ToolName, `{"description":"x","prompt":"y"}`)
	if res.IsError || c.remaining < 6*time.Second || c.remaining > 7*time.Second {
		t.Fatalf("configured task timeout not applied: %v, %+v", c.remaining, res)
	}
	cfg := m.Config()
	cfg.TimeoutSec = 9
	if err := m.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res = reg.Call(ctx, ToolName, `{"description":"x","prompt":"y"}`)
	if res.IsError || c.remaining <= 0 || c.remaining > time.Second {
		t.Fatal("configured timeout must not extend the parent deadline")
	}
}

func TestConfiguredToolRoundLimitAndFinalAnswer(t *testing.T) {
	for _, final := range []bool{true, false} {
		fc := &fakeClient{step: func(n int, req llm.ChatRequest) (llm.ChatResult, error) {
			if final && n == 1 {
				return llm.ChatResult{Content: "finished at the limit"}, nil
			}
			return llm.ChatResult{ToolCalls: []llm.ToolCall{grepCall("g")}}, nil
		}}
		m, reg := newMgr(t, fc, Config{Enabled: true, MaxToolRounds: 1})
		res := reg.Call(context.Background(), ToolName, `{"description":"x","prompt":"y"}`)
		if final {
			if res.IsError || !strings.Contains(res.Content, "finished at the limit") {
				t.Fatal("must allow a final answer after the configured tool limit", res)
			}
		} else if !res.IsError || !strings.Contains(res.Content, "超过上限 1 轮") {
			t.Fatal("must reject another tool batch beyond the configured limit", res)
		}
		if fc.n != 2 || !strings.Contains(res.Content, "1 轮工具") || m.Running() != 0 {
			t.Fatalf("unexpected limit accounting or unreleased concurrency slot: %+v, requests=%d", res, fc.n)
		}
	}
}

func TestTaskLimitConfigValidationAndPersistence(t *testing.T) {
	tempXDG(t)
	for _, cfg := range []Config{{TimeoutSec: -1}, {TimeoutSec: int(^uint(0) >> 1)}, {MaxToolRounds: -1}} {
		if err := SaveConfig(cfg); err == nil {
			t.Fatal("invalid task limits must not be saved")
		}
	}
	want := Config{Enabled: true, TimeoutSec: 45, MaxToolRounds: 12}.Normalize()
	if err := SaveConfig(want); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadConfig(); err != nil || got != want {
		t.Fatalf("task limits did not survive config save/load: %+v, %v", got, err)
	}
}
