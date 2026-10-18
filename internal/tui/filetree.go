package tui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ainnhuomiao/miao/internal/childenv"
	"github.com/ainnhuomiao/miao/internal/tools"
)

const treePreviewLines = 200

// fileChange 是一个文件相对 HEAD 的改动。
type fileChange struct {
	code     byte // 'M' 修改、'A' 新增（含未跟踪）、'D' 删除
	add, del int
}

type treeNode struct {
	path     string // 相对工作区，/ 分隔；根为 ""
	name     string
	dir      bool
	expanded bool
	loaded   bool
	children []*treeNode
}

type treeRow struct {
	n     *treeNode
	depth int
}

// treeState 是文件树侧栏的状态（挂在 sidebarState 里）。
type treeState struct {
	root     *treeNode
	changes  map[string]fileChange
	dirs     map[string]bool // 含改动的目录
	sel      int
	confirm  string // 等待确认撤销的文件
	preview  []string
	prevHead string
}

func (m *Model) workspaceDir() string {
	if m.ag == nil || m.ag.Workspace() == "" {
		return "."
	}
	return m.ag.Workspace()
}

// refreshTree 重新读取 git 改动，保留已展开的目录。
func (m *Model) refreshTree() {
	t := &m.side.tree
	dir := m.workspaceDir()
	t.changes = gitChanges(dir)
	t.dirs = map[string]bool{"": true}
	for p := range t.changes {
		for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
			t.dirs[d] = true
		}
	}
	expanded := map[string]bool{}
	if t.root != nil {
		var walk func(n *treeNode)
		walk = func(n *treeNode) {
			if n.expanded {
				expanded[n.path] = true
			}
			for _, c := range n.children {
				walk(c)
			}
		}
		walk(t.root)
	}
	t.root = &treeNode{dir: true, expanded: true}
	var load func(n *treeNode)
	load = func(n *treeNode) {
		m.loadTreeDir(n)
		for _, c := range n.children {
			if c.dir && (t.dirs[c.path] || expanded[c.path]) {
				c.expanded = true
				load(c)
			}
		}
	}
	load(t.root)
	t.sel = min(t.sel, max(0, len(m.treeRows())-1))
}

// loadTreeDir 读目录（只在展开时调用），跳过 .git 和 git 忽略的文件，并补上已删除的文件。
func (m *Model) loadTreeDir(n *treeNode) {
	if n.loaded {
		return
	}
	n.loaded = true
	dir := m.workspaceDir()
	ents, _ := os.ReadDir(filepath.Join(dir, filepath.FromSlash(n.path)))
	var paths []string
	byPath := map[string]*treeNode{}
	for _, e := range ents {
		if e.Name() == ".git" {
			continue
		}
		p := path.Join(n.path, e.Name())
		c := &treeNode{path: p, name: e.Name(), dir: e.IsDir()}
		byPath[p] = c
		paths = append(paths, p)
	}
	for p := range gitIgnored(dir, paths) {
		delete(byPath, p)
	}
	for p, ch := range m.side.tree.changes {
		if ch.code == 'D' && path.Dir(p) == orDot(n.path) && byPath[p] == nil {
			byPath[p] = &treeNode{path: p, name: path.Base(p)}
		}
	}
	n.children = n.children[:0]
	for _, c := range byPath {
		n.children = append(n.children, c)
	}
	sort.Slice(n.children, func(i, j int) bool {
		a, b := n.children[i], n.children[j]
		if a.dir != b.dir {
			return a.dir
		}
		return a.name < b.name
	})
}

func orDot(p string) string {
	if p == "" {
		return "."
	}
	return p
}

func (m *Model) treeRows() []treeRow {
	var rows []treeRow
	var walk func(n *treeNode, depth int)
	walk = func(n *treeNode, depth int) {
		for _, c := range n.children {
			rows = append(rows, treeRow{c, depth})
			if c.dir && c.expanded {
				walk(c, depth+1)
			}
		}
	}
	if m.side.tree.root != nil {
		walk(m.side.tree.root, 0)
	}
	return rows
}

func (m *Model) selectedTreeNode() (*treeNode, bool) {
	rows := m.treeRows()
	if m.side.tree.sel < 0 || m.side.tree.sel >= len(rows) {
		return nil, false
	}
	return rows[m.side.tree.sel].n, true
}

// treeKey 处理文件树聚焦时的按键。
func (m *Model) treeKey(k string) tea.Cmd {
	t := &m.side.tree
	if t.confirm != "" {
		p := t.confirm
		t.confirm = ""
		if k == "y" || k == "Y" {
			if err := m.ag.UndoFile(p); err != nil {
				m.notice("撤销失败: " + err.Error())
			} else {
				m.notice("已撤销 " + p + " 的改动。")
				m.refreshTree()
			}
		}
		return nil
	}
	rows := m.treeRows()
	switch k {
	case "up", "k":
		t.sel = max(0, t.sel-1)
	case "down", "j":
		t.sel = min(len(rows)-1, t.sel+1)
	case "right", "l", "enter":
		n, ok := m.selectedTreeNode()
		if !ok {
			return nil
		}
		if n.dir {
			if k == "enter" && n.expanded {
				n.expanded = false
			} else {
				m.loadTreeDir(n)
				n.expanded = true
			}
			return nil
		}
		if k == "enter" {
			m.previewTreeFile(n.path)
		}
	case "left", "h":
		n, ok := m.selectedTreeNode()
		if !ok {
			return nil
		}
		if n.dir && n.expanded {
			n.expanded = false
			return nil
		}
		parent := path.Dir(n.path)
		for i, r := range rows {
			if r.n.path == parent {
				t.sel = i
			}
		}
	case "u":
		n, ok := m.selectedTreeNode()
		if !ok || n.dir {
			return nil
		}
		if !m.ag.HasFileSnapshot(n.path) {
			m.notice(n.path + " 没有本会话的修改快照，不能撤销。")
			return nil
		}
		t.confirm = n.path
	case "@":
		n, ok := m.selectedTreeNode()
		if !ok {
			return nil
		}
		ref := "@" + n.path
		if n.dir {
			ref += "/"
		}
		v := m.input.Value()
		if v != "" && !strings.HasSuffix(v, " ") && !strings.HasSuffix(v, "\n") {
			ref = " " + ref
		}
		m.input.InsertString(ref + " ")
		m.side.focus = false
		m.input.Focus()
	}
	return nil
}

// previewTreeFile 在抽屉里显示改动文件的 diff，或未改动文件的只读预览。
func (m *Model) previewTreeFile(p string) {
	t := &m.side.tree
	dir := m.workspaceDir()
	var lines []string
	if ch, ok := t.changes[p]; ok {
		t.prevHead = fmt.Sprintf("%s · +%d −%d", p, ch.add, ch.del)
		if tools.IsSensitivePath(p) {
			lines = []string{"敏感文件，不显示内容。"}
		} else {
			diff := fileDiff(dir, p, ch)
			lines = formatDiffLines(diff)
			if len(lines) > 0 && lines[0] == "" {
				lines = lines[1:]
			}
			if len(lines) > 0 && strings.HasPrefix(lines[0], "── ") {
				lines = lines[1:]
			}
		}
	} else {
		t.prevHead = p + " · 只读预览"
		body, more, err := tools.PreviewFile(dir, p, treePreviewLines)
		if err != nil {
			lines = []string{err.Error()}
		} else {
			w := len(strconv.Itoa(len(body)))
			for i, l := range body {
				lines = append(lines, fmt.Sprintf("  %*d  %s", w, i+1, strings.ReplaceAll(l, "\t", "    ")))
			}
			if more {
				lines = append(lines, fmt.Sprintf("  ⋯ 只显示前 %d 行", treePreviewLines))
			}
			if len(body) == 0 {
				lines = []string{"(空文件)"}
			}
		}
	}
	if len(lines) == 0 {
		lines = []string{"(没有内容差异)"}
	}
	t.preview = lines
	m.refreshViewport(false)
	m.viewport.GotoTop()
}

func (m *Model) closeTreePreview() {
	m.side.tree.preview = nil
	m.refreshViewport(true)
}

func (m *Model) renderTreePreview(width int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(truncateWidth(m.side.tree.prevHead, width)))
	b.WriteString(mutedStyle.Render("  Esc 关闭"))
	b.WriteString("\n")
	for _, l := range m.side.tree.preview {
		b.WriteString(diffLineStyle(l).Render(truncateWidth(l, width)))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// treeView 渲染 height 行文件树（不含右侧分隔线）。
func (m *Model) treeView(inner, height int) []string {
	t := &m.side.tree
	accent := lipgloss.NewStyle().Foreground(accentColor)
	text := lipgloss.NewStyle().Foreground(textColor)
	header := " " + mutedStyle.Render(fmt.Sprintf("改动 %d · Ctrl+B 关闭", len(t.changes)))
	rows := m.treeRows()
	body := height - 1
	start := 0
	if t.sel >= body {
		start = t.sel - body + 1
	}
	out := []string{padRight(header, inner)}
	for i := start; i < len(rows) && len(out) < height; i++ {
		r := rows[i]
		n := r.n
		mark := "  "
		if m.side.focus && i == t.sel {
			mark = accent.Render("›") + " "
		}
		indent := strings.Repeat("  ", r.depth)
		var name, right string
		if n.dir {
			arrow := "▸ "
			if n.expanded {
				arrow = "▾ "
			}
			name = mutedStyle.Render(arrow + n.name + "/")
		} else if ch, ok := t.changes[n.path]; ok {
			dot := lipgloss.NewStyle().Foreground(changeColor(ch.code)).Render("●")
			st := text
			switch ch.code {
			case 'D':
				st = mutedStyle.Strikethrough(true)
				right = mutedStyle.Render(fmt.Sprintf("−%d", ch.del))
			case 'A':
				right = mutedStyle.Render("新")
			default:
				right = mutedStyle.Render(fmt.Sprintf("+%d −%d", ch.add, ch.del))
			}
			name = dot + " " + st.Render(n.name)
		} else {
			name = "  " + mutedStyle.Render(n.name)
		}
		if t.confirm == n.path {
			right = text.Render("撤销？y/n")
		}
		left := " " + mark + indent
		nameW := inner - lipgloss.Width(left) - lipgloss.Width(right) - 2
		line := left + truncateWidth(name, max(2, nameW))
		if right != "" {
			line += strings.Repeat(" ", max(1, inner-1-lipgloss.Width(line)-lipgloss.Width(right))) + right
		}
		out = append(out, padRight(line, inner))
	}
	if len(rows) == 0 {
		out = append(out, padRight(" "+mutedStyle.Render("空目录"), inner))
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", inner))
	}
	return out
}

func changeColor(code byte) adaptiveColor {
	switch code {
	case 'A':
		return agentColor
	case 'D':
		return errorColor
	}
	return pacYellowColor
}

// gitChanges 返回工作区相对 HEAD 的改动；不是 git 仓库时为空。
func gitChanges(dir string) map[string]fileChange {
	out := map[string]fileChange{}
	st, err := runGit(dir, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return out
	}
	recs := strings.Split(st, "\x00")
	for i := 0; i < len(recs); i++ {
		r := recs[i]
		if len(r) < 4 {
			continue
		}
		x, y, p := r[0], r[1], r[3:]
		if x == 'R' || x == 'C' {
			i++ // -z 下重命名的原路径单独一条
		}
		var code byte = 'M'
		switch {
		case x == '?' || x == 'A':
			code = 'A'
		case x == 'D' || y == 'D':
			code = 'D'
		}
		out[p] = fileChange{code: code}
	}
	if ns, err := runGit(dir, "diff", "--numstat", "-z", "HEAD"); err == nil {
		for _, rec := range strings.Split(ns, "\x00") {
			f := strings.SplitN(rec, "\t", 3)
			if len(f) != 3 {
				continue
			}
			if ch, ok := out[f[2]]; ok {
				ch.add, _ = strconv.Atoi(f[0])
				ch.del, _ = strconv.Atoi(f[1])
				out[f[2]] = ch
			}
		}
	}
	for p, ch := range out {
		if ch.code == 'A' && ch.add == 0 {
			if data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p))); err == nil && !tools.IsSensitivePath(p) {
				ch.add = bytes.Count(data, []byte("\n"))
				out[p] = ch
			}
		}
	}
	return out
}

// gitIgnored 返回 paths 中被 .gitignore 忽略的那些。
func gitIgnored(dir string, paths []string) map[string]bool {
	res := map[string]bool{}
	if len(paths) == 0 {
		return res
	}
	cmd := exec.Command("git", childenv.GitArgs([]string{"check-ignore", "-z", "--stdin"})...)
	cmd.Dir = dir
	cmd.Env = childenv.GitEnv()
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	out, _ := cmd.Output() // 没有被忽略的文件时退出码为 1
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			res[strings.TrimSuffix(p, "/")] = true
		}
	}
	return res
}

func fileDiff(dir, p string, ch fileChange) string {
	if ch.code == 'A' {
		if out, err := runGit(dir, "diff", "--no-color", "HEAD", "--", p); err == nil && out != "" {
			return out
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			return ""
		}
		var b strings.Builder
		for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			b.WriteString("+" + l + "\n")
		}
		return b.String()
	}
	out, err := runGit(dir, "diff", "--no-color", "HEAD", "--", p)
	if err != nil {
		return ""
	}
	return out
}
