package tools

import (
	"fmt"
	"os"
	"strings"

	"miao/internal/llm"
)

func isImageMention(data []byte) bool {
	_, ok := llm.DetectImage(data)
	return ok
}

// MentionedImages 返回文本里 @引用的工作区图片（png/jpeg/gif/webp，按内容识别），
// warnings 是跳过的原因（超过大小、越界符号链接、敏感文件）。数量上限由调用方统一检查。
func (r *Registry) MentionedImages(text string) (imgs []llm.Image, warnings []string) {
	for _, p := range MentionedPaths(text) {
		full, err := r.ResolvePath(p)
		if err != nil {
			continue
		}
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			continue
		}
		rel := strings.TrimPrefix(p, "./")
		if err := r.checkNoSymlinkEscape(full); err != nil {
			continue // ExpandMentions 已经报过警告
		}
		if isSensitive(rel) {
			continue
		}
		head := make([]byte, 512)
		f, err := os.Open(full)
		if err != nil {
			continue
		}
		n, _ := f.Read(head)
		f.Close()
		if !isImageMention(head[:n]) {
			continue
		}
		if info.Size() > llm.MaxImageBytes {
			warnings = append(warnings, fmt.Sprintf("@%s 未附加：图片超过 %dMiB", rel, llm.MaxImageBytes>>20))
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("@%s 未附加：%v", rel, err))
			continue
		}
		img, err := llm.NewImage(rel, data)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		imgs = append(imgs, img)
	}
	return imgs, warnings
}
