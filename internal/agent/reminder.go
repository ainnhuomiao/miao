package agent

import (
	"fmt"
	"strings"

	"github.com/ainnhuomiao/miao/internal/llm"
)

// ReminderPrefix 标记由 miao 注入的 <system-reminder>，不是用户发言。
const ReminderPrefix = "<system-reminder>"

func wrapReminder(body string) string {
	return ReminderPrefix + "\n" + strings.TrimSpace(body) + "\n</system-reminder>"
}

func isReminder(m llm.Message) bool {
	if m.Role != llm.RoleUser {
		return false
	}
	c := strings.TrimSpace(m.Content)
	return strings.HasPrefix(c, ReminderPrefix) || strings.HasPrefix(c, "（已切换到「")
}

func isBaselineReminder(m llm.Message) bool {
	return isReminder(m) && strings.Contains(m.Content, "工作区根目录")
}

// SetProjectContext 记下工作区说明用的 AGENTS.md 和 -system 附加内容。
// 不改系统提示词；第一次 Send 时写入会话基线 reminder。
func (a *Agent) SetProjectContext(content string, truncated bool, extra string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.projectMD = strings.TrimSpace(content)
	a.projectTrunc = truncated
	a.extraContext = strings.TrimSpace(extra)
	a.agentsMD = a.projectMD != ""
}

func (a *Agent) injectBaselineLocked() {
	if a.hasBaselineLocked() {
		return
	}
	note := a.baselineReminderLocked()
	if note == "" {
		return
	}
	a.history = append([]llm.Message{{Role: llm.RoleUser, Content: note}}, a.history...)
}

func (a *Agent) hasBaselineLocked() bool {
	for _, m := range a.history {
		if isBaselineReminder(m) {
			return true
		}
	}
	return false
}

func (a *Agent) baselineReminderLocked() string {
	var b strings.Builder
	if ws := a.Workspace(); ws != "" {
		fmt.Fprintf(&b, "工作区根目录是 %q。工具只能访问此目录内的相对路径。go test / go build 会编译并运行仓库里的代码。\n", ws)
	} else if a.projectMD != "" || a.extraContext != "" {
		b.WriteString("工作区根目录尚未设置。\n")
	}
	if block := agentsMDBlock(a.projectMD, a.projectTrunc); block != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(block)
		b.WriteByte('\n')
	}
	if a.extraContext != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(a.extraContext)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		return ""
	}
	return wrapReminder(b.String())
}
