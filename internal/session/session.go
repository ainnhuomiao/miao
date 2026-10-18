// Package session 把对话保存为本地 JSON 文件，供会话侧栏列出、恢复和删除。
package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ainnhuomiao/miao/internal/appdir"
	"github.com/ainnhuomiao/miao/internal/llm"
)

// ErrNotFound 表示会话不存在。
var ErrNotFound = errors.New("会话不存在")

// Session 是一段完整对话。
type Session struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Model     string        `json:"model,omitempty"`
	Workspace string        `json:"workspace,omitempty"`
	Created   time.Time     `json:"created"`
	Updated   time.Time     `json:"updated"`
	Messages  []llm.Message `json:"messages"`
}

// Meta 是侧栏列表用的摘要，不含消息正文。
type Meta struct {
	ID       string
	Title    string
	Updated  time.Time
	MsgCount int
}

// Store 管理一个目录下的会话文件（每个会话一个 <id>.json）。
type Store struct {
	dir string
	now func() time.Time
}

// DefaultDir 返回默认目录：$XDG_DATA_HOME/miao/sessions，或 ~/.local/share/miao/sessions。
func DefaultDir() (string, error) {
	dir, err := appdir.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions"), nil
}

// NewStore 打开（必要时创建）会话目录。
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建会话目录失败: %w", err)
	}
	return &Store{dir: dir, now: time.Now}, nil
}

// Dir 返回会话目录。
func (s *Store) Dir() string { return s.dir }

// New 创建一个尚未保存的空会话。
func (s *Store) New(model, workspace string) *Session {
	t := s.now()
	return &Session{ID: newID(t), Model: model, Workspace: workspace, Created: t, Updated: t}
}

// Save 原子写入会话；标题为空时取第一条用户消息。空会话不落盘。
func (s *Store) Save(sess *Session) error {
	if err := validID(sess.ID); err != nil {
		return err
	}
	if len(sess.Messages) == 0 {
		return nil
	}
	if sess.Title == "" {
		sess.Title = TitleFrom(sess.Messages)
	}
	sess.Updated = s.now()
	toWrite := *sess
	toWrite.Messages = redactMessages(sess.Messages)
	data, err := json.MarshalIndent(s.diskForm(&toWrite), "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), s.path(sess.ID)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Load 读取一个会话。
func (s *Store) Load(id string) (*Session, error) {
	if err := validID(id); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("会话文件损坏 %s: %w", id, err)
	}
	s.loadImages(&sess, data)
	return &sess, nil
}

// List 按最近更新倒序列出会话；损坏的文件跳过。
func (s *Store) List() ([]Meta, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []Meta
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		meta, err := s.readMeta(filepath.Join(s.dir, name))
		if err != nil {
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// Delete 删除一个会话。
func (s *Store) Delete(id string) error {
	if err := validID(id); err != nil {
		return err
	}
	err := os.Remove(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// TitleFrom 取第一条用户消息的首行，最多 24 个字。
func TitleFrom(msgs []llm.Message) string {
	for _, m := range msgs {
		if m.Role != llm.RoleUser {
			continue
		}
		line, _, _ := strings.Cut(strings.TrimSpace(m.Content), "\n")
		r := []rune(line)
		if len(r) > 24 {
			return string(r[:24]) + "…"
		}
		if len(r) > 0 {
			return string(r)
		}
	}
	return "新会话"
}

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".json") }

// readMeta 只解析侧栏需要的字段：消息数组逐项跳过，不把正文读进内存。
func (s *Store) readMeta(path string) (Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	t, err := dec.Token()
	if err != nil {
		return Meta{}, err
	}
	if t != json.Delim('{') {
		return Meta{}, fmt.Errorf("会话文件不是 JSON 对象")
	}
	var meta Meta
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return Meta{}, err
		}
		name, _ := key.(string)
		switch name {
		case "id":
			if err := dec.Decode(&meta.ID); err != nil {
				return Meta{}, err
			}
		case "title":
			if err := dec.Decode(&meta.Title); err != nil {
				return Meta{}, err
			}
		case "updated":
			if err := dec.Decode(&meta.Updated); err != nil {
				return Meta{}, err
			}
		case "messages":
			n, err := countJSONArray(dec)
			if err != nil {
				return Meta{}, err
			}
			meta.MsgCount = n
		default:
			if err := skipJSONValue(dec); err != nil {
				return Meta{}, err
			}
		}
	}
	return meta, nil
}

func countJSONArray(dec *json.Decoder) (int, error) {
	t, err := dec.Token()
	if err != nil {
		return 0, err
	}
	if t != json.Delim('[') {
		if err := skipJSONRest(dec, t); err != nil {
			return 0, err
		}
		return 0, nil
	}
	n := 0
	for dec.More() {
		if err := skipJSONValue(dec); err != nil {
			return 0, err
		}
		n++
	}
	_, err = dec.Token()
	return n, err
}

func skipJSONValue(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	return skipJSONRest(dec, t)
}

func skipJSONRest(dec *json.Decoder, t json.Token) error {
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	for dec.More() {
		if delim == '{' {
			if _, err := dec.Token(); err != nil {
				return err
			}
		}
		if err := skipJSONValue(dec); err != nil {
			return err
		}
	}
	_, err := dec.Token()
	return err
}

func newID(t time.Time) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return t.Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// validID 只允许字母、数字和 -，防止路径穿越。
func validID(id string) error {
	if id == "" || len(id) > 64 {
		return fmt.Errorf("非法会话 ID: %q", id)
	}
	for _, r := range id {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return fmt.Errorf("非法会话 ID: %q", id)
		}
	}
	return nil
}
