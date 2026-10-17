package session

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"miao/internal/llm"
)

func TestSessionImageRefsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 4096)...)
	sess := s.New("gpt-4o", "/w")
	sess.Messages = []llm.Message{
		{Role: llm.RoleUser, Content: "看图", Images: []llm.Image{{MIME: "image/png", Name: "a.png", Data: png}}},
		{Role: llm.RoleAssistant, Content: "好的"},
	}
	if err := s.Save(sess); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, sess.ID+".json"))
	if strings.Contains(string(raw), "base64") || !strings.Contains(string(raw), `"images/`) || !strings.Contains(string(raw), `"content": "看图"`) {
		t.Fatalf("会话文件应只存图片引用：%s", raw)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "images", "*.png"))
	if len(files) != 1 {
		t.Fatalf("应写出一张图片文件：%v", files)
	}
	got, err := s.Load(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	im := got.Messages[0].Images
	if len(im) != 1 || !bytes.Equal(im[0].Data, png) || im[0].Name != "a.png" || len(got.Messages[1].Images) != 0 {
		t.Fatalf("应读回图片：%+v", im)
	}
	if err := s.Save(got); err != nil { // 再存一次不重复写
		t.Fatal(err)
	}
	if list, _ := s.List(); len(list) != 1 {
		t.Fatalf("images 目录不应算会话：%v", list)
	}
}

func TestSessionImageRefRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	os.WriteFile(filepath.Join(dir, "x.json"), []byte(`{"id":"x","messages":[{"role":"user","content":"a","images":[{"path":"images/../../etc/passwd","mime":"image/png"}]}]}`), 0o644)
	if got, err := s.Load("x"); err == nil && len(got.Messages[0].Images) != 0 {
		t.Fatal("越界引用应被忽略")
	}
}

func TestSessionImageOldAbsolutePath(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	dir := filepath.Join(data, "miao", "sessions")
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "images"), 0o755)
	os.WriteFile(filepath.Join(dir, "images", "a.png"), []byte("png"), 0o644)
	os.WriteFile(filepath.Join(data, "secret.png"), []byte("no"), 0o644)
	oldAbs := filepath.Join(data, "agnet", "sessions", "images", "a.png")
	newAbs := filepath.Join(dir, "images", "a.png")
	escape := filepath.Join(data, "agnet", "sessions", "images", "..", "..", "..", "secret.png")
	body := `{"id":"x","messages":[{"role":"user","content":"a","images":[` +
		`{"path":"` + oldAbs + `","mime":"image/png"},` +
		`{"path":"` + newAbs + `","mime":"image/png"},` +
		`{"path":"` + escape + `","mime":"image/png"},` +
		`{"path":"/etc/passwd","mime":"image/png"}]}]}`
	os.WriteFile(filepath.Join(dir, "x.json"), []byte(body), 0o644)
	got, err := s.Load("x")
	if err != nil {
		t.Fatal(err)
	}
	im := got.Messages[0].Images
	if len(im) != 2 || string(im[0].Data) != "png" || im[0].Path != "images/a.png" || im[1].Path != "images/a.png" {
		t.Fatalf("旧数据目录下的绝对路径应改写为相对路径，越界的忽略：%+v", im)
	}
}
