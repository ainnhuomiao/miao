package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxWriteBytes = 64 << 10 // 64 KiB，与 run_command 输出上限一致

const maxDiffLines = 2000

// registerWriteFile 注册 write_file：写工作区内文件，每次都需 Approver 确认。
func registerWriteFile(r *Registry) {
	r.Register(Tool{
		Def: toolDef("write_file", "在工作区内创建或覆盖一个文本文件（每次写入都需用户确认）。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "相对工作区根目录的文件路径"},
				"content": map[string]any{"type": "string", "description": "完整的文件内容"},
			},
			"required": []string{"path", "content"},
		}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return Result{Content: "参数解析失败: " + err.Error(), IsError: true}
			}
			if len(in.Content) > maxWriteBytes {
				return Result{Content: "内容超过 64KiB 上限", IsError: true}
			}
			if !utf8.ValidString(in.Content) {
				return Result{Content: "只支持 UTF-8 文本", IsError: true}
			}
			full, err := r.ResolvePath(in.Path)
			if err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			if err := r.checkNoSymlinkEscape(full); err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			rel, _ := filepath.Rel(r.workspace, full)
			if isSensitive(rel) {
				return Result{Content: "禁止写入敏感文件: " + rel, IsError: true}
			}

			action := "新建"
			var diff []string
			header := ""
			if info, err := os.Stat(full); err == nil {
				if info.IsDir() {
					return Result{Content: "目标是目录", IsError: true}
				}
				action = "覆盖"
				oldData, err := os.ReadFile(full)
				if err != nil {
					return Result{Content: "读取原文件失败: " + err.Error(), IsError: true}
				}
				if string(oldData) == in.Content {
					return Result{Content: rel + " 内容未变化，无需写入"}
				}
				var add, del int
				diff, add, del = lineDiff(string(oldData), in.Content)
				header = fmt.Sprintf("%s · +%d -%d", rel, add, del)
			} else {
				for _, l := range splitLines(in.Content) {
					diff = append(diff, "+ "+l)
				}
				header = "新建 " + rel
			}
			if len(diff) > maxDiffLines {
				diff = append(diff[:maxDiffLines], fmt.Sprintf("…（另有 %d 行未显示）", len(diff)-maxDiffLines))
			}
			// 约定：第一行是标题，其余是 diff 行（"+ "/"- " 前缀），TUI 据此上色。
			display := header + "\n" + strings.Join(diff, "\n")

			approver := r.Approver()
			if approver == nil {
				return Result{Content: "write_file 需要用户确认，但当前没有审批通道", IsError: true}
			}
			allow, err := approver(ctx, "write_file", display, args)
			if err != nil {
				return Result{Content: "审批中断: " + err.Error(), IsError: true}
			}
			if !allow {
				return Result{Content: "用户拒绝写入 " + rel, IsError: true}
			}

			if err := r.saveSnapshot(full); err != nil {
				return Result{Content: "保存撤销快照失败: " + err.Error(), IsError: true}
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return Result{Content: "创建目录失败: " + err.Error(), IsError: true}
			}
			tmp, err := os.CreateTemp(filepath.Dir(full), ".miao-*")
			if err != nil {
				return Result{Content: "写入失败: " + err.Error(), IsError: true}
			}
			_, werr := tmp.WriteString(in.Content)
			cerr := tmp.Close()
			if werr != nil || cerr != nil {
				os.Remove(tmp.Name())
				return Result{Content: fmt.Sprintf("写入失败: %v %v", werr, cerr), IsError: true}
			}
			if err := os.Rename(tmp.Name(), full); err != nil {
				os.Remove(tmp.Name())
				return Result{Content: "写入失败: " + err.Error(), IsError: true}
			}
			return Result{Content: fmt.Sprintf("已%s %s（%d 字节）", action, rel, len(in.Content))}
		},
	})
}

// checkNoSymlinkEscape 确认最近的已存在祖先目录（含目标本身）解析符号链接后仍在工作区内。
func (r *Registry) checkNoSymlinkEscape(full string) error {
	root, err := filepath.EvalSymlinks(r.workspace)
	if err != nil {
		return fmt.Errorf("工作区解析失败: %w", err)
	}
	p := full
	for {
		if _, err := os.Lstat(p); err == nil {
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				return fmt.Errorf("路径解析失败: %w", err)
			}
			rel, err := filepath.Rel(root, real)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("路径经符号链接越出工作区")
			}
			return nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
	}
}

func isSensitive(rel string) bool {
	base := filepath.Base(rel)
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	return first == ".git"
}
