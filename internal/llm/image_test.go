package llm

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

var testPNG = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)

func TestMessageMarshalStringWithoutImages(t *testing.T) {
	b, err := json.Marshal(Message{Role: RoleUser, Content: "你好"})
	if err != nil || string(b) != `{"role":"user","content":"你好"}` {
		t.Fatalf("%s %v", b, err)
	}
}

func TestMessageMarshalParts(t *testing.T) {
	img, err := NewImage("a.png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(Message{Role: RoleUser, Content: "看图", Images: []Image{img}})
	var got struct {
		Role    string `json:"role"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		} `json:"content"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	if got.Role != "user" || len(got.Content) != 2 || got.Content[0].Type != "text" || got.Content[0].Text != "看图" ||
		got.Content[1].Type != "image_url" || !strings.HasPrefix(got.Content[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("%s", b)
	}
	if strings.Contains(string(b), `"Images"`) {
		t.Fatal("Images 不应直接序列化")
	}
}

func TestNewImageLimits(t *testing.T) {
	if _, err := NewImage("x.txt", []byte("hello world")); err == nil {
		t.Fatal("文本不是图片")
	}
	big := append(append([]byte{}, testPNG...), make([]byte, MaxImageBytes)...)
	if _, err := NewImage("big.png", big); err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("应拒绝超大图片：%v", err)
	}
	for _, d := range [][]byte{[]byte("\xff\xd8\xff\xe0xxxx"), []byte("GIF89a......"), []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")} {
		if _, ok := DetectImage(d); !ok {
			t.Fatalf("应识别 %q", d[:4])
		}
	}
}

func TestSupportsVision(t *testing.T) {
	t.Setenv("AGNET_VISION", "")
	for m, want := range map[string]bool{"deepseek-chat": false, "deepseek-reasoner": false, "deepseek/deepseek-chat": false, "gpt-4o": true, "unknown-x": true} {
		if SupportsVision(m) != want {
			t.Fatalf("%s 期望 %v", m, want)
		}
	}
	t.Setenv("AGNET_VISION", "1")
	if !SupportsVision("deepseek-chat") {
		t.Fatal("AGNET_VISION=1 应强制开启")
	}
	t.Setenv("AGNET_VISION", "0")
	if SupportsVision("gpt-4o") {
		t.Fatal("AGNET_VISION=0 应强制关闭")
	}
}

func TestStripImagesCopies(t *testing.T) {
	in := []Message{{Role: RoleUser, Content: "a", Images: []Image{{MIME: "image/png", Data: testPNG}}}}
	out := StripImages(in)
	if len(out[0].Images) != 0 || len(in[0].Images) != 1 {
		t.Fatal("应返回去图副本且不改原切片")
	}
}
