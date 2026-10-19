package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

const adaptiveMetadata = `{"id":"new-model-alias","capabilities":{"thinking":{"supported":true,"types":{"adaptive":{"supported":true},"enabled":{"supported":false},"disabled":{"supported":true}}},"effort":{"supported":true,"low":{"supported":true},"medium":{"supported":false},"high":{"supported":true},"xhigh":{"supported":false},"max":{"supported":false}}}}`

func TestQueriedThinkingLevelsAndRequestMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" || r.Header.Get("anthropic-version") == "" {
			t.Error("missing metadata authentication")
		}
		switch r.URL.Path {
		case "/v1/models/new-model-alias":
			if r.Method != http.MethodGet {
				t.Error("capability discovery must only GET metadata")
			}
			fmt.Fprint(w, adaptiveMetadata)
		case "/v1/messages":
			var body anthRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Thinking == nil || body.Thinking.Type != "adaptive" || body.OutputConfig == nil || body.OutputConfig.Effort != "low" {
				t.Errorf("queried mode not used for unknown alias: %+v", body)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewSwappable(NewAnthropic("test-key", srv.URL, "new-model-alias"))
	if info := c.ThinkingCapabilities(); info.Known || !reflect.DeepEqual(info.Levels, []string{"auto"}) {
		t.Fatalf("unknown model must not advertise generic levels: %+v", info)
	}
	info, err := c.QueryThinkingCapabilities(context.Background())
	if err != nil || !info.Known || info.Source != "服务端查询" || !reflect.DeepEqual(info.Levels, []string{"auto", "none", "low", "high"}) {
		t.Fatalf("server metadata not honored: %+v, %v", info, err)
	}
	if !strings.Contains(info.Summary(), "2 档") {
		t.Fatalf("auto and none must not count as effort levels: %s", info.Summary())
	}
	if err := c.SetThinking("max"); err == nil {
		t.Fatal("unsupported max must be rejected before a chat request")
	}
	if err := c.SetThinking("low"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StreamChat(context.Background(), ChatRequest{}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIListMetadataFallbackAndUnknown(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		t.Run(fmt.Sprint(metadata), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("discovery must only authenticate GET requests")
				}
				if r.URL.Path != "/models" {
					http.NotFound(w, r)
					return
				}
				if metadata {
					fmt.Fprintf(w, `{"data":[{"id":"unrelated"},%s]}`, adaptiveMetadata)
				} else {
					fmt.Fprint(w, `{"data":[{"id":"new-model-alias"}]}`)
				}
			}))
			defer srv.Close()
			c := NewSwappable(NewOpenAI("test-key", srv.URL, "new-model-alias"))
			info, err := c.QueryThinkingCapabilities(context.Background())
			if err != nil || info.Known != metadata || calls != 2 {
				t.Fatalf("info=%+v err=%v calls=%d", info, err, calls)
			}
			if !metadata {
				if !reflect.DeepEqual(info.Levels, []string{"auto"}) || !strings.Contains(info.Summary(), "未知") {
					t.Fatalf("missing metadata must remain unknown: %+v", info)
				}
				if err := c.SetThinking("high"); err != nil {
					t.Fatalf("unknown models must still allow explicit manual configuration: %v", err)
				}
			}
		})
	}
}

func TestDocumentedThinkingAndModelSwitch(t *testing.T) {
	c := NewSwappable(NewOpenAI("test", "http://unused.invalid", "deepseek-flash"))
	info := c.ThinkingCapabilities()
	if info.Source != "文档适配" || !reflect.DeepEqual(info.Levels, []string{"auto", "none", "low", "high", "max"}) {
		t.Fatalf("DeepSeek must expose distinct effective levels: %+v", info)
	}
	if err := c.SetThinking("max"); err != nil {
		t.Fatal(err)
	}
	c.SetModel("gpt-5")
	if c.Thinking() != "auto" || c.ThinkingCapabilities().Supports("max") {
		t.Fatal("incompatible thinking level must reset on model change")
	}
}

type capabilityClient struct {
	thinkingRecorder
	query func(context.Context) (ThinkingCapabilities, error)
}

func (c *capabilityClient) Model() string   { return "same-model" }
func (c *capabilityClient) SetModel(string) {}
func (c *capabilityClient) queryThinkingCapabilities(ctx context.Context, _ string) (ThinkingCapabilities, error) {
	return c.query(ctx)
}

func TestThinkingDiscoveryIgnoresOldProviderResult(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	old := &capabilityClient{query: func(ctx context.Context) (ThinkingCapabilities, error) {
		close(started)
		select {
		case <-release:
			return ThinkingCapabilities{Levels: []string{"auto", "max"}, Known: true, Source: "服务端查询"}, nil
		case <-ctx.Done():
			return unknownThinking(), ctx.Err()
		}
	}}
	c := NewSwappable(old)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.QueryThinkingCapabilities(ctx); done <- err }()
	<-started
	c.Swap(&capabilityClient{query: func(context.Context) (ThinkingCapabilities, error) {
		return ThinkingCapabilities{Levels: []string{"auto", "low"}, Known: true, Source: "服务端查询"}, nil
	}})
	if _, err := c.QueryThinkingCapabilities(ctx); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if info := c.ThinkingCapabilities(); !reflect.DeepEqual(info.Levels, []string{"auto", "low"}) {
		t.Fatalf("late response from old provider polluted cache: %+v", info)
	}
}

func TestLegacyThinkingMetadataIsBudgetNotNativeEffort(t *testing.T) {
	var model modelThinkingMetadata
	if err := json.Unmarshal([]byte(`{"id":"legacy-alias","capabilities":{"thinking":{"supported":true,"types":{"enabled":{"supported":true},"disabled":{"supported":true}}},"effort":{"supported":false}}}`), &model); err != nil {
		t.Fatal(err)
	}
	info := model.thinkingCapabilities()
	if !info.Budget || info.mode != "enabled" || info.Supports("minimal") || !strings.Contains(info.Summary(), "预算预设") {
		t.Fatalf("legacy budgets must not be confused with native levels: %+v", info)
	}
}

func TestThinkingMetadataFailureKeepsConfirmedCache(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, adaptiveMetadata)
	}))
	defer srv.Close()
	c := NewSwappable(NewAnthropic("test", srv.URL, "new-model-alias"))
	want, err := c.QueryThinkingCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	got, err := c.QueryThinkingCapabilities(context.Background())
	if err == nil || got.Source != "服务端缓存" || !reflect.DeepEqual(got.Levels, want.Levels) {
		t.Fatalf("failed refresh must retain confirmed levels: %+v, %v", got, err)
	}
}

func TestThinkingRefreshIgnoresOlderQuery(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	c := NewSwappable(&capabilityClient{query: func(ctx context.Context) (ThinkingCapabilities, error) {
		level := "low"
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
				level = "max"
			case <-ctx.Done():
				return unknownThinking(), ctx.Err()
			}
		}
		return ThinkingCapabilities{Levels: []string{"auto", level}, Known: true, Source: "服务端查询"}, nil
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.QueryThinkingCapabilities(ctx); done <- err }()
	<-started
	if _, err := c.QueryThinkingCapabilities(ctx); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if info := c.ThinkingCapabilities(); !reflect.DeepEqual(info.Levels, []string{"auto", "low"}) {
		t.Fatalf("older query overwrote refreshed capabilities: %+v", info)
	}
}
