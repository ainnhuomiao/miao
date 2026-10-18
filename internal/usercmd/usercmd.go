// Package usercmd 加载用户自定义斜杠命令：一个 Markdown 文件就是一条命令。
//
// 目录：~/.miao/commands/*.md（用户级）和 <工作区>/.miao/commands/*.md（项目级；没有 .miao 时读 .agnet，同名覆盖用户级）。
// 文件名（去掉 .md）就是命令名；可选的 frontmatter 里写 description；正文是提示词模板，
// 其中的 $ARGS 替换成命令后面的参数。模板里没有 $ARGS 而用户给了参数时，参数接在末尾。
package usercmd

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ainnhuomiao/miao/internal/appdir"
)

// Command 是一条自定义命令。
type Command struct {
	Name        string // 不含 /
	Description string
	Template    string
	Source      string // "用户" 或 "项目"
	Path        string
}

// TakesArgs 报告模板是否引用了 $ARGS。
func (c Command) TakesArgs() bool { return strings.Contains(c.Template, "$ARGS") }

// Expand 把参数代入模板。
func (c Command) Expand(args string) string {
	args = strings.TrimSpace(args)
	if c.TakesArgs() {
		return strings.TrimSpace(strings.ReplaceAll(c.Template, "$ARGS", args))
	}
	if args == "" {
		return strings.TrimSpace(c.Template)
	}
	return strings.TrimSpace(c.Template) + "\n\n" + args
}

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// maxTemplateBytes 防止误放大文件进命令目录。
const maxTemplateBytes = 64 << 10

// UserDir 返回用户级命令目录。
func UserDir() string {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "commands")
}

// Load 读取用户级和项目级命令，按名字排序；reserved 里的名字（内置命令）被跳过。
func Load(workspace string, reserved map[string]bool) []Command {
	byName := map[string]Command{}
	if d := UserDir(); d != "" {
		loadDir(d, "用户", byName, reserved)
	}
	if pd := appdir.ProjectDir(workspace); workspace != "" && pd != "" {
		loadDir(filepath.Join(pd, "commands"), "项目", byName, reserved)
	}
	out := make([]Command, 0, len(byName))
	for _, c := range byName {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func loadDir(dir, source string, into map[string]Command, reserved map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		if !validName.MatchString(name) || reserved[strings.ToLower(name)] {
			continue
		}
		p := filepath.Join(dir, e.Name())
		info, err := os.Stat(p) // 跟随符号链接，但只接受普通小文件
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxTemplateBytes {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		desc, body := parseFrontmatter(string(data))
		if strings.TrimSpace(body) == "" {
			continue
		}
		if desc == "" {
			desc = firstLine(body)
		}
		into[name] = Command{Name: name, Description: desc, Template: body, Source: source, Path: p}
	}
}

// parseFrontmatter 解析开头 --- 包住的 key: value，只取 description。
func parseFrontmatter(s string) (desc, body string) {
	s = strings.TrimPrefix(s, "\ufeff")
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return "", s
	}
	rest := s[strings.Index(s, "\n")+1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", s
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(k) == "description" {
			desc = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	body = rest[end+4:]
	if i := strings.Index(body, "\n"); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	return desc, body
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(strings.TrimLeft(l, "# ")); l != "" {
			if r := []rune(l); len(r) > 40 {
				return string(r[:40]) + "…"
			}
			return l
		}
	}
	return ""
}
