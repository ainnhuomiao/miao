package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ainnhuomiao/miao/internal/usercmd"
)

// userCmdState 缓存自定义命令，几秒内不重复读目录。
type userCmdState struct {
	cmds   []usercmd.Command
	loaded time.Time
}

// builtinCommands 返回内置命令名（不含 /），自定义命令不能占用。
func (m *Model) builtinCommands() map[string]bool {
	out := map[string]bool{"quit": true, "exit": true}
	for _, c := range m.builtinPalette() {
		out[strings.TrimPrefix(c.name, "/")] = true
	}
	return out
}

func (m *Model) userCommands() []usercmd.Command {
	if m.userCmds.cmds != nil && time.Since(m.userCmds.loaded) < 3*time.Second {
		return m.userCmds.cmds
	}
	cmds := usercmd.Load(m.ag.Workspace(), m.builtinCommands())
	if cmds == nil {
		cmds = []usercmd.Command{}
	}
	m.userCmds = userCmdState{cmds: cmds, loaded: time.Now()}
	return cmds
}

// userPalette 把自定义命令变成菜单行，排在内置命令后面。
func (m *Model) userPalette() []paletteCmd {
	var out []paletteCmd
	for _, c := range m.userCommands() {
		out = append(out, paletteCmd{
			name:    "/" + c.Name,
			desc:    c.Description + "（" + c.Source + "命令）",
			needArg: c.TakesArgs(),
		})
	}
	return out
}

// runUserCommand 执行自定义命令：界面显示命令原文，发给模型的是展开后的模板。
func (m *Model) runUserCommand(text string) (tea.Cmd, bool) {
	name, args, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	for _, c := range m.userCommands() {
		if c.Name != name {
			continue
		}
		if m.streaming {
			m.notice("正在生成回复，稍后再用 /" + name + "。")
			return nil, true
		}
		return m.sendMessage(text, c.Expand(args)), true
	}
	return nil, false
}
