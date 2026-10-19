package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/llm"
)

func TestThinkingPaletteAndCommands(t *testing.T) {
	isolateHome(t)
	c := llm.NewSwappable(llm.NewOpenAI("test", "http://unused.invalid", "deepseek-flash"))
	m := New(agent.New(c, "test", nil), "deepseek-flash")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	typeText(m, "/thinking")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.input.Value() != "/thinking " || len(paletteNames(m)) != 5 {
		t.Fatalf("expected level menu, input=%q, matches=%v", m.input.Value(), paletteNames(m))
	}
	typeText(m, "hi")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "/thinking high" {
		t.Fatalf("tab completion: %q", m.input.Value())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if c.Thinking() != "high" || m.input.Value() != "" {
		t.Fatalf("selection not applied: %s, %q", c.Thinking(), m.input.Value())
	}
	if footer := ansi.Strip(m.hintView()); !strings.Contains(footer, "思考 high") {
		t.Fatalf("footer must show the current thinking level: %q", footer)
	}
	m.runCommand("/thinking typo")
	if c.Thinking() != "high" || !strings.Contains(m.messages[len(m.messages)-1].content, "未知思考强度") {
		t.Fatal("invalid level must be explained without changing the setting")
	}
	m.streaming = true
	m.runCommand("/thinking low")
	if c.Thinking() != "high" {
		t.Fatal("must not change while streaming")
	}
	m.streaming = false
	m.runCommand("/thinking off")
	if c.Thinking() != "none" {
		t.Fatal("off alias not applied")
	}
	m.runCommand("/thinking auto")
	if c.Thinking() != "auto" {
		t.Fatal("auto did not restore defaults")
	}
	if footer := ansi.Strip(m.hintView()); !strings.Contains(footer, "思考 auto") || strings.Contains(footer, "思考 high") {
		t.Fatalf("footer did not update with the setting: %q", footer)
	}
	if len(m.ag.History()) != 0 {
		t.Fatal("slash command must not become a user prompt")
	}
}

func TestThinkingMenuQueriesCurrentModelAndRefreshes(t *testing.T) {
	isolateHome(t)
	queries := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries++
		if r.Method != http.MethodGet {
			t.Error("opening thinking menu must not call a model for generation")
		}
		high := r.URL.Path == "/v1/models/custom-one"
		fmt.Fprintf(w, `{"id":"custom","capabilities":{"thinking":{"supported":true,"types":{"adaptive":{"supported":true},"disabled":{"supported":true}}},"effort":{"supported":true,"low":{"supported":true},"high":{"supported":%t}}}}`, high)
	}))
	defer srv.Close()
	c := llm.NewSwappable(llm.NewAnthropic("test", srv.URL, "custom-one"))
	m := New(agent.New(c, "test", nil), "custom-one")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if err := c.SetThinking("high"); err != nil {
		t.Fatal(err)
	}
	typeText(m, "/thinking")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runCmd(m, cmd)
	if want := []string{"auto", "none", "low", "high"}; !reflect.DeepEqual(paletteNames(m), want) || queries != 1 {
		t.Fatalf("menu must use queried levels: %v queries=%d", paletteNames(m), queries)
	}
	oldGeneration := m.thinkingLevels.generation
	m.applyModel("custom-two")
	runCmd(m, m.fetchThinkingCmd())
	if want := []string{"auto", "none", "low"}; !reflect.DeepEqual(paletteNames(m), want) || c.Thinking() != "auto" {
		t.Fatalf("new model must replace old capabilities and reset unsupported level: %v %s", paletteNames(m), c.Thinking())
	}
	notices := len(m.messages)
	m.Update(thinkingCapabilitiesMsg{model: "custom-one", generation: oldGeneration})
	if len(m.messages) != notices {
		t.Fatal("stale model response must not update notices or menu")
	}
	runCmd(m, m.runCommand("/thinking refresh"))
	if queries != 3 {
		t.Fatalf("refresh must perform a new metadata query: %d", queries)
	}
}

func TestUnknownThinkingMenuDoesNotAdvertiseGenericLevels(t *testing.T) {
	isolateHome(t)
	c := llm.NewSwappable(&fakeClient{})
	m := New(agent.New(c, "test", nil), "unknown-model")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	typeText(m, "/thinking ")
	if want := []string{"auto"}; !reflect.DeepEqual(paletteNames(m), want) {
		t.Fatalf("unknown model must not show unverified levels: %v", paletteNames(m))
	}
	m.changeThinking([]string{"high"})
	if !strings.Contains(m.messages[len(m.messages)-1].content, "尚未验证") {
		t.Fatal("manual configuration for unknown models must be identified as unverified")
	}
}
