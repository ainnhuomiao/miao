package agent

import "strings"

// ModelPlaceholder 在系统提示词里代表当前模型名，每次请求时替换，
// 这样 /model 切换后提示词里的模型标识也会跟着变。
const ModelPlaceholder = "{{MODEL}}"

// systemPrompt 返回本次请求实际使用的系统提示词。
func (a *Agent) systemPrompt() string {
	if !strings.Contains(a.system, ModelPlaceholder) {
		return a.system
	}
	model := a.Model()
	if model == "" {
		model = "未知"
	}
	return strings.ReplaceAll(a.system, ModelPlaceholder, model)
}
