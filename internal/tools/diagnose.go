package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ainnhuomiao/miao/internal/childenv"
	"github.com/ainnhuomiao/miao/internal/config"
)

func (r *Registry) noteEdited(rel string) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return
	}
	r.diagMu.Lock()
	defer r.diagMu.Unlock()
	if r.dirty == nil {
		r.dirty = map[string]struct{}{}
	}
	r.dirty[rel] = struct{}{}
}

// FlushDiagnose 对本轮写过的包各跑一次诊断（go vet / tsc），避免每次编辑都同步等 15 秒。
func (r *Registry) FlushDiagnose(ctx context.Context) string {
	r.diagMu.Lock()
	files := make([]string, 0, len(r.dirty))
	for f := range r.dirty {
		files = append(files, f)
	}
	r.dirty = nil
	r.diagMu.Unlock()
	sort.Strings(files)
	if len(files) == 0 {
		return ""
	}
	seenGo := map[string]bool{}
	var tsRel string
	var b strings.Builder
	for _, rel := range files {
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".go":
			key := strings.Join(goVetArgs(rel), " ")
			if seenGo[key] {
				continue
			}
			seenGo[key] = true
			b.WriteString(diagnoseFile(ctx, r.workspace, rel))
		case ".ts", ".tsx", ".js", ".jsx":
			if tsRel == "" {
				tsRel = rel
			}
		}
	}
	if tsRel != "" {
		b.WriteString(diagnoseFile(ctx, r.workspace, tsRel))
	}
	return b.String()
}

func diagnoseFile(ctx context.Context, workspace, rel string) string {
	if strings.TrimSpace(strings.ToLower(config.Getenv("DIAGNOSE"))) == "0" {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(rel))
	var name string
	var args []string
	var label string
	switch ext {
	case ".go":
		if _, err := os.Stat(filepath.Join(workspace, "go.mod")); err != nil {
			return ""
		}
		if _, err := exec.LookPath("go"); err != nil {
			return ""
		}
		name, args, label = "go", goVetArgs(rel), "go vet"
	case ".ts", ".tsx", ".js", ".jsx":
		if _, err := os.Stat(filepath.Join(workspace, "tsconfig.json")); err != nil {
			return ""
		}
		tsc, err := exec.LookPath("tsc")
		if err != nil {
			return ""
		}
		// 不在命令行上传文件名，否则 tsc 会忽略 tsconfig.json。
		name, args, label = tsc, tscArgs(), "tsc"
	default:
		return ""
	}
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(dctx, name, args...)
	cmd.Dir = workspace
	cmd.Env = childenv.Environ()
	out, err := cmd.CombinedOutput()
	return formatDiagnose(ctx, dctx, label, string(out), err)
}

func formatDiagnose(parent, dctx context.Context, label, out string, err error) string {
	if parent.Err() != nil {
		return ""
	}
	if dctx.Err() == context.DeadlineExceeded {
		return "\n\n[诊断 " + label + "]\n诊断超时，已跳过"
	}
	text := strings.TrimSpace(out)
	if text == "" && err == nil {
		return ""
	}
	if text == "" {
		text = err.Error()
	}
	if len(text) > 4000 {
		text = text[:4000] + "\n…（已截断）"
	}
	return "\n\n[诊断 " + label + "]\n" + text
}

// goVetArgs 对文件所在包跑 go vet，而不是把单文件当成独立包。
func goVetArgs(rel string) []string {
	dir := filepath.ToSlash(filepath.Dir(filepath.Clean(rel)))
	if dir == "." || dir == "" {
		return []string{"vet", "."}
	}
	return []string{"vet", "./" + dir}
}

func tscArgs() []string {
	return []string{"--noEmit", "--pretty", "false"}
}
