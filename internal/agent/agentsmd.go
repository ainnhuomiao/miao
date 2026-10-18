package agent

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// AgentsMDName 是工作区根目录下的项目记忆文件名。
const AgentsMDName = "AGENTS.md"

// MaxAgentsMDBytes 是读取 AGENTS.md 的上限（32KiB），超出部分截断。
const MaxAgentsMDBytes = 32 * 1024

// LoadAgentsMD 读取 workspace 根目录的 AGENTS.md。
// 文件不存在或内容为空时返回 ("", false, nil)。
// 超过 MaxAgentsMDBytes 时按 UTF-8 边界截断，truncated 为 true。
func LoadAgentsMD(workspace string) (content string, truncated bool, err error) {
	if workspace == "" {
		return "", false, nil
	}
	f, err := os.Open(filepath.Join(workspace, AgentsMDName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.IsDir() {
		return "", false, nil
	}
	buf, err := io.ReadAll(io.LimitReader(f, MaxAgentsMDBytes+1))
	if err != nil {
		return "", false, err
	}
	if len(buf) > MaxAgentsMDBytes {
		buf = buf[:MaxAgentsMDBytes]
		for len(buf) > 0 && !utf8.Valid(buf) {
			buf = buf[:len(buf)-1]
		}
		truncated = true
	}
	return strings.TrimSpace(string(buf)), truncated, nil
}

// WithAgentsMD 把 AGENTS.md 内容拼到系统提示词末尾。content 为空时原样返回。
func WithAgentsMD(system, content string, truncated bool) string {
	if content == "" {
		return system
	}
	note := ""
	if truncated {
		note = fmt.Sprintf("（文件超过 %dKiB，已截断）", MaxAgentsMDBytes/1024)
	}
	return fmt.Sprintf("%s\n\n以下是项目根目录 %s 中的项目说明%s，请遵守：\n<agents_md>\n%s\n</agents_md>",
		system, AgentsMDName, note, content)
}

const agentsMDMarker = "\n\n以下是项目根目录 "

// ApplyAgentsMD 把 AGENTS.md 写进当前会话的系统提示词（去掉旧块再拼上）。
func (a *Agent) ApplyAgentsMD(content string, truncated bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	base := a.system
	if i := strings.Index(base, agentsMDMarker); i >= 0 {
		base = strings.TrimRight(base[:i], "\n")
	}
	a.system = WithAgentsMD(base, content, truncated)
	a.agentsMD = strings.TrimSpace(content) != ""
}

// SetAgentsMDLoaded 标记启动时是否加载了 AGENTS.md，供界面提示。
func (a *Agent) SetAgentsMDLoaded(v bool) { a.agentsMD = v }

// AgentsMDLoaded 报告启动时是否加载了 AGENTS.md。
func (a *Agent) AgentsMDLoaded() bool { return a != nil && a.agentsMD }
