package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var makefileTargetRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*):`)

// GenerateAgentsMD 扫描工作区，生成一份 AGENTS.md 草稿。
func GenerateAgentsMD(workspace string) (string, error) {
	if workspace == "" {
		return "", fmt.Errorf("工作区为空")
	}
	var b strings.Builder
	b.WriteString("# AGENTS.md\n\n")
	b.WriteString("给编码助手的项目说明。改代码前先读相关文件，改完跑测试。\n\n")

	if heading := readmeHeading(workspace); heading != "" {
		fmt.Fprintf(&b, "项目：%s\n\n", heading)
	}

	writeGoSection(&b, workspace)
	writeNodeSection(&b, workspace)
	writeMakeSection(&b, workspace)
	if _, err := os.Stat(filepath.Join(workspace, "flake.nix")); err == nil {
		b.WriteString("## Nix\n\n仓库用 Nix flake 管理开发环境，改动后可用 `nix flake check`。\n\n")
	}
	if _, err := os.Stat(filepath.Join(workspace, ".github", "workflows")); err == nil {
		b.WriteString("## CI\n\n有 GitHub Actions workflows，改动后留意 CI。\n\n")
	}

	b.WriteString("## 约定\n\n")
	b.WriteString("- 工具只能访问工作区相对路径；不要读 `.env`，不要改 `.git`\n")
	b.WriteString("- 需要执行命令时用 `run_command`，不要编造输出\n")

	if pkgs := goPackageDirs(workspace, 24); len(pkgs) > 0 {
		b.WriteString("\n## 代码布局\n\n")
		for _, p := range pkgs {
			fmt.Fprintf(&b, "- `%s/`\n", p)
		}
	}
	return b.String(), nil
}

func writeGoSection(b *strings.Builder, workspace string) {
	mod := readHead(filepath.Join(workspace, "go.mod"), 12)
	if mod == "" {
		return
	}
	b.WriteString("## Go\n\n```\n")
	b.WriteString(mod)
	if !strings.HasSuffix(mod, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n\n")
	b.WriteString("- 测试：`go test ./...`\n")
	if _, err := os.Stat(filepath.Join(workspace, "cmd")); err == nil {
		b.WriteString("- 构建：`go build ./cmd/...`\n")
	} else {
		b.WriteString("- 构建：`go build ./...`\n")
	}
	b.WriteByte('\n')
}

func writeNodeSection(b *strings.Builder, workspace string) {
	data, err := os.ReadFile(filepath.Join(workspace, "package.json"))
	if err != nil {
		return
	}
	var pkg struct {
		Name    string            `json:"name"`
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		b.WriteString("## Node\n\n项目含 `package.json`。优先用仓库里已有的 lint / test 脚本。\n\n")
		return
	}
	b.WriteString("## Node\n\n")
	if pkg.Name != "" {
		fmt.Fprintf(b, "包名 `%s`。\n", pkg.Name)
	}
	if len(pkg.Scripts) > 0 {
		b.WriteString("常用脚本：")
		n := 0
		for name := range pkg.Scripts {
			if n > 0 {
				b.WriteString("、")
			}
			fmt.Fprintf(b, "`npm run %s`", name)
			n++
			if n >= 8 {
				break
			}
		}
		b.WriteString("\n")
	}
	b.WriteByte('\n')
}

func writeMakeSection(b *strings.Builder, workspace string) {
	data, err := os.ReadFile(filepath.Join(workspace, "Makefile"))
	if err != nil {
		return
	}
	seen := map[string]bool{}
	var targets []string
	for _, line := range strings.Split(string(data), "\n") {
		m := makefileTargetRe.FindStringSubmatch(line)
		if m == nil || strings.HasPrefix(m[1], ".") || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		targets = append(targets, m[1])
		if len(targets) >= 12 {
			break
		}
	}
	if len(targets) == 0 {
		return
	}
	b.WriteString("## Make\n\n")
	for _, t := range targets {
		fmt.Fprintf(b, "- `make %s`\n", t)
	}
	b.WriteByte('\n')
}

func readmeHeading(workspace string) string {
	for _, name := range []string{"README.md", "README"} {
		for _, line := range strings.Split(readHead(filepath.Join(workspace, name), 40), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "# ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "#"))
			}
		}
	}
	return ""
}

func goPackageDirs(workspace string, limit int) []string {
	var out []string
	_ = filepath.WalkDir(workspace, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == ".git" || name == "vendor" || name == "node_modules" || name == "testdata" || name == ".miao" || name == ".agnet" {
			if path != workspace {
				return filepath.SkipDir
			}
		}
		if strings.HasPrefix(name, ".") && path != workspace {
			return filepath.SkipDir
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil
		}
		hasGo := false
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
				hasGo = true
				break
			}
		}
		if !hasGo {
			return nil
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil || rel == "." {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	return out
}

func readHead(path string, maxLines int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// WriteAgentsMD 把内容写入工作区 AGENTS.md；已存在且 overwrite 为 false 时拒绝。
func WriteAgentsMD(workspace, content string, overwrite bool) error {
	path := filepath.Join(workspace, AgentsMDName)
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return os.ErrExist
		}
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
