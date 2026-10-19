package subagent

import (
	"context"
	"testing"

	"github.com/ainnhuomiao/miao/internal/tools"
)

func TestSubagentInheritsToolSwitches(t *testing.T) {
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tools.RegisterBuiltins(reg)
	reg.RestrictTo([]string{"task", "read_file", "web_fetch"})
	reg.SetDisabledTools([]string{"web_fetch"})
	mgr := New(reg, nil, func() string { return "test" }, Config{Enabled: true, AllowWeb: true})
	defs := mgr.SubTools().Definitions()
	if len(defs) != 1 || defs[0].Function.Name != "read_file" {
		t.Fatal("subagent should inherit the main tool allowlist and denylist")
	}
	if res := mgr.SubTools().Call(context.Background(), "web_fetch", `{"url":"https://example.com"}`); !res.IsError {
		t.Fatal("allow_web must not override a globally disabled tool")
	}
}
