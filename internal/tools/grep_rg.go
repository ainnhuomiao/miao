package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// rgTimeout 是单次 ripgrep 的超时。
	rgTimeout = 30 * time.Second
	// rgMaxOutput 是读取 ripgrep 标准输出的上限；超出即杀进程并改用 Go 实现（它按文件流式扫描，内存有界）。
	rgMaxOutput = 32 << 20
)

// rgLookPath 查找 rg；测试可替换。
var rgLookPath = func() (string, error) { return exec.LookPath("rg") }

// errRgOverCap 表示 ripgrep 输出超过上限。
var errRgOverCap = errors.New("ripgrep 输出超过上限")

// rgArgv 构造 ripgrep 参数。安全约束：
//   - 显式 argv，不经 shell；--no-config 忽略 RIPGREP_CONFIG_PATH（防止注入 --pre）；
//   - pattern 用 -e 传，路径放在 -- 之后，以 '-' 开头的 pattern / 路径都不会被当成选项；
//   - 不传 -L/--follow、--hidden、--pre、-z、--no-ignore/-u。
//
// 忽略规则尽量贴近 Go 实现：只认 .gitignore（不读全局 excludes、.ignore/.rgignore、上级目录），
// 不要求处于 git 仓库内；按路径排序，保证与 Go 实现（WalkDir 字典序）同序。
func rgArgv(pattern string, ignoreCase bool, base string) []string {
	argv := []string{
		"--no-config",
		"--json",
		"--sort", "path",
		"--no-ignore-global",
		"--no-ignore-parent",
		"--no-ignore-dot",
		"--no-ignore-exclude",
		"--no-require-git",
		"--no-messages",
	}
	if ignoreCase {
		argv = append(argv, "-i")
	}
	return append(argv, "-e", pattern, "--", base)
}

// rgEnv 返回去掉 ripgrep 相关环境变量后的环境（--no-config 之外再加一层保险）。
func rgEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "RIPGREP_CONFIG_PATH=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

type rgText struct {
	Text  *string `json:"text"`
	Bytes *string `json:"bytes"`
}

func (t rgText) value() (string, bool) {
	if t.Text != nil {
		return *t.Text, true
	}
	if t.Bytes != nil {
		b, err := base64.StdEncoding.DecodeString(*t.Bytes)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
	return "", false
}

type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path       rgText `json:"path"`
		Lines      rgText `json:"lines"`
		LineNumber int    `json:"line_number"`
	} `json:"data"`
}

// grepRipgrep 用 ripgrep 搜索；ok=false 表示应退回 Go 实现（rg 不存在、出错、超时、输出超限等）。
// 结果再经过与 Go 实现相同的过滤：敏感文件、根 .gitignore、glob、符号链接越界、二进制文件。
func (r *Registry) grepRipgrep(ctx context.Context, pattern string, ignoreCase bool, glob, base string, baseIsDir bool) (string, bool) {
	bin, err := rgLookPath()
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, rgTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, rgArgv(pattern, ignoreCase, base)...)
	cmd.Dir = r.workspace
	cmd.Env = rgEnv()
	cmd.Stdin = nil
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", false
	}
	if err := cmd.Start(); err != nil {
		return "", false
	}
	lines, total, perr := r.parseRgOutput(io.LimitReader(stdout, rgMaxOutput+1), glob, base, baseIsDir)
	if perr != nil {
		_ = cmd.Process.Kill()
	}
	werr := cmd.Wait()
	if perr != nil || ctx.Err() != nil {
		return "", false
	}
	if werr != nil {
		var ee *exec.ExitError
		// 退出码 1 = 没有匹配；其他（2 = 出错，如正则语法与 Go 不兼容）退回 Go 实现。
		if !errors.As(werr, &ee) || ee.ExitCode() != 1 {
			return "", false
		}
	}
	return formatResults(lines, total), true
}

func (r *Registry) parseRgOutput(src io.Reader, glob, base string, baseIsDir bool) ([]string, int, error) {
	gi := loadGitIgnore(r.workspace)
	allowed := map[string]bool{} // 按文件缓存过滤结果
	var lines []string
	total := 0
	read := 0
	br := bufio.NewReaderSize(src, 64<<10)
	for {
		raw, err := br.ReadBytes('\n')
		read += len(raw)
		if read > rgMaxOutput {
			return nil, 0, errRgOverCap
		}
		if len(raw) > 0 {
			var ev rgEvent
			if json.Unmarshal(raw, &ev) == nil && ev.Type == "match" {
				full, ok1 := ev.Data.Path.value()
				text, ok2 := ev.Data.Lines.value()
				if !ok1 || !ok2 || ev.Data.LineNumber <= 0 {
					return nil, 0, fmt.Errorf("无法解析 ripgrep 输出")
				}
				ok, seen := allowed[full]
				if !seen {
					ok = r.rgFileAllowed(gi, full, glob, base, baseIsDir)
					allowed[full] = ok
				}
				if ok {
					rel, _ := filepath.Rel(r.workspace, full)
					rel = filepath.ToSlash(rel)
					// 与 bufio.Scanner 一致：去掉行尾 \n 和一个 \r。多行匹配不会出现（未开 -U）。
					text = strings.TrimSuffix(text, "\n")
					text = strings.TrimSuffix(text, "\r")
					total++
					if len(lines) < maxSearchResults {
						lines = append(lines, fmt.Sprintf("%s:%d: %s", rel, ev.Data.LineNumber, truncGrepLine(text)))
					}
				}
			}
		}
		if err == io.EOF {
			return lines, total, nil
		}
		if err != nil {
			return nil, 0, err
		}
	}
}

// rgFileAllowed 对 ripgrep 报出的文件重做 Go 实现的过滤。
func (r *Registry) rgFileAllowed(gi *gitIgnore, full, glob, base string, baseIsDir bool) bool {
	if !filepath.IsAbs(full) {
		full = filepath.Join(r.workspace, full)
	}
	rel, err := filepath.Rel(r.workspace, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	rel = filepath.ToSlash(rel)
	if baseIsDir {
		// 目录搜索：逐级检查 .git、根 .gitignore、敏感路径（Go 实现遍历时会跳过整个目录）。
		parts := strings.Split(rel, "/")
		for i := range parts {
			prefix := strings.Join(parts[:i+1], "/")
			last := i == len(parts)-1
			if parts[i] == ".git" || gi.Ignored(prefix, !last) || isSensitive(prefix) {
				return false
			}
		}
	} else if isSensitive(rel) {
		return false
	}
	// 目录搜索时不跟随符号链接（与 WalkDir 一致）；单文件目标已在调用方做过越界检查，与 Go 实现一样按 Stat 判断。
	stat := os.Lstat
	if !baseIsDir {
		stat = os.Stat
	}
	fi, err := stat(full)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	if r.checkNoSymlinkEscape(full) != nil {
		return false
	}
	if !grepGlobOK(glob, base, full, rel, baseIsDir) {
		return false
	}
	// 与 Go 实现一致：前 8000 字节含 NUL 视为二进制，跳过。
	f, err := os.Open(full)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 8000)
	n, _ := f.Read(head)
	return bytes.IndexByte(head[:n], 0) < 0
}
