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

// registerEditFile 注册 edit_file：把文件里唯一出现的 old_string 换成 new_string，写入前需 Approver 确认。
func registerEditFile(r *Registry) {
	r.Register(Tool{
		Def: toolDef("edit_file", "局部修改工作区内的已有文本文件：把唯一出现的 old_string 替换为 new_string（每次修改都需用户确认）。old_string 必须与文件内容逐字一致（含缩进），且在文件中只出现一次；不唯一时请多带几行上下文。新建文件请用 write_file。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string", "description": "相对工作区根目录的文件路径"},
				"old_string": map[string]any{"type": "string", "description": "要被替换的原文片段，必须在文件中恰好出现一次"},
				"new_string": map[string]any{"type": "string", "description": "替换后的新片段，可为空表示删除"},
			},
			"required": []string{"path", "old_string", "new_string"},
		}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				Path      string `json:"path"`
				OldString string `json:"old_string"`
				NewString string `json:"new_string"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return Result{Content: "参数解析失败: " + err.Error(), IsError: true}
			}
			if in.OldString == "" {
				return Result{Content: "old_string 不能为空；新建文件请用 write_file", IsError: true}
			}
			if in.OldString == in.NewString {
				return Result{Content: "old_string 与 new_string 相同，无需修改", IsError: true}
			}
			if !utf8.ValidString(in.NewString) {
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
				return Result{Content: "禁止修改敏感文件: " + rel, IsError: true}
			}
			info, err := os.Stat(full)
			if err != nil {
				return Result{Content: "文件不存在: " + rel + "（新建文件请用 write_file）", IsError: true}
			}
			if info.IsDir() {
				return Result{Content: "目标是目录", IsError: true}
			}
			data, err := os.ReadFile(full)
			if err != nil {
				return Result{Content: "读取失败: " + err.Error(), IsError: true}
			}
			old := string(data)
			if !utf8.ValidString(old) {
				return Result{Content: "只支持 UTF-8 文本文件", IsError: true}
			}
			switch n := strings.Count(old, in.OldString); n {
			case 0:
				return Result{Content: "在 " + rel + " 中没有找到 old_string，请先 read_file 确认原文（含缩进和空白）", IsError: true}
			case 1:
			default:
				return Result{Content: fmt.Sprintf("old_string 在 %s 中出现了 %d 次，必须唯一；请多带几行上下文", rel, n), IsError: true}
			}
			updated := strings.Replace(old, in.OldString, in.NewString, 1)
			if len(updated) > maxWriteBytes {
				return Result{Content: "修改后文件超过 64KiB 上限", IsError: true}
			}

			diff, add, del := lineDiff(old, updated)
			if len(diff) > maxDiffLines {
				diff = append(diff[:maxDiffLines], fmt.Sprintf("…（另有 %d 行未显示）", len(diff)-maxDiffLines))
			}
			// 与 write_file 同一约定：第一行标题，其余为 diff 行。
			display := fmt.Sprintf("%s · +%d -%d", rel, add, del) + "\n" + strings.Join(diff, "\n")

			approver := r.Approver()
			if approver == nil {
				return Result{Content: "edit_file 需要用户确认，但当前没有审批通道", IsError: true}
			}
			allow, err := approver(ctx, "edit_file", display, args)
			if err != nil {
				return Result{Content: "审批中断: " + err.Error(), IsError: true}
			}
			if !allow {
				return Result{Content: "用户拒绝修改 " + rel, IsError: true}
			}
			if err := r.saveSnapshot(full); err != nil {
				return Result{Content: "保存撤销快照失败: " + err.Error(), IsError: true}
			}
			if err := atomicWrite(full, updated, info.Mode().Perm()); err != nil {
				return Result{Content: "写入失败: " + err.Error(), IsError: true}
			}
			return Result{Content: fmt.Sprintf("已修改 %s（+%d -%d）", rel, add, del)}
		},
	})
}

// atomicWrite 先写同目录临时文件再 rename，保留原权限。
func atomicWrite(full, content string, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(full), ".miao-*")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(content)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("%v %v", werr, cerr)
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), full); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
