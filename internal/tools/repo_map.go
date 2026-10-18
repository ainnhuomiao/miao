package tools

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

const (
	repoMapMaxFiles = 80
	repoMapMaxBytes = 32 << 10
)

func registerRepoMap(r *Registry) {
	r.Register(Tool{
		Def: toolDef("repo_map", "生成工作区的 Go 符号地图（包、类型、函数），适合快速了解项目结构，减少盲目 grep。path 默认整个工作区。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "相对工作区的目录，默认 \".\"",
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
			text, err := buildRepoMap(ctx, r.workspace, full)
			if err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			if text == "" {
				return Result{Content: "没有找到 Go 源文件。"}
			}
			return Result{Content: text}
		},
	})
}

func buildRepoMap(ctx context.Context, workspace, root string) (string, error) {
	var b strings.Builder
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", "vendor", "node_modules", "testdata", ".miao", ".agnet":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if n >= repoMapMaxFiles || b.Len() >= repoMapMaxBytes {
			return fs.SkipAll
		}
		syms := goFileSymbols(path)
		if syms == "" {
			return nil
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			rel = path
		}
		b.WriteString(rel)
		b.WriteByte('\n')
		b.WriteString(syms)
		n++
		return nil
	})
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(b.String())
	if n >= repoMapMaxFiles || b.Len() >= repoMapMaxBytes {
		out += "\n…（已截断）"
	}
	return out, nil
}

func goFileSymbols(path string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return ""
	}
	var lines []string
	if f.Name != nil {
		lines = append(lines, "  package "+f.Name.Name)
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = "(" + recvType(d.Recv.List[0].Type) + ") " + name
			}
			lines = append(lines, "  func "+name)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					kind := "type"
					switch s.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					lines = append(lines, "  "+kind+" "+s.Name.Name)
				}
			}
		}
		if len(lines) > 40 {
			lines = append(lines, "  …")
			break
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func recvType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "*" + recvType(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvType(t.X)
	default:
		return "T"
	}
}
