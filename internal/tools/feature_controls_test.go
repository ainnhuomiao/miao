package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/config"
)

func TestDisabledSnapshotsStillEnforceApprovalsAndPaths(t *testing.T) {
	r, workspace := newWriteReg(t, false)
	snapshotDir := filepath.Join(t.TempDir(), "snapshots")
	r.SetSnapshotsEnabled(false)
	r.SetSnapshotDir(snapshotDir) // 切换会话不能重新启用快照
	ctx := context.Background()
	args := `{"path":"a.txt","content":"after"}`
	if res := r.Call(ctx, "write_file", args); !res.IsError {
		t.Fatal("disabling snapshots must not bypass write approval")
	}
	if _, err := os.Stat(filepath.Join(workspace, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("rejected write created a file")
	}
	r.SetApprover(func(context.Context, string, string, json.RawMessage) (bool, error) { return true, nil })
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := r.Call(ctx, "write_file", args); res.IsError {
		t.Fatal(res.Content)
	}
	if data, err := os.ReadFile(filepath.Join(workspace, "a.txt")); err != nil || string(data) != "after" {
		t.Fatalf("approved edits must still work: %s, %v", data, err)
	}
	if r.UndoDepth() != 0 || r.HasSnapshot("a.txt") {
		t.Fatal("disabled snapshots must create no undo state")
	}
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Fatal("disabled snapshots must not create their disk directory")
	}
	for _, path := range []string{"../escape.txt", ".env"} {
		body, _ := json.Marshal(map[string]string{"path": path, "content": "no"})
		if res := r.Call(ctx, "write_file", string(body)); !res.IsError {
			t.Fatalf("disabled snapshots weakened path protection for %s", path)
		}
	}
	r.SetSnapshotsEnabled(true)
	if res := r.Call(ctx, "edit_file", `{"path":"a.txt","old_string":"after","new_string":"next"}`); res.IsError {
		t.Fatal(res.Content)
	}
	if _, err := r.Undo(); err != nil {
		t.Fatal("re-enabled snapshots should support undo", err)
	}
	if data, _ := os.ReadFile(filepath.Join(workspace, "a.txt")); string(data) != "after" {
		t.Fatal("undo after re-enabling restored the wrong version")
	}
}

func TestDisabledDiagnosticsSuppressWorkAndNotices(t *testing.T) {
	t.Setenv("MIAO_DIAGNOSE", "1")
	r, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.noteEdited("a.go")
	r.SetDiagnosticsEnabled(false)
	r.noteEdited("b.go")
	if result := r.FlushDiagnose(context.Background()); result != "" || r.TakeDiagnoseNotice() != "" {
		t.Fatal("disabled diagnostics must not run or warn about skipped checks")
	}
	if len(r.dirty) != 0 {
		t.Fatal("disabled diagnostics should not retain a pending batch")
	}
}

func TestExplicitDiagnosticsRetainProjectTrustBoundary(t *testing.T) {
	workspace := vetWorkspace(t)
	t.Setenv("MIAO_DIAGNOSE", "0")
	r, err := NewRegistry(workspace)
	if err != nil {
		t.Fatal(err)
	}
	r.SetDiagnosticsEnabled(true)
	r.noteEdited("a.go")
	if result := r.FlushDiagnose(context.Background()); result != "" {
		t.Fatal("explicitly enabling diagnostics must not execute untrusted Go code")
	}
	if !strings.Contains(r.TakeDiagnoseNotice(), "/trust-project") {
		t.Fatal("explicit enable must preserve the project trust notice")
	}
	if _, err := config.TrustProject(workspace); err != nil {
		t.Fatal(err)
	}
	r.noteEdited("a.go")
	if result := r.FlushDiagnose(context.Background()); !strings.Contains(result, "go vet") {
		t.Fatalf("explicit setting should override the legacy environment after trust: %q", result)
	}
}
