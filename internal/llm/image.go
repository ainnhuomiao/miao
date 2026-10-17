package llm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ainnhuomiao/miao/internal/config"
)

// 图片限制：单张 5 MiB，每条消息最多 5 张。
const (
	MaxImageBytes = 5 << 20
	MaxImages     = 5
	// ImageTokens 是估算上下文时每张图片按的固定 token 数。
	ImageTokens = 1000
)

// Image 是一张随用户消息发送的图片。
// Data 只在内存里；会话文件里只存 Path（相对会话目录的引用，如 images/<sha>.png）。
type Image struct {
	MIME string
	Name string
	Data []byte
	Path string
}

var supportedImageTypes = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/gif":  "gif",
	"image/webp": "webp",
}

// DetectImage 按内容识别图片类型（png/jpeg/gif/webp），不是支持的图片时 ok=false。
func DetectImage(data []byte) (mime string, ok bool) {
	mt := http.DetectContentType(data)
	_, ok = supportedImageTypes[mt]
	return mt, ok
}

// ImageExt 返回 MIME 对应的扩展名（不带点）。
func ImageExt(mime string) string {
	if e, ok := supportedImageTypes[mime]; ok {
		return e
	}
	return "bin"
}

// NewImage 校验内容与大小并构造图片。
func NewImage(name string, data []byte) (Image, error) {
	if len(data) > MaxImageBytes {
		return Image{}, fmt.Errorf("图片 %s 超过 %dMiB（%s），已跳过", name, MaxImageBytes>>20, FormatSize(len(data)))
	}
	mt, ok := DetectImage(data)
	if !ok {
		return Image{}, fmt.Errorf("%s 不是支持的图片（png/jpeg/gif/webp）", name)
	}
	return Image{MIME: mt, Name: name, Data: data}, nil
}

// FormatSize 把字节数格式化成 120KB / 1.5MB。
func FormatSize(n int) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%dKB", (n+1023)/1024)
}

// DataURL 返回 data:<mime>;base64,... 形式的地址。
func (img Image) DataURL() string {
	return "data:" + img.MIME + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
}

type imageURL struct {
	URL string `json:"url"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

// MarshalJSON：没有图片时与原来一致（content 是字符串）；有图片时 content 是 OpenAI 兼容的 parts 数组。
func (m Message) MarshalJSON() ([]byte, error) {
	type plain Message
	if len(m.Images) == 0 {
		return json.Marshal(plain(m))
	}
	parts := make([]contentPart, 0, len(m.Images)+1)
	if m.Content != "" {
		parts = append(parts, contentPart{Type: "text", Text: m.Content})
	}
	for _, img := range m.Images {
		if len(img.Data) == 0 {
			continue
		}
		parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: img.DataURL()}})
	}
	return json.Marshal(struct {
		plain
		Content []contentPart `json:"content"`
	}{plain(m), parts})
}

// visionEnv 可强制开关图片支持：MIAO_VISION=1 / 0（旧名 AGNET_VISION 仍可用）。
const visionEnv = "VISION"

// noVisionModels 是已知不支持图片输入的模型名前缀；不在表里的一律允许。
var noVisionModels = []string{
	"deepseek-chat", "deepseek-reasoner", "deepseek-coder", "deepseek-v3", "deepseek-r1",
	"gpt-3.5", "o1-mini", "o3-mini", "qwen-turbo", "qwen-plus", "qwen-max", "qwen3-coder",
	"kimi-k2", "moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k", "glm-4.5", "glm-4.6",
}

// SupportsVision 报告模型是否可以接收图片（未知模型视为支持）。
func SupportsVision(model string) bool {
	switch config.Getenv(visionEnv) {
	case "1":
		return true
	case "0":
		return false
	}
	name := strings.ToLower(model)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	for _, p := range noVisionModels {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return true
}

// StripImages 返回去掉所有图片的消息副本（模型不支持图片时用）。
func StripImages(msgs []Message) []Message {
	out := msgs
	for i, m := range msgs {
		if len(m.Images) == 0 {
			continue
		}
		if &out[0] == &msgs[0] {
			out = append([]Message(nil), msgs...)
		}
		out[i].Images = nil
	}
	return out
}
