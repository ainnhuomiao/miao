package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"miao/internal/agent"
	"miao/internal/llm"
)

const (
	doubleEscWindow = 500 * time.Millisecond
	rewindMaxRows   = 8
)

// rewindPoint 记录发送一条用户消息时的状态，回退时用来还原文件。
type rewindPoint struct {
	histIdx int    // 这条用户消息在 Agent 历史里的下标
	depth   int    // 发送前的撤销栈深度
	text    string // 用户原文（不含 @ 附加的文件内容）
}

type rewindState struct {
	points  []rewindPoint
	lastEsc time.Time
	open    bool
	items   []rewindItem
	sel     int
}

type rewindItem struct {
	histIdx int
	text    string
}

// markRewindPoint 在发送用户消息前调用。
func (m *Model) markRewindPoint(text string) {
	m.rewind.points = append(m.rewind.points, rewindPoint{
		histIdx: len(m.ag.History()), depth: m.ag.UndoDepth(), text: text,
	})
}

// resetRewindPoints 在切换会话或压缩后调用：历史下标已失效。
func (m *Model) resetRewindPoints() {
	m.rewind.points = nil
	m.rewind.open = false
}

func (m *Model) rewindPointAt(idx int) (rewindPoint, bool) {
	for _, p := range m.rewind.points {
		if p.histIdx == idx {
			return p, true
		}
	}
	return rewindPoint{}, false
}

// rewindKey 处理 Esc Esc 和回退菜单的按键；返回 false 交给常规处理。
func (m *Model) rewindKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	k := msg.String()
	if m.rewind.open {
		switch k {
		case "up", "ctrl+p":
			m.rewind.sel = max(0, m.rewind.sel-1)
		case "down", "ctrl+n":
			m.rewind.sel = min(len(m.rewind.items)-1, m.rewind.sel+1)
		case "enter":
			it := m.rewind.items[m.rewind.sel]
			m.closeRewind()
			m.rewindTo(it)
		case "esc", "ctrl+c":
			m.closeRewind()
		}
		return true, nil
	}
	if k != "esc" || m.streaming || m.paletteOpen() || strings.TrimSpace(m.input.Value()) != "" {
		m.rewind.lastEsc = time.Time{}
		return false, nil
	}
	now := time.Now()
	if !m.rewind.lastEsc.IsZero() && now.Sub(m.rewind.lastEsc) <= doubleEscWindow {
		m.rewind.lastEsc = time.Time{}
		m.openRewind()
		return true, nil
	}
	m.rewind.lastEsc = now
	return false, nil
}

func (m *Model) openRewind() {
	var items []rewindItem
	for i, msg := range m.ag.History() {
		if msg.Role != llm.RoleUser || strings.HasPrefix(msg.Content, agent.SummaryPrefix) {
			continue
		}
		text := msg.Content
		if p, ok := m.rewindPointAt(i); ok {
			text = p.text
		}
		items = append(items, rewindItem{histIdx: i, text: text})
	}
	if len(items) == 0 {
		m.notice("还没有可以回退的消息。")
		return
	}
	// 最新的在最上面
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	m.rewind.items, m.rewind.sel, m.rewind.open = items, 0, true
	m.layout()
}

func (m *Model) closeRewind() {
	m.rewind.open = false
	m.layout()
}

func (m *Model) rewindHeight() int {
	if !m.rewind.open {
		return 0
	}
	return min(len(m.rewind.items), rewindMaxRows) + 1
}

func (m *Model) rewindView() string {
	w := max(10, m.columnWidth())
	pad := lipgloss.NewStyle().MarginLeft(m.columnMargin())
	mauve := lipgloss.NewStyle().Foreground(accentColor)
	text := lipgloss.NewStyle().Foreground(textColor)
	start := max(0, m.rewind.sel-rewindMaxRows+1)
	lines := []string{" " + mutedStyle.Render("回到哪条消息重新编辑？ ↑↓ 选择 · Enter 回退 · Esc 取消")}
	for i := start; i < len(m.rewind.items) && i < start+rewindMaxRows; i++ {
		first, _, _ := strings.Cut(strings.TrimSpace(m.rewind.items[i].text), "\n")
		label := truncateWidth(first, w-4)
		if i == m.rewind.sel {
			lines = append(lines, " "+mauve.Render("›")+" "+text.Render(label))
		} else {
			lines = append(lines, "   "+mutedStyle.Render(label))
		}
	}
	return pad.Render(strings.Join(lines, "\n"))
}

// rewindTo 把对话截到这条用户消息之前，还原之后的文件改动，并把原文放回输入框。
func (m *Model) rewindTo(it rewindItem) {
	if m.streaming {
		m.notice("正在生成，稍后再回退。")
		return
	}
	hist := m.ag.History()
	if it.histIdx >= len(hist) {
		return
	}
	restored, fileNote := 0, ""
	if p, ok := m.rewindPointAt(it.histIdx); ok {
		for m.ag.UndoDepth() > p.depth {
			if _, err := m.ag.Undo(); err != nil {
				fileNote = "，部分文件没能还原：" + err.Error()
				break
			}
			restored++
		}
	} else if len(sessionEditedFiles(hist[it.histIdx:])) > 0 {
		fileNote = "，这段对话的文件改动没有快照记录，未还原"
	}

	kept := append([]llm.Message(nil), hist[:it.histIdx]...)
	m.ag.SetHistory(kept)
	var points []rewindPoint
	for _, p := range m.rewind.points {
		if p.histIdx < it.histIdx {
			points = append(points, p)
		}
	}
	m.rewind.points = points
	if m.sess != nil {
		m.sess.Messages = kept
		m.messages = chatMessagesFromHistory(m.sess)
	} else {
		m.messages = nil
	}
	m.ctxTokens = 0
	m.input.SetValue(it.text)
	m.input.CursorEnd()
	m.fitInput()
	note := "已回到这条消息，修改后重新发送"
	if restored > 0 {
		note += fmt.Sprintf("（还原了 %d 处文件改动）", restored)
	}
	m.notice(note + fileNote)
	m.saveCurrentSession()
	m.layout()
	m.refreshViewport(true)
}

// forkSession 实现 /fork：把当前对话复制成一个新会话并切换过去，原会话保留。
func (m *Model) forkSession() {
	if m.streaming {
		m.notice("正在生成，稍后再分支。")
		return
	}
	if m.store == nil || m.sess == nil {
		m.notice("会话存储不可用。")
		return
	}
	hist := m.ag.History()
	if len(hist) == 0 {
		m.notice("当前对话还是空的，不用分支。")
		return
	}
	m.saveCurrentSession()
	orig := m.sess.Title
	if orig == "" {
		orig = "新会话"
	}
	ns := m.store.New(m.modelName, m.ag.Workspace())
	ns.Messages = append([]llm.Message(nil), hist...)
	ns.Title = strings.TrimSuffix(orig, " · 分支") + " · 分支"
	if err := m.store.Save(ns); err != nil {
		m.notice("分支失败：" + err.Error())
		return
	}
	m.sess = ns
	m.syncSnapshotDir()
	m.resetRewindPoints()
	m.notice("已复制为新会话「" + ns.Title + "」，原会话保留在侧栏里。")
	m.refreshSidebar()
}
