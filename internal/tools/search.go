package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxSearchResults = 100
	maxGrepLineLen   = 300
	maxGrepFileBytes = 4 << 20
)

// ignoreRule 是一条 .gitignore 规则。
type ignoreRule struct {
	pattern  string
	negate   bool
	dirOnly  bool
	anchored bool
}

// gitIgnore 是根目录 .gitignore 的简易匹配器。
type gitIgnore struct{ rules []ignoreRule }

func loadGitIgnore(root string) *gitIgnore {
	gi := &gitIgnore{}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return gi
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var r ignoreRule
		if strings.HasPrefix(line, "!") {
			r.negate = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if strings.HasPrefix(line, "/") {
			r.anchored = true
			line = strings.TrimPrefix(line, "/")
		} else if strings.Contains(line, "/") {
			r.anchored = true
		}
		if line == "" {
			continue
		}
		r.pattern = line
		gi.rules = append(gi.rules, r)
	}
	return gi
}

// Ignored 判断相对工作区的 slash 路径是否被忽略。
func (g *gitIgnore) Ignored(rel string, isDir bool) bool {
	ignored := false
	for _, r := range g.rules {
		if r.dirOnly && !isDir {
			continue
		}
		var ok bool
		if r.anchored {
			ok = matchGlob(r.pattern, rel)
		} else {
			ok, _ = path.Match(r.pattern, path.Base(rel))
		}
		if ok {
			ignored = !r.negate
		}
	}
	return ignored
}

// matchGlob 匹配 slash 路径，支持 ** 跨目录。
func matchGlob(pattern, name string) bool {
	return matchParts(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchParts(pat, parts []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			for i := 0; i <= len(parts); i++ {
				if matchParts(rest, parts[i:]) {
					return true
				}
			}
			return false
		}
		if len(parts) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], parts[0]); !ok {
			return false
		}
		pat, parts = pat[1:], parts[1:]
	}
	return len(parts) == 0
}

// walkWorkspace 遍历 base 下的文件，跳过 .git 与 .gitignore 忽略项。
func (r *Registry) walkWorkspace(ctx context.Context, base string, fn func(full, rel string) error) error {
	gi := loadGitIgnore(r.workspace)
	return filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel, rerr := filepath.Rel(r.workspace, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel != "." {
			if d.Name() == ".git" || gi.Ignored(rel, d.IsDir()) || isSensitive(rel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if err := r.checkNoSymlinkEscape(p); err != nil {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		return fn(p, rel)
	})
}

func formatResults(lines []string, total int) string {
	if total == 0 {
		return "(无匹配)"
	}
	out := strings.Join(lines, "\n")
	if total > maxSearchResults {
		out += fmt.Sprintf("\n……已截断（共 %d 条，只显示前 %d 条。缩小 pattern 或改 path 再搜。）", total, maxSearchResults)
	}
	return out
}

func registerSearchTools(r *Registry) {
	r.Register(Tool{
		Def: toolDef("glob", "按通配符查找工作区内的文件路径（如 **/*.go），每行一个相对路径。不知道文件名时用这个；读内容再用 read_file。最多返回 100 条，超出时缩小 pattern 或改 path。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "通配符模式，相对 path，如 **/*.go"},
				"path":    map[string]any{"type": "string", "description": "起始目录，相对工作区，默认 \".\""},
			},
			"required": []string{"pattern"},
		}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				Pattern string `json:"pattern"`
				Path    string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return Result{Content: "参数解析失败: " + err.Error(), IsError: true}
			}
			if strings.TrimSpace(in.Pattern) == "" {
				return Result{Content: "pattern 不能为空", IsError: true}
			}
			if _, err := path.Match(strings.ReplaceAll(in.Pattern, "**", "*"), ""); err != nil {
				return Result{Content: "pattern 非法: " + err.Error(), IsError: true}
			}
			if strings.TrimSpace(in.Path) == "" {
				in.Path = "."
			}
			base, err := r.ResolvePath(in.Path)
			if err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			if err := r.checkNoSymlinkEscape(base); err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			var lines []string
			total := 0
			err = r.walkWorkspace(ctx, base, func(full, rel string) error {
				sub, _ := filepath.Rel(base, full)
				if matchGlob(in.Pattern, filepath.ToSlash(sub)) {
					total++
					if len(lines) < maxSearchResults {
						lines = append(lines, rel)
					}
				}
				return nil
			})
			if err != nil {
				return Result{Content: "遍历失败: " + err.Error(), IsError: true}
			}
			return Result{Content: formatResults(lines, total)}
		},
	})

	r.Register(Tool{
		Def: toolDef("grep", "用正则搜索工作区内的文件内容，输出 文件:行号: 内容。定位代码时用；打开匹配文件再用 read_file。最多 100 条，超出时收紧 pattern、加 glob 或缩小 path。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string", "description": "Go 正则表达式"},
				"path":        map[string]any{"type": "string", "description": "搜索的目录或文件，相对工作区，默认 \".\""},
				"glob":        map[string]any{"type": "string", "description": "文件过滤通配符，如 **/*.go 或 *.go"},
				"ignore_case": map[string]any{"type": "boolean", "description": "是否忽略大小写"},
			},
			"required": []string{"pattern"},
		}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				Pattern    string `json:"pattern"`
				Path       string `json:"path"`
				Glob       string `json:"glob"`
				IgnoreCase bool   `json:"ignore_case"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return Result{Content: "参数解析失败: " + err.Error(), IsError: true}
			}
			if in.Pattern == "" {
				return Result{Content: "pattern 不能为空", IsError: true}
			}
			expr := in.Pattern
			if in.IgnoreCase {
				expr = "(?i)" + expr
			}
			re, err := regexp.Compile(expr)
			if err != nil {
				return Result{Content: "正则非法: " + err.Error(), IsError: true}
			}
			if strings.TrimSpace(in.Path) == "" {
				in.Path = "."
			}
			base, err := r.ResolvePath(in.Path)
			if err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			if err := r.checkNoSymlinkEscape(base); err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			relBase, _ := filepath.Rel(r.workspace, base)
			if isSensitive(filepath.ToSlash(relBase)) {
				return Result{Content: "禁止搜索敏感文件: " + relBase, IsError: true}
			}
			info, err := os.Stat(base)
			if err != nil {
				return Result{Content: "无法访问: " + err.Error(), IsError: true}
			}
			var lines []string
			total := 0
			search := func(full, rel string) error {
				if in.Glob != "" {
					sub := rel
					if info.IsDir() {
						s, _ := filepath.Rel(base, full)
						sub = filepath.ToSlash(s)
					}
					okBase, _ := path.Match(in.Glob, path.Base(rel))
					if !matchGlob(in.Glob, sub) && !(!strings.Contains(in.Glob, "/") && okBase) {
						return nil
					}
				}
				f, err := os.Open(full)
				if err != nil {
					return nil
				}
				defer func() { _ = f.Close() }()
				head := make([]byte, 8000)
				n, _ := f.Read(head)
				if bytes.IndexByte(head[:n], 0) >= 0 {
					return nil
				}
				if _, err := f.Seek(0, 0); err != nil {
					return nil
				}
				sc := bufio.NewScanner(f)
				sc.Buffer(make([]byte, 64<<10), maxGrepFileBytes)
				ln := 0
				for sc.Scan() {
					ln++
					text := sc.Text()
					if !re.MatchString(text) {
						continue
					}
					total++
					if len(lines) < maxSearchResults {
						if len(text) > maxGrepLineLen {
							cut := maxGrepLineLen
							for cut > 0 && (text[cut]&0xC0) == 0x80 {
								cut--
							}
							text = text[:cut] + "…(行过长已截断)"
						}
						lines = append(lines, fmt.Sprintf("%s:%d: %s", rel, ln, text))
					}
				}
				return nil
			}
			if info.IsDir() {
				err = r.walkWorkspace(ctx, base, search)
			} else {
				rel, _ := filepath.Rel(r.workspace, base)
				err = search(base, filepath.ToSlash(rel))
			}
			if err != nil {
				return Result{Content: "搜索失败: " + err.Error(), IsError: true}
			}
			return Result{Content: formatResults(lines, total)}
		},
	})
}
