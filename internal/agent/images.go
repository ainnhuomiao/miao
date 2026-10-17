package agent

import "github.com/ainnhuomiao/miao/internal/llm"

// AttachImages 设置下一次 Send 的用户消息要附带的图片（发送后清空）。
func (a *Agent) AttachImages(imgs []llm.Image) { a.pendingImages = imgs }

func (a *Agent) takeImages() []llm.Image {
	imgs := a.pendingImages
	a.pendingImages = nil
	return imgs
}

// visionFilter：当前模型已知不支持图片时，请求里去掉历史消息中的图片（切换模型后不会报错）。
func (a *Agent) visionFilter(msgs []llm.Message) []llm.Message {
	if llm.SupportsVision(a.Model()) {
		return msgs
	}
	return llm.StripImages(msgs)
}

// MentionedImages 返回用户消息里 @引用的工作区图片，见 tools.Registry.MentionedImages。
func (a *Agent) MentionedImages(text string) ([]llm.Image, []string) {
	if a == nil || a.tools == nil {
		return nil, nil
	}
	return a.tools.MentionedImages(text)
}
