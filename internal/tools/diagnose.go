package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ainnhuomiao/miao/internal/config"
)

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
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
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
