package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"

	"agnet/internal/llm"
)

// 会话里的图片不把 base64 写进 JSON，而是存成会话目录下的 images/<sha256>.<ext>，
// 消息里只记引用 {"path":"images/…","mime":…,"name":…}。同内容的图片只存一份；
// 删除会话不会删图片（可能被 /fork 出的会话共用）。

const imagesDir = "images"

type imageRef struct {
	Path string `json:"path"`
	MIME string `json:"mime"`
	Name string `json:"name,omitempty"`
}

type plainMessage llm.Message // 去掉 MarshalJSON，content 保持字符串

type diskMessage struct {
	plainMessage
	Images []imageRef `json:"images,omitempty"`
}

type sessionAlias Session

type diskSession struct {
	*sessionAlias
	Messages []diskMessage `json:"messages"`
}

// diskForm 把图片写成文件并返回落盘用的结构；写失败的图片不记引用。
func (s *Store) diskForm(sess *Session) diskSession {
	out := diskSession{sessionAlias: (*sessionAlias)(sess), Messages: make([]diskMessage, len(sess.Messages))}
	for i, m := range sess.Messages {
		out.Messages[i].plainMessage = plainMessage(m)
		for j, img := range m.Images {
			ref, err := s.storeImage(img)
			if err != nil {
				continue
			}
			sess.Messages[i].Images[j].Path = ref.Path
			out.Messages[i].Images = append(out.Messages[i].Images, ref)
		}
	}
	return out
}

func (s *Store) storeImage(img llm.Image) (imageRef, error) {
	if img.Path != "" && len(img.Data) == 0 {
		return imageRef{Path: img.Path, MIME: img.MIME, Name: img.Name}, nil
	}
	sum := sha256.Sum256(img.Data)
	rel := path.Join(imagesDir, hex.EncodeToString(sum[:16])+"."+llm.ImageExt(img.MIME))
	full := filepath.Join(s.dir, filepath.FromSlash(rel))
	if _, err := os.Stat(full); err != nil {
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return imageRef{}, err
		}
		tmp := full + ".tmp"
		if err := os.WriteFile(tmp, img.Data, 0o644); err != nil {
			return imageRef{}, err
		}
		if err := os.Rename(tmp, full); err != nil {
			return imageRef{}, err
		}
	}
	return imageRef{Path: rel, MIME: img.MIME, Name: img.Name}, nil
}

// loadImages 按引用读回图片；文件缺失或路径不合法的引用跳过。
func (s *Store) loadImages(sess *Session, data []byte) {
	var raw struct {
		Messages []struct {
			Images []imageRef `json:"images"`
		} `json:"messages"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	for i, m := range raw.Messages {
		if i >= len(sess.Messages) {
			break
		}
		for _, ref := range m.Images {
			clean := path.Clean(ref.Path)
			if !strings.HasPrefix(clean, imagesDir+"/") || strings.Contains(clean, "..") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(s.dir, filepath.FromSlash(clean)))
			if err != nil {
				continue
			}
			sess.Messages[i].Images = append(sess.Messages[i].Images, llm.Image{MIME: ref.MIME, Name: ref.Name, Data: b, Path: clean})
		}
	}
}
