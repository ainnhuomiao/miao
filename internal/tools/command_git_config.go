package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ainnhuomiao/miao/internal/childenv"
	"github.com/ainnhuomiao/miao/internal/config"
)

// git 的 --get-regexp 使用 POSIX ERE。--local 不展开 include.path / includeIf，
// 攻击者可以把 filter 写进被包含的文件，所以这里关掉系统级和全局配置后再查，只看仓库自己的配置。
const gitExecConfigRE = `^(filter\.|gpg\.|core\.sshcommand$)|textconv$`

// UntrustedGitExecConfig 报告未信任工作区的本地 git 配置（含 include）里是否有能执行程序的项：
// filter.*、gpg.*、core.sshCommand、*.textconv。已信任的项目配置不再拦截。
func UntrustedGitExecConfig(workspace string) bool {
	if workspace == "" || config.ProjectTrustedFor(workspace) {
		return false
	}
	stamp, cacheable := gitConfigStamp(workspace)
	if stamp == "" {
		return false // 没有 .git
	}
	if cacheable {
		gitRiskMu.Lock()
		if ent, ok := gitRiskCache[workspace]; ok && ent.stamp == stamp {
			gitRiskMu.Unlock()
			return ent.risk
		}
		gitRiskMu.Unlock()
	}
	risk := queryGitExecConfig(workspace)
	if cacheable {
		gitRiskMu.Lock()
		gitRiskCache[workspace] = gitRiskEntry{stamp: stamp, risk: risk}
		gitRiskMu.Unlock()
	}
	return risk
}

type gitRiskEntry struct {
	stamp string
	risk  bool
}

var (
	gitRiskMu    sync.Mutex
	gitRiskCache = map[string]gitRiskEntry{}
)

func riskyGitConfigKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if strings.HasPrefix(k, "filter.") || strings.HasPrefix(k, "gpg.") {
		return true
	}
	if k == "core.sshcommand" {
		return true
	}
	return k == "textconv" || strings.HasSuffix(k, ".textconv")
}

func queryGitExecConfig(workspace string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", childenv.GitArgs([]string{
		"-c", "alias.config=",
		"config", "--get-regexp", gitExecConfigRE,
	})...)
	cmd.Dir = workspace
	cmd.Env = setEnv(setEnv(childenv.GitEnv(), "GIT_CONFIG_NOSYSTEM", "1"), "GIT_CONFIG_GLOBAL", "/dev/null")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if err == nil {
		return gitConfigOutputRisky(stdout.String())
	}
	if ctx.Err() != nil {
		return true // 查不清（例如恶意 include 让 git 卡住）就当作有风险
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		switch ee.ExitCode() {
		case 1, 128: // 1 没有匹配的键；128 不是 git 仓库
			return false
		default:
			return true
		}
	}
	return false // 没有 git 可执行文件时，后面的 git 命令自己会失败
}

func gitConfigOutputRisky(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, _, _ := strings.Cut(line, " ")
		if riskyGitConfigKey(key) {
			return true
		}
	}
	return false
}

func setEnv(env []string, key, val string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if e == key || strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	return append(out, prefix+val)
}

// gitConfigStamp 给 .git/config 及其 include 链算一个摘要。工作区没有 .git 时返回空串。
// .git 如果是文件（worktree / submodule），不缓存，调用方每次都重新查询。
func gitConfigStamp(workspace string) (stamp string, cacheable bool) {
	meta := filepath.Join(workspace, ".git")
	st, err := os.Lstat(meta)
	if err != nil {
		return "", false
	}
	if !st.IsDir() {
		return "file", false
	}
	h := sha256.New()
	seen := map[string]bool{}
	if !hashGitConfig(h, filepath.Join(meta, "config"), seen, 0) {
		return "uncacheable", false
	}
	return fmt.Sprintf("%x", h.Sum(nil)), true
}

// hashGitConfig 把配置文件及其 include 的内容写进 h。
// 返回 false 表示文件不是普通小文件，或 include 链太深，调用方不要缓存查询结果。
func hashGitConfig(h hash.Hash, path string, seen map[string]bool, depth int) bool {
	if seen[path] {
		return true
	}
	if depth > 4 {
		return false
	}
	seen[path] = true
	st, err := os.Lstat(path)
	if err != nil {
		fmt.Fprintf(h, "missing %s\n", path)
		return true
	}
	fmt.Fprintf(h, "stat %s %d %d\n", path, st.Mode(), st.Size())
	if !st.Mode().IsRegular() || st.Size() > 256<<10 {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(h, "unreadable %s\n", path)
		return false
	}
	h.Write(b)
	dir := filepath.Dir(path)
	for _, inc := range includePaths(string(b)) {
		if !hashGitConfig(h, resolveGitInclude(dir, inc), seen, depth+1) {
			return false
		}
	}
	return true
}

func includePaths(cfg string) []string {
	var paths []string
	include := false
	for _, line := range strings.Split(cfg, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, ";") {
			continue
		}
		if strings.HasPrefix(s, "[") && strings.Contains(s, "]") {
			name := strings.ToLower(s[1:strings.IndexByte(s, ']')])
			name = strings.Trim(strings.TrimSpace(name), `"`)
			include = name == "include" || strings.HasPrefix(name, "includeif ")
			continue
		}
		if !include {
			continue
		}
		k, v, ok := strings.Cut(s, "=")
		if !ok || strings.ToLower(strings.TrimSpace(k)) != "path" {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"`)
		if v != "" {
			paths = append(paths, v)
		}
	}
	return paths
}

func resolveGitInclude(fromDir, path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				path = home
			} else {
				path = filepath.Join(home, path[2:])
			}
		}
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(fromDir, path)
}
