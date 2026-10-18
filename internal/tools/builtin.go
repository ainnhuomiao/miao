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
		Def: toolDef("read_file", "读取工作区内的文本文件，带行号。查看文件内容时用这个，不要用 run_command 调 cat/head。大文件用 offset（从 1 起）和 limit 分段读；结果末尾会写共多少行、下一步 offset。不跟随指向工作区外的符号链接，不读 .env 等敏感文件。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "相对工作区根目录的文件路径",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "起始行号，从 1 起，默认 1",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "最多读取行数，默认 2000",
				},
			},
			"required": []string{"path"},
		}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				Path   string `json:"path"`
				Offset int    `json:"offset"`
				Limit  int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return Result{Content: "参数解析失败: " + err.Error(), IsError: true}
			}
			f, _, err := r.openForRead(in.Path)
			if err != nil {
				return Result{Content: err.Error() + "——检查 path 是否相对工作区，或改用 list_dir。", IsError: true}
			}
			defer f.Close()
			text, err := readFileWindow(f, in.Offset, in.Limit)
			if err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			return Result{Content: text}
		},
	})

	r.Register(Tool{
		Def: toolDef("list_dir", "列出工作区内某个目录的文件和子目录（目录名带 /）。只看目录结构时用；读文件内容用 read_file。不跟随指向工作区外的符号链接。", map[string]any{
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
