package tools

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMentionedImagesAttachAsImage(t *testing.T) {
	dir := t.TempDir()
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 64)...)
	os.WriteFile(filepath.Join(dir, "图片.png"), png, 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644)
	os.WriteFile(filepath.Join(dir, "big.png"), append(append([]byte{}, png...), make([]byte, 5<<20)...), 0o644)
	r, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	text := "看看 @图片.png 和 @a.txt 还有 @big.png"
	imgs, warns := r.MentionedImages(text)
	if len(imgs) != 1 || imgs[0].Name != "图片.png" || imgs[0].MIME != "image/png" {
		t.Fatalf("应附加一张图片：%+v", imgs)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "big.png") {
		t.Fatalf("超大图片应有警告：%v", warns)
	}
	_, attached, w2 := r.ExpandMentions(text)
	if len(attached) != 1 || attached[0] != "a.txt" {
		t.Fatalf("文本文件照常附加：%v", attached)
	}
	for _, w := range w2 {
		if strings.Contains(w, "png") {
			t.Fatalf("图片不应再报“不是文本文件”：%v", w2)
		}
	}
}
