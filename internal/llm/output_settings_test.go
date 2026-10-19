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

func TestDefaultOutputLimitSurvivesSwap(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			var limits []int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					MaxTokens int `json:"max_tokens"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				limits = append(limits, body.MaxTokens)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer srv.Close()
			c := NewSwappable(NewClient(protocol, "test", srv.URL, "test-model"))
			c.SetMaxOutputTokens(2048)
			for _, maxTokens := range []int{0, 1, 0, 0} {
				if len(limits) == 2 {
					c.Swap(NewClient(protocol, "new-key", srv.URL, "next-model"))
				}
				if len(limits) == 3 {
					c.SetMaxOutputTokens(0)
				}
				_, err := c.StreamChat(context.Background(), ChatRequest{MaxTokens: maxTokens, Messages: []Message{{Role: RoleUser, Content: "hi"}}}, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			protocolDefault := 0
			if protocol == "anthropic" {
				protocolDefault = 8192
			}
			if want := []int{2048, 1, 2048, protocolDefault}; !reflect.DeepEqual(limits, want) {
				t.Fatalf("limits=%v, want %v", limits, want)
			}
		})
	}
}
