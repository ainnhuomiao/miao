package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ainnhuomiao/miao/internal/appdir"
	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/session"
)

func (m *Model) exportSession(path string) {
	msgs := m.ag.History()
	if len(msgs) == 0 {
		m.notice("当前会话是空的，没有可导出的内容。")
		return
	}
	if strings.TrimSpace(path) == "" {
		name := "miao-export-" + time.Now().Format("20060102-150405") + ".md"
		if m.sess != nil && m.sess.ID != "" {
			name = "miao-" + m.sess.ID + ".md"
		}
		dir, err := defaultExportDir()
		if err != nil {
			m.notice("导出失败：" + err.Error())
			return
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			m.notice("导出失败：" + err.Error())
			return
		}
		path = filepath.Join(dir, name)
	}
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			m.notice("导出失败：" + path + " 是符号链接，不会跟随写入。")
			return
		}
		if m.exportConfirm != path {
			m.exportConfirm = path
			m.notice(path + " 已存在。再执行一次同样的 /export 覆盖它。")
			return
		}
	}
	m.exportConfirm = ""
	body := session.RedactForExport(sessionMarkdown(m.modelName, msgs))
	if err := writeNoFollow(path, body); err != nil {
		m.notice("导出失败：" + err.Error())
		return
	}
	m.notice("已导出到 " + path)
}

// writeNoFollow 写文件但不跟随符号链接（防止检查和写入之间被换成链接）。
func writeNoFollow(path, body string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func defaultExportDir() (string, error) {
	dir, err := appdir.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "exports"), nil
}

func sessionMarkdown(model string, msgs []llm.Message) string {
	var b strings.Builder
	b.WriteString("# miao 会话\n\n")
	if model != "" {
		fmt.Fprintf(&b, "模型：`%s`\n\n", model)
	}
	for _, m := range msgs {
		switch m.Role {
		case llm.RoleUser:
			b.WriteString("## 用户\n\n")
			b.WriteString(m.Content)
			b.WriteString("\n\n")
		case llm.RoleAssistant:
			b.WriteString("## 助手\n\n")
			if m.Content != "" {
				b.WriteString(m.Content)
				b.WriteString("\n\n")
			}
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&b, "```tool %s\n%s\n```\n\n", tc.Function.Name, tc.Function.Arguments)
			}
		case llm.RoleTool:
			fmt.Fprintf(&b, "### 工具 %s\n\n```\n%s\n```\n\n", m.Name, m.Content)
		}
	}
	return b.String()
}
