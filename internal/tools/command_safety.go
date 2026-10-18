package tools

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ainnhuomiao/miao/internal/config"
)

// 这些命令名在白名单里，但只有部分子命令/参数是只读安全的。
var (
	gitReadOnly = map[string]bool{"status": true, "log": true, "diff": true, "show": true,
		"rev-parse": true, "ls-files": true, "blame": true, "shortlog": true, "describe": true}
	// build/test/vet 会编译并运行仓库里的代码（测试、cgo、vettool），不再免审批。
	goSafe     = map[string]bool{"version": true, "list": true, "doc": true, "env": true}
	findDanger = map[string]bool{"-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
		"-delete": true, "-fprint": true, "-fprint0": true, "-fprintf": true, "-fls": true}
)

// HasDangerousFlag 报告命令参数里是否带有能写文件、执行任意程序，或绕过工作区的用法。
// 权限白名单（ALLOW_COMMANDS）放行前必须先过这一关。
func HasDangerousFlag(name string, args []string) bool {
	if name == "uniq" && uniqWrites(args) {
		return true
	}
	if name == "sort" && sortUnsafe(args) {
		return true
	}
	if name == "grep" && grepRecursive(args) {
		return true
	}
	if name == "diff" && diffRecursive(args) {
		return true
	}
	if name == "rg" && rgUnrestricted(args) {
		return true
	}
	for _, a := range args {
		if dangerousFlag(name, a) {
			return true
		}
	}
	return false
}

// CommandEscapesWorkspace 报告参数是否指向工作区外的路径（含绝对路径、.. 穿越、敏感路径、越界符号链接）。
func CommandEscapesWorkspace(workspace, name string, args []string) bool {
	if workspace == "" {
		return false
	}
	for _, a := range pathCandidates(args) {
		if argPathSensitiveOrOutside(workspace, a) {
			return true
		}
	}
	return false
}

// CommandDenied 报告命令是否命中 MIAO_DENY_COMMANDS（逗号分隔，按前缀匹配，如 curl,rm -rf,go test）。
func CommandDenied(name string, args []string) bool {
	v := strings.TrimSpace(config.Getenv("DENY_COMMANDS"))
	if v == "" {
		return false
	}
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	for _, rule := range strings.Split(v, ",") {
		rule = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rule), ":*"))
		if rule == "" {
			continue
		}
		if line == rule || strings.HasPrefix(line, rule+" ") {
			return true
		}
	}
	return false
}

// needsApproval 判断命令是否要走审批：白名单外，或白名单内但带写入/任意执行/越界路径的用法。
func needsApproval(name string, args []string) bool {
	if _, ok := commandAllowlist[name]; !ok {
		return true
	}
	if HasDangerousFlag(name, args) {
		return true
	}
	first := ""
	if len(args) > 0 {
		first = args[0]
	}
	switch name {
	case "git":
		return !gitReadOnly[first]
	case "go":
		if first == "env" && len(args) > 1 && strings.HasPrefix(args[1], "-w") {
			return true
		}
		return !goSafe[first]
	case "find":
		for _, a := range args {
			if findDanger[a] {
				return true
			}
		}
	}
	return false
}

func needsApprovalIn(workspace, name string, args []string) bool {
	return needsApproval(name, args) || CommandEscapesWorkspace(workspace, name, args) || ImplicitCommandRisk(workspace, name, args)
}

// ImplicitCommandRisk 报告参数里没有危险标志、但结合工作区仍会读到密钥或执行仓库配置里的程序。
// 权限白名单（ALLOW_COMMANDS）不能因此放行。
func ImplicitCommandRisk(workspace, name string, args []string) bool {
	switch name {
	case "diff":
		return diffDirectoryPair(workspace, args)
	case "git":
		return UntrustedGitExecConfig(workspace)
	default:
		return false
	}
}

// diffDirectoryPair 报告 diff 是否在比较两个目录。
// 不带 -r 时，同名文件（包括两边的 .env）也会被整文件打印出来；路径检查只看得到目录名。
func diffDirectoryPair(workspace string, args []string) bool {
	if workspace == "" {
		return false
	}
	n := 0
	for _, a := range pathCandidates(args) {
		p := a
		if !filepath.IsAbs(p) {
			p = filepath.Join(workspace, p)
		}
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			continue
		}
		n++
		if n >= 2 {
			return true
		}
	}
	return false
}

// dangerousFlag 识别能写文件或执行任意程序的参数（含 -flag=value 形式）。
func dangerousFlag(name, a string) bool {
	key, _, _ := strings.Cut(a, "=")
	if !strings.HasPrefix(a, "-") {
		return false
	}
	k := strings.TrimLeft(key, "-")
	switch name {
	case "go":
		switch k {
		case "exec", "toolexec", "o", "overlay", "modfile", "coverprofile",
			"cpuprofile", "memprofile", "blockprofile", "mutexprofile", "trace", "outputdir", "vettool":
			return true
		}
	case "git":
		return k == "output" || k == "ext-diff" || k == "textconv" ||
			strings.HasPrefix(k, "exec") || k == "c" || k == "config-env" ||
			k == "no-index" || k == "git-dir" || k == "work-tree" || k == "upload-pack"
	case "rg":
		return k == "pre" || k == "pre-glob" || k == "hostname-bin" || k == "config"
	case "grep":
		return k == "f" || k == "file"
	}
	return false
}

// uniqWrites：uniq [OPTION]... [INPUT [OUTPUT]]，两个位置参数时第二个是输出文件。
func uniqWrites(args []string) bool {
	n := 0
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--" {
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			k, _, eq := strings.Cut(strings.TrimLeft(a, "-"), "=")
			if !eq && (k == "f" || k == "s" || k == "w" || k == "skip-fields" || k == "skip-chars" || k == "check-chars") {
				skipNext = true
			}
			continue
		}
		if a == "-" {
			continue
		}
		n++
	}
	return n >= 2
}

// pathCandidates 抽出可能是文件系统路径的参数（含 --flag=path 的取值）。
func pathCandidates(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out = append(out, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			if _, v, ok := strings.Cut(a, "="); ok && v != "" {
				out = append(out, v)
			}
			continue
		}
		if a == "-" || a == "" {
			continue
		}
		out = append(out, a)
	}
	return out
}

func workspaceRel(workspace, arg string) (string, bool) {
	if arg == "" || arg == "-" || arg == "." {
		return ".", true
	}
	p := arg
	if !filepath.IsAbs(p) {
		p = filepath.Join(workspace, p)
	}
	p = filepath.Clean(p)
	ws := filepath.Clean(workspace)
	rel, err := filepath.Rel(ws, p)
	if err != nil {
		return "", false
	}
	return rel, true
}

// argPathSensitiveOrOutside 先 Clean 再判断越界和 .git/.env；已存在的路径会解析符号链接。
func argPathSensitiveOrOutside(workspace, arg string) bool {
	if arg == "" || arg == "-" || arg == "." {
		return false
	}
	rel, ok := workspaceRel(workspace, arg)
	if !ok {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	if isSensitive(rel) || isSensitive(filepath.Base(rel)) {
		return true
	}
	p := arg
	if !filepath.IsAbs(p) {
		p = filepath.Join(workspace, p)
	}
	p = filepath.Clean(p)
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		root = filepath.Clean(workspace)
	}
	check := p
	for {
		real, err := filepath.EvalSymlinks(check)
		if err == nil {
			rrel, err := filepath.Rel(root, real)
			if err != nil || rrel == ".." || strings.HasPrefix(rrel, ".."+string(filepath.Separator)) {
				return true
			}
			if isSensitive(rrel) {
				return true
			}
			return false
		}
		parent := filepath.Dir(check)
		if parent == check {
			return false
		}
		check = parent
	}
}
