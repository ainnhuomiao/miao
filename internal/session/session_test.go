package session

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ainnhuomiao/miao/internal/llm"
)

func newTestStore(t *testing.T) *Store {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	sess := s.New("deepseek-flash", "/ws")
	sess.Messages = []llm.Message{
		{Role: llm.RoleUser, Content: "帮我看看这个仓库\n第二行"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c1"}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "ok"},
	}
	if err := s.Save(sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "帮我看看这个仓库" || len(got.Messages) != 3 || got.Messages[2].ToolCallID != "c1" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	info, _ := os.Stat(s.path(sess.ID))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", info.Mode().Perm())
	}
}

func TestEmptySessionNotSaved(t *testing.T) {
	s := newTestStore(t)
	sess := s.New("m", "")
	if err := s.Save(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(sess.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestListOrderAndCorruptSkip(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 10, 17, 20, 0, 0, 0, time.UTC)
	for i, title := range []string{"旧的", "新的"} {
		s.now = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		sess := s.New("m", "")
		sess.ID = "s" + title[:0] + string(rune('a'+i))
		sess.Title = title
		sess.Messages = []llm.Message{{Role: llm.RoleUser, Content: title}}
		if err := s.Save(sess); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(s.path("broken"), []byte("{not json"), 0o600)
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Title != "新的" || list[1].Title != "旧的" {
		t.Fatalf("list = %+v", list)
	}
}

func TestDeleteAndBadIDs(t *testing.T) {
	s := newTestStore(t)
	sess := s.New("m", "")
	sess.Messages = []llm.Message{{Role: llm.RoleUser, Content: "x"}}
	s.Save(sess)
	if err := s.Delete(sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(sess.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	for _, id := range []string{"", "../x", "a/b", "a.json"} {
		if _, err := s.Load(id); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("Load(%q) should reject id, got %v", id, err)
		}
	}
}

func TestSaveRedactsSecrets(t *testing.T) {
	s := newTestStore(t)
	sess := s.New("m", "")
	sess.Messages = []llm.Message{{
		Role:    llm.RoleTool,
		Content: "MIAO_API_KEY=sk-abcdefghijklmnopqrstuvwxyz extra",
	}}
	if err := s.Save(sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Messages[0].Content, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("should redact secret: %q", got.Messages[0].Content)
	}
}

func TestTitleFrom(t *testing.T) {
	if got := TitleFrom(nil); got != "新会话" {
		t.Fatal(got)
	}
	long := "一二三四五六七八九十一二三四五六七八九十一二三四五六"
	if got := TitleFrom([]llm.Message{{Role: llm.RoleUser, Content: long}}); got != string([]rune(long)[:24])+"…" {
		t.Fatal(got)
	}
}
