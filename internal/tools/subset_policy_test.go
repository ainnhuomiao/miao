package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestEmptyToolAllowlistBlocksExistingAndLateTools(t *testing.T) {
	r, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)
	r.RestrictTo([]string{})
	called := false
	r.Register(Tool{Def: toolDef("late_tool", "late MCP tool", nil), Run: func(context.Context, json.RawMessage) Result {
		called = true
		return Result{Content: "ran"}
	}})
	r.Freeze()
	if len(r.Definitions()) != 0 {
		t.Fatal("empty allowlist must expose no tools, including late registrations")
	}
	for _, name := range []string{"read_file", "write_file", "run_command", "late_tool"} {
		if res := r.Call(context.Background(), name, `{}`); !res.IsError {
			t.Fatalf("disabled tool ran: %s", name)
		}
	}
	if called {
		t.Fatal("disabled tool callback must never run")
	}
	r.Unfreeze()
	r.RestrictTo(nil)
	if len(r.Definitions()) == 0 {
		t.Fatal("nil must restore the unrestricted tool list")
	}
}

func TestDisabledToolsOverrideAllowlistAndSubsets(t *testing.T) {
	r, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)
	r.RestrictTo([]string{"read_file", "web_fetch"})
	r.SetDisabledTools([]string{"web_fetch", "late_tool"})
	called := false
	r.Register(Tool{Def: toolDef("late_tool", "late tool", nil), Run: func(context.Context, json.RawMessage) Result {
		called = true
		return Result{}
	}})
	for _, registry := range []*Registry{r, r.Subset("read_file", "web_fetch", "late_tool")} {
		defs := registry.Definitions()
		if len(defs) != 1 || defs[0].Function.Name != "read_file" {
			t.Fatal("only enabled tools may be exposed or copied to subsets")
		}
		for _, name := range []string{"web_fetch", "late_tool"} {
			if res := registry.Call(context.Background(), name, `{}`); !res.IsError {
				t.Fatalf("disabled tool ran: %s", name)
			}
		}
	}
	if called {
		t.Fatal("disabled callback ran")
	}
}
