package agent

import "github.com/ainnhuomiao/miao/internal/llm"

// SetImagesEnabled 控制图片入口；关闭后历史图片也不会发给模型。
func (a *Agent) SetImagesEnabled(enabled bool) {
	a.mu.Lock()
	a.imagesDisabled = !enabled
	if !enabled {
		a.pendingImages = nil
	}
	a.mu.Unlock()
}

func (a *Agent) ImagesEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.imagesDisabled
}

// AttachImages 设置下一次 Send 的用户消息要附带的图片（发送后清空）。
func (a *Agent) AttachImages(imgs []llm.Image) {
	a.mu.Lock()
	if a.imagesDisabled {
		imgs = nil
	}
	a.pendingImages = imgs
	a.mu.Unlock()
}

func (a *Agent) takeImagesLocked() []llm.Image {
	imgs := a.pendingImages
	a.pendingImages = nil
	return imgs
}

// visionFilter 在持有 a.mu 时调用；禁用图片或模型不支持时过滤历史图片。
func (a *Agent) visionFilter(msgs []llm.Message) []llm.Message {
	if !a.imagesDisabled && llm.SupportsVision(a.Model()) {
		return msgs
	}
	return llm.StripImages(msgs)
}

// MentionedImages 返回用户消息里 @引用的工作区图片，见 tools.Registry.MentionedImages。
func (a *Agent) MentionedImages(text string) ([]llm.Image, []string) {
	if a == nil || a.tools == nil || !a.ImagesEnabled() {
		return nil, nil
	}
	return a.tools.MentionedImages(text)
}
