package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"agnet/internal/llm"
)

const (
	gitDiffMaxLines  = 400
	commitDiffMaxLen = 32 << 10
	gitTimeout       = 15 * time.Second
	commitToolName   = "git commit"
)

// roleDiff 是 /diff 输出的气泡：每行按 + / - 着色。
const roleDiff role = 100

type gitState struct {
	busy bool
}

// gitMsg 是 git 相关异步结果，统一交给 handleGitMsg。
type gitMsg interface{ isGitMsg() }

type commitMsgReady struct {
	msg   string
	files []string
	err   error
}

type commitDecision struct {
	allow bool
	msg   string
	files []string
}

func (commitMsgReady) isGitMsg() {}
func (commitDecision) isGitMsg() {}

func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

// sessionEditedFiles 从对话历史里找出本会话用 write_file/edit_file 改过的文件。
func sessionEditedFiles(history []llm.Message) []string {
	seen := map[string]bool{}
	var out []string
	for _, msg := range history {
		for _, tc := range msg.ToolCalls {
			if !isFileEdit(tc.Function.Name) {
				continue
			}
			var in struct {
				Path string `json:"path"`
			}
			if json.Unmarshal([]byte(tc.Function.Arguments), &in) != nil || in.Path == "" {
				continue
			}
			p := filepath.ToSlash(filepath.Clean(in.Path))
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// changedSessionFiles 返回本会话改过、且相对 HEAD 仍有未提交改动的文件。
func (m *Model) changedSessionFiles() (files []string, untracked map[string]bool, err error) {
	dir := m.ag.Workspace()
	edited := sessionEditedFiles(m.ag.History())
	if len(edited) == 0 {
		return nil, nil, nil
	}
	args := append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, edited...)
	out, err := runGit(dir, args...)
	if err != nil {
		return nil, nil, err
	}
	untracked = map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		p := strings.Trim(line[3:], `"`)
		if i := strings.Index(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		files = append(files, p)
		if strings.HasPrefix(line, "??") {
			untracked[p] = true
		}
	}
	sort.Strings(files)
	return files, untracked, nil
}

// sessionDiff 生成这些文件的 unified diff（未跟踪文件按全新增处理）。
func (m *Model) sessionDiff(files []string, untracked map[string]bool) (string, error) {
	dir := m.ag.Workspace()
	var tracked []string
	for _, f := range files {
		if !untracked[f] {
			tracked = append(tracked, f)
		}
	}
	var b strings.Builder
	if len(tracked) > 0 {
		out, err := runGit(dir, append([]string{"diff", "--no-color", "HEAD", "--"}, tracked...)...)
		if err != nil {
			// 还没有任何提交时没有 HEAD
			out, err = runGit(dir, append([]string{"diff", "--no-color", "--cached", "--"}, tracked...)...)
			if err != nil {
				return "", err
			}
		}
		b.WriteString(out)
	}
	for _, f := range files {
		if !untracked[f] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "diff --git a/%s b/%s\nnew file\n", f, f)
		for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			b.WriteString("+" + l + "\n")
		}
	}
	return b.String(), nil
}

// formatDiffLines 把 git diff 转成界面的 "+ " / "- " 行格式。
func formatDiffLines(diff string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			parts := strings.Fields(l)
			out = append(out, "", "── "+strings.TrimPrefix(parts[len(parts)-1], "b/"))
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"), strings.HasPrefix(l, "index "),
			strings.HasPrefix(l, "new file"), strings.HasPrefix(l, "deleted file"), strings.HasPrefix(l, "similarity"):
		case strings.HasPrefix(l, "@@"):
			out = append(out, "  ⋯")
		case strings.HasPrefix(l, "+"):
			out = append(out, "+ "+l[1:])
		case strings.HasPrefix(l, "-"):
			out = append(out, "- "+l[1:])
		case strings.HasPrefix(l, " "):
			out = append(out, "  "+l[1:])
		}
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	if len(out) > gitDiffMaxLines {
		more := len(out) - gitDiffMaxLines
		out = append(out[:gitDiffMaxLines], fmt.Sprintf("…还有 %d 行", more))
	}
	return out
}

// showDiff 实现 /diff：列出本会话改过的文件和红绿 diff。
func (m *Model) showDiff() {
	files, untracked, err := m.changedSessionFiles()
	if err != nil {
		m.notice("无法读取 git 状态：" + err.Error())
		return
	}
	if len(files) == 0 {
		m.notice("本次会话还没有未提交的文件改动。")
		return
	}
	diff, err := m.sessionDiff(files, untracked)
	if err != nil {
		m.notice("生成 diff 失败：" + err.Error())
		return
	}
	head := fmt.Sprintf("本次会话改动 %d 个文件：%s", len(files), strings.Join(files, "、"))
	m.messages = append(m.messages, chatMessage{
		role: roleDiff, content: head + "\n" + strings.Join(formatDiffLines(diff), "\n"), done: true,
	})
	m.refreshViewport(true)
}

// startCommit 实现 /commit：让模型写提交信息，然后弹审批卡确认。
func (m *Model) startCommit() tea.Cmd {
	if m.git.busy || m.streaming {
		m.notice("请等当前任务结束后再提交。")
		return nil
	}
	files, untracked, err := m.changedSessionFiles()
	if err != nil {
		m.notice("无法读取 git 状态：" + err.Error())
		return nil
	}
	if len(files) == 0 {
		m.notice("本次会话没有需要提交的文件改动。")
		return nil
	}
	diff, err := m.sessionDiff(files, untracked)
	if err != nil {
		m.notice("生成 diff 失败：" + err.Error())
		return nil
	}
	if len(diff) > commitDiffMaxLen {
		diff = diff[:commitDiffMaxLen] + "\n…(diff 过长，已截断)"
	}
	m.git.busy = true
	m.notice("正在根据 " + fmt.Sprint(len(files)) + " 个文件的改动生成提交信息…")
	ag := m.ag
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		msg, err := ag.CommitMessage(ctx, diff)
		return commitMsgReady{msg: msg, files: files, err: err}
	}
}

func (m *Model) handleGitMsg(msg gitMsg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case commitMsgReady:
		if msg.err != nil {
			m.git.busy = false
			m.notice("生成提交信息失败：" + msg.err.Error())
			return m, nil
		}
		if m.approval != nil { // 正有别的审批，先放弃这次
			m.git.busy = false
			m.notice("有待确认的操作，提交已取消，稍后再试 /commit。")
			return m, nil
		}
		resp := make(chan bool, 1)
		display := msg.msg + "\n\n只提交本会话改过的文件：\n" + strings.Join(msg.files, "\n")
		m.approval = &approvalState{toolName: commitToolName, display: display, resp: resp}
		m.layout()
		m.refreshViewport(true)
		text, files := msg.msg, msg.files
		return m, func() tea.Msg { return commitDecision{allow: <-resp, msg: text, files: files} }

	case commitDecision:
		m.git.busy = false
		if !msg.allow {
			m.notice("已取消提交。")
			return m, nil
		}
		dir := m.ag.Workspace()
		if _, err := runGit(dir, append([]string{"add", "--"}, msg.files...)...); err != nil {
			m.notice("暂存失败：" + err.Error())
			return m, nil
		}
		if _, err := runGit(dir, append([]string{"commit", "-m", msg.msg, "--"}, msg.files...)...); err != nil {
			m.notice("提交失败：" + err.Error())
			return m, nil
		}
		hash, _ := runGit(dir, "rev-parse", "--short", "HEAD")
		first, _, _ := strings.Cut(msg.msg, "\n")
		m.notice("已提交 " + strings.TrimSpace(hash) + "  " + first)
	}
	return m, nil
}

// diffBubble 渲染 roleDiff 气泡。
func diffBubble(content string, width int) string {
	lines := strings.Split(content, "\n")
	var b strings.Builder
	b.WriteString(mutedStyle.Render(truncateWidth("· "+lines[0], width)))
	for _, l := range lines[1:] {
		b.WriteString("\n")
		style := diffLineStyle(l)
		if strings.HasPrefix(l, "── ") {
			style = lipgloss.NewStyle().Foreground(textColor).Bold(true)
		}
		b.WriteString(style.Render(truncateWidth(l, width)))
	}
	return b.String()
}
