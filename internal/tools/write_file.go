package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxWriteBytes = 64 << 10 // 64 KiB，与 run_command 输出上限一致

const maxDiffLines = 2000

// registerWriteFile 注册 write_file：写工作区内文件，每次都需 Approver 确认。
func registerWriteFile(r *Registry) {
	r.Register(Tool{
		Def: toolDef("write_file", "新建文本文件，或整文件覆盖重写（需确认）。已有文件的局部修改用 edit_file。写入前确认目标路径；覆盖已有文件会先给出 diff。上限 64KiB。", map[string]any{
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
				return Result{Content: "内容超过 64KiB 上限——缩小内容，或先 write_file 再分多次 edit_file", IsError: true}
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
			if err := r.checkWritable(full, "写入"); err != nil {
				return Result{Content: err.Error(), IsError: true}
			}

			action := "新建"
			var diff []string
			header := ""
			if info, err := os.Stat(full); err == nil {
				if info.IsDir() {
					return Result{Content: "目标是目录", IsError: true}
				}
				action = "覆盖"
				oldData, err := readRegularFile(full)
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
				auditEvent(r.workspace, "write_file", rel, "reject")
				return Result{Content: "用户拒绝写入 " + rel, IsError: true}
			}
			auditEvent(r.workspace, "write_file", rel, "allow")

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
				_ = os.Remove(tmp.Name())
				return Result{Content: fmt.Sprintf("写入失败: %v %v", werr, cerr), IsError: true}
			}
			if err := os.Rename(tmp.Name(), full); err != nil {
				_ = os.Remove(tmp.Name())
				return Result{Content: "写入失败: " + err.Error(), IsError: true}
			}
			msg := fmt.Sprintf("已%s %s（%d 字节）", action, rel, len(in.Content))
			r.noteEdited(rel)
			return Result{Content: msg}
		},
	})
}

// checkNoSymlinkEscape 确认最近的已存在祖先目录（含目标本身）解析符号链接后仍在工作区内，
// 且解析后的真实路径不是敏感文件（防止 notes.txt -> .env 这类工作区内链接绕过）。
func (r *Registry) checkNoSymlinkEscape(full string) error {
	realRel, err := r.realRel(full)
	if err != nil {
		return err
	}
	if lit, lerr := filepath.Rel(r.workspace, full); lerr == nil && !isSensitive(lit) && isSensitive(realRel) {
		return fmt.Errorf("路径经符号链接指向敏感文件: %s", realRel)
	}
	return nil
}

// realRel 解析 full 最近的已存在祖先（含自身）的符号链接，返回真实路径相对工作区根的路径；越界则报错。
func (r *Registry) realRel(full string) (string, error) {
	root, err := filepath.EvalSymlinks(r.workspace)
	if err != nil {
		return "", fmt.Errorf("工作区解析失败: %w", err)
	}
	p := full
	for {
		if _, err := os.Lstat(p); err == nil {
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				return "", fmt.Errorf("路径解析失败: %w", err)
			}
			if suffix, err := filepath.Rel(p, full); err == nil && suffix != "." {
				real = filepath.Join(real, suffix)
			}
			rel, err := filepath.Rel(root, real)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("路径经符号链接越出工作区")
			}
			return rel, nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Rel(r.workspace, full)
		}
		p = parent
	}
}

// checkWritable 拒绝写入/修改受保护文件：敏感文件，以及 AGENTS.md（可读不可写）。符号链接按真实路径再查一次。
func (r *Registry) checkWritable(full, verb string) error {
	rel, _ := filepath.Rel(r.workspace, full)
	realRel, err := r.realRel(full)
	if err != nil {
		return err
	}
	for _, p := range []string{rel, realRel} {
		if isSensitive(p) || isWriteProtected(p) {
			return fmt.Errorf("禁止%s受保护文件: %s", verb, p)
		}
	}
	return nil
}

func isWriteProtected(rel string) bool {
	return filepath.Base(filepath.ToSlash(filepath.Clean(rel))) == "AGENTS.md"
}

// sshKeyPrefixes 是常见 SSH 私钥/公钥文件名前缀（id_rsa、id_ed25519.pub 等）。
var sshKeyPrefixes = []string{"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519"}

var sensitiveNames = map[string]bool{
	".npmrc": true, ".netrc": true, ".envrc": true, ".git-credentials": true,
}

func isSensitive(rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	base := path.Base(rel)
	if base == ".env" || strings.HasPrefix(base, ".env.") || sensitiveNames[base] {
		return true
	}
	if strings.HasSuffix(strings.ToLower(base), ".pem") {
		return true
	}
	for _, k := range sshKeyPrefixes {
		if base == k || strings.HasPrefix(base, k+".") || strings.HasPrefix(base, k+"_") {
			return true
		}
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".git" || seg == ".miao" {
			return true
		}
	}
	return false
}
