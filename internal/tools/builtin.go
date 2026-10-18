package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/ainnhuomiao/miao/internal/llm"
)

const maxReadBytes = 256 << 10 // 256 KiB

// RegisterBuiltins 注册内置工具（读、搜、写、命令、网页、todo）。
func RegisterBuiltins(r *Registry) {
	r.Register(Tool{
		Def: toolDef("read_file", "读取工作区内的文本文件。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "相对工作区根目录的文件路径",
				},
			},
			"required": []string{"path"},
		}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return Result{Content: "参数解析失败: " + err.Error(), IsError: true}
			}
			f, _, err := r.openForRead(in.Path)
			if err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			defer f.Close()
			text, truncated, err := readTextLimited(f, maxReadBytes)
			if err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			if truncated {
				return Result{Content: text + "\n\n…(已截断，超过 256KiB)"}
			}
			return Result{Content: text}
		},
	})

	r.Register(Tool{
		Def: toolDef("list_dir", "列出工作区内某个目录的内容。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "相对工作区根目录的目录路径，默认 \".\"",
				},
			},
		}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal(args, &in)
			if strings.TrimSpace(in.Path) == "" {
				in.Path = "."
			}
			full, err := r.ResolvePath(in.Path)
			if err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			if err := r.checkNoSymlinkEscape(full); err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			entries, err := os.ReadDir(full)
			if err != nil {
				return Result{Content: "列出失败: " + err.Error(), IsError: true}
			}
			var b strings.Builder
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				b.WriteString(name)
				b.WriteByte('\n')
			}
			if b.Len() == 0 {
				return Result{Content: "(空目录)"}
			}
			return Result{Content: strings.TrimSuffix(b.String(), "\n")}
		},
	})

	registerRunCommand(r)
	registerCommandOutput(r)
	registerWriteFile(r)
	registerSearchTools(r)
	registerEditFile(r)
	registerWebFetch(r)
	registerWebSearch(r)
	registerTodoWrite(r)
	registerRepoMap(r)
}

func toolDef(name, desc string, params map[string]any) llm.ToolDef {
	raw, _ := json.Marshal(params)
	return llm.ToolDef{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        name,
			Description: desc,
			Parameters:  raw,
		},
	}
}
