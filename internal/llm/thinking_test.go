package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestThinkingRequestParameters(t *testing.T) {
	for _, tt := range []struct {
		name, protocol, model, level, effort, kind string
		budget, maxTokens                          int
	}{
		{name: "default", model: "gpt-5", level: "auto"},
		{name: "openai", model: "gpt-5", level: "high", effort: "high"},
		{name: "off", model: "gpt-6-sol", level: "off", effort: "none"},
		{name: "deepseek", model: "deepseek-chat", level: "high", effort: "high", kind: "enabled"},
		{name: "deepseek-max", model: "deepseek-reasoner", level: "max", effort: "max", kind: "enabled"},
		{name: "deepseek-off", model: "deepseek-chat", level: "none", kind: "disabled"},
		{name: "claude-default", protocol: "anthropic", model: "claude-sonnet-5-5", level: "auto"},
		{name: "claude-adaptive", protocol: "anthropic", model: "claude-sonnet-5-5", level: "high", effort: "high", kind: "adaptive"},
		{name: "claude-off", protocol: "anthropic", model: "claude-sonnet-5-5", level: "off", kind: "between_tools"},
		{name: "claude-legacy", protocol: "anthropic", model: "claude-sonnet-4-5-20250929", level: "medium", kind: "enabled", budget: 4096},
		{name: "claude-legacy-high", protocol: "anthropic", model: "claude-opus-4-1", level: "high", kind: "enabled", budget: 8192},
		{name: "claude-legacy-max", protocol: "anthropic", model: "claude-3-7-sonnet-latest", level: "max", kind: "enabled", budget: 24576},
		{name: "claude-explicit-cap", protocol: "anthropic", model: "claude-sonnet-4-5", level: "high", kind: "enabled", budget: 2047, maxTokens: 2048},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var body struct {
				ReasoningEffort string          `json:"reasoning_effort"`
				Thinking        *thinkingConfig `json:"thinking"`
				OutputConfig    *outputConfig   `json:"output_config"`
				MaxTokens       int             `json:"max_tokens"`
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer srv.Close()
			c := NewSwappable(NewClient(tt.protocol, "test", srv.URL, tt.model))
			if err := c.SetThinking(tt.level); err != nil {
				t.Fatal(err)
			}
			_, err := c.StreamChat(context.Background(), ChatRequest{MaxTokens: tt.maxTokens, Messages: []Message{{Role: RoleUser, Content: "hi"}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			effort := body.ReasoningEffort
			if tt.protocol == "anthropic" && body.OutputConfig != nil {
				effort = body.OutputConfig.Effort
			}
			if effort != tt.effort {
				t.Errorf("effort=%q, want %q", effort, tt.effort)
			}
			if tt.kind == "" {
				if body.Thinking != nil {
					t.Fatalf("unexpected thinking: %+v", body.Thinking)
				}
			} else if body.Thinking == nil || body.Thinking.Type != tt.kind || body.Thinking.BudgetTokens != tt.budget {
				t.Fatalf("thinking=%+v, want %s budget=%d", body.Thinking, tt.kind, tt.budget)
			}
			if tt.budget > 0 && body.MaxTokens <= tt.budget {
				t.Errorf("max_tokens=%d must exceed budget=%d", body.MaxTokens, tt.budget)
			}
			if tt.maxTokens > 0 && body.MaxTokens != tt.maxTokens {
				t.Errorf("explicit max_tokens changed to %d", body.MaxTokens)
			}
		})
	}
}

type thinkingRecorder struct{ got string }

func (c *thinkingRecorder) StreamChat(_ context.Context, req ChatRequest, _ DeltaHandler) (ChatResult, error) {
	c.got = req.Thinking
	return ChatResult{}, nil
}

func TestThinkingSurvivesSwapAndRejectsInvalid(t *testing.T) {
	first, next := &thinkingRecorder{}, &thinkingRecorder{}
	c := NewSwappable(first)
	if err := c.SetThinking(" HIGH "); err != nil {
		t.Fatal(err)
	}
	if err := c.SetThinking("typo"); err == nil || c.Thinking() != "high" {
		t.Fatal("invalid input must leave the setting unchanged")
	}
	c.Swap(next)
	if _, err := c.StreamChat(context.Background(), ChatRequest{}, nil); err != nil || next.got != "high" {
		t.Fatalf("swap lost thinking: %q, %v", next.got, err)
	}
	if _, err := c.StreamChat(context.Background(), ChatRequest{Thinking: "auto"}, nil); err != nil || next.got != "auto" {
		t.Fatalf("request override failed: %q, %v", next.got, err)
	}
	if err := c.SetThinking("default"); err != nil || c.Thinking() != "auto" {
		t.Fatalf("reset failed: %v", err)
	}
}

func TestOpenAIExcludesAnthropicContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if _, exists := body.Messages[0]["anthropic_content"]; exists {
			t.Error("provider-specific signed content leaked to OpenAI")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	msgs := []Message{{Role: RoleAssistant, Content: "hello", AnthropicContent: json.RawMessage(`[{"type":"thinking","thinking":"plan","signature":"sig"}]`)}}
	c := NewOpenAI("test", srv.URL, "test-model")
	if _, err := c.StreamChat(context.Background(), ChatRequest{Messages: msgs}, nil); err != nil {
		t.Fatal(err)
	}
	if len(msgs[0].AnthropicContent) == 0 {
		t.Fatal("request serialization must not mutate the stored history")
	}
}

func TestAnthropicThinkingRejectsImpossibleSettings(t *testing.T) {
	for _, tt := range []struct {
		model, level string
		max          int
	}{
		{"claude-sonnet-4-5", "high", 1024},
		{"claude-opus-5-5", "none", 8192},
		{"claude-mythos-preview", "none", 8192},
		{"claude-sonnet-4-6", "typo", 8192},
	} {
		out := anthRequest{Model: tt.model, MaxTokens: tt.max}
		if err := configureAnthropicThinking(&out, tt.level, true); err == nil {
			t.Errorf("expected error for %s %s max=%d", tt.model, tt.level, tt.max)
		}
	}
}

func TestAnthropicSignedContentRoundTrip(t *testing.T) {
	events := []string{
		`{"type":"content_block_start","content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"plan"}}`,
		`{"type":"content_block_delta","delta":{"type":"signature_delta","signature":"sig-"}}`,
		`{"type":"content_block_delta","delta":{"type":"signature_delta","signature":"123"}}`,
		`{"type":"content_block_stop"}`,
		`{"type":"content_block_start","content_block":{"type":"redacted_thinking","data":"opaque"}}`,
		`{"type":"content_block_stop"}`,
		`{"type":"content_block_start","content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"checking"}}`,
		`{"type":"content_block_stop"}`,
		`{"type":"content_block_start","content_block":{"type":"tool_use","id":"call-1","name":"read_file","input":{}}}`,
		`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a.go\"}"}}`,
		`{"type":"content_block_stop"}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}))
	defer srv.Close()
	c := NewAnthropic("test", srv.URL, "claude-sonnet-4-6")
	res, err := c.StreamChat(context.Background(), ChatRequest{Thinking: "high"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `[ {"type":"thinking","thinking":"plan","signature":"sig-123"}, {"type":"redacted_thinking","data":"opaque"}, {"type":"text","text":"checking"}, {"type":"tool_use","id":"call-1","name":"read_file","input":{"path":"a.go"}} ]`
	var gotBlocks, wantBlocks any
	if err := json.Unmarshal(res.AnthropicContent, &gotBlocks); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &wantBlocks); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotBlocks, wantBlocks) {
		t.Fatalf("content lost: %s", res.AnthropicContent)
	}
	msg := Message{Role: RoleAssistant, Content: res.Content, ToolCalls: res.ToolCalls, AnthropicContent: res.AnthropicContent}
	// 模拟会话落盘和恢复，然后构建工具续轮。
	disk, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var restored Message
	if err := json.Unmarshal(disk, &restored); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(assistantContent(restored))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &gotBlocks); err != nil || !reflect.DeepEqual(gotBlocks, wantBlocks) {
		t.Fatalf("replay changed signed blocks: %s, %v", raw, err)
	}
}
