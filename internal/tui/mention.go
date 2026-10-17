package tui

import (
	"context"
	"path"
	"strings"
	"time"
)

// mentionMaxList 是 @ 补全最多扫描的文件数。
const mentionMaxList = 5000

// mentionState 缓存 @ 补全用的文件列表，几秒内重复输入不重新扫描。
type mentionState struct {
	files  []string
	loaded time.Time
}

// atMode 判断输入的最后一个词是否是正在输入的 @引用；query 是 @ 后面的内容。
func (m *Model) atMode() (query string, ok bool) {
	v := m.input.Value()
	if v == "" || strings.HasPrefix(v, "/") {
		return "", false
	}
	if last := v[len(v)-1]; last == ' ' || last == '\t' || last == '\n' {
		return "", false
	}
	i := strings.LastIndexAny(v, " \t\n")
	tok := v[i+1:]
	if !strings.HasPrefix(tok, "@") || strings.Count(tok, "@") > 1 {
		return "", false
	}
	return tok[1:], true
}

func (m *Model) mentionFiles() []string {
	if m.mention.files != nil && time.Since(m.mention.loaded) < 5*time.Second {
		return m.mention.files
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	files, _ := m.ag.ListFiles(ctx, mentionMaxList)
	if files == nil {
		files = []string{}
	}
	m.mention = mentionState{files: files, loaded: time.Now()}
	return files
}

// atMatches 按文件名前缀、路径前缀、路径包含、子序列的顺序排序。
func (m *Model) atMatches(query string) []paletteCmd {
	q := strings.ToLower(query)
	var b0, b1, b2, b3 []paletteCmd
	for _, f := range m.mentionFiles() {
		lf := strings.ToLower(f)
		c := paletteCmd{name: f}
		switch {
		case q == "" || strings.HasPrefix(strings.ToLower(path.Base(f)), q):
			b0 = append(b0, c)
		case strings.HasPrefix(lf, q):
			b1 = append(b1, c)
		case strings.Contains(lf, q):
			b2 = append(b2, c)
		case isSubsequence(q, lf):
			b3 = append(b3, c)
		}
		if len(b0) >= 200 {
			break
		}
	}
	out := append(append(append(b0, b1...), b2...), b3...)
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

// atComplete 把正在输入的 @词替换成选中的路径，后面补一个空格。
func (m *Model) atComplete(c paletteCmd) {
	v := m.input.Value()
	i := strings.LastIndexAny(v, " \t\n")
	m.input.SetValue(v[:i+1] + "@" + c.name + " ")
	m.input.CursorEnd()
	m.fitInput()
	m.syncPalette()
}

// expandMentions 发送前把 @引用的文件内容附进消息；界面上仍显示原文。
func (m *Model) expandMentions(text string) string {
	expanded, attached, warnings := m.ag.ExpandMentions(text)
	if len(attached) > 0 {
		m.notice("已附加 " + strings.Join(attached, "、"))
	}
	for _, w := range warnings {
		m.notice(w)
	}
	return expanded
}
