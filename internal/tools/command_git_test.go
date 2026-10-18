package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// PoC：仓库 .git/config 里的 core.fsmonitor / core.hooksPath / diff.external 能让免审批的
// git status / git diff 执行任意程序。加固后这些配置被 -c 覆盖。
func TestGitReadOnlyIgnoresRepoExecConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	ws := t.TempDir()
	marker := filepath.Join(ws, "pwned")
	evil := filepath.Join(ws, "evil.sh")
	if err := os.WriteFile(evil, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = ws
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-qm", "init")
	run("config", "core.fsmonitor", evil)
	run("config", "diff.external", evil)
	run("config", "core.pager", evil)
	run("config", "core.hooksPath", ws)
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"status"}, {"diff"}, {"log", "-p", "-1"}, {"show"}} {
		res := execCommandStream(context.Background(), ws, "git", args, time.Minute)
		if res.IsError {
			t.Fatalf("git %v: %s", args, res.Content)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("git %v 执行了仓库配置里的程序", args)
		}
	}
	res := execCommandStream(context.Background(), ws, "git", []string{"diff"}, time.Minute)
	if !strings.Contains(res.Content, "+b") {
		t.Fatalf("git diff 应仍正常输出: %s", res.Content)
	}
}

func TestRiskyGitConfigKey(t *testing.T) {
	for _, k := range []string{"filter.lfs.clean", "filter.LFS.process", "gpg.program", "core.sshCommand", "diff.py.textconv", "textconv"} {
		if !riskyGitConfigKey(k) {
			t.Errorf("%s 应视为可执行配置", k)
		}
	}
	for _, k := range []string{"core.pager", "user.email", "diff.external", "log.showsignature", "include.path"} {
		if riskyGitConfigKey(k) {
			t.Errorf("%s 不应视为可执行配置", k)
		}
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func initPlainRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Setenv("MIAO_HOME", t.TempDir())
	ws := t.TempDir()
	gitRun(t, ws, "init", "-q")
	gitRun(t, ws, "config", "user.email", "t@t")
	gitRun(t, ws, "config", "user.name", "t")
	return ws
}

func TestUntrustedGitExecConfigGatesReadOnly(t *testing.T) {
	ws := initPlainRepo(t)
	if UntrustedGitExecConfig(ws) || needsApprovalIn(ws, "git", []string{"status"}) {
		t.Fatal("只有 user.name 的仓库不应拦截 git status")
	}
	evil := filepath.Join(ws, "evil.sh")
	marker := filepath.Join(ws, "pwned")
	if err := os.WriteFile(evil, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, ws, "config", "filter.evil.clean", evil)
	gitRun(t, ws, "config", "filter.evil.required", "true")
	gitRun(t, ws, "config", "core.fsmonitor", evil)
	if err := os.WriteFile(filepath.Join(ws, ".gitattributes"), []byte("* filter=evil\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Remove(marker)
	if !UntrustedGitExecConfig(ws) {
		t.Fatal("filter.* 应被识别")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("检查配置时不应执行 filter 或 fsmonitor")
	}
	for _, args := range [][]string{{"status"}, {"diff"}, {"log"}, {"show"}, {"blame"}} {
		if !needsApprovalIn(ws, "git", args) {
			t.Errorf("git %v 应需要审批", args)
		}
	}
	r, err := NewRegistry(ws)
	if err != nil {
		t.Fatal(err)
	}
	RegisterBuiltins(r)
	var display string
	r.SetApprover(nil)
	res := r.Call(context.Background(), "run_command", `{"command":"git","args":["status"]}`)
	if !res.IsError {
		t.Fatalf("无审批时应拒绝: %+v", res)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("拒绝审批前不应执行 git status")
	}
	r.SetApprover(func(_ context.Context, _ string, disp string, _ json.RawMessage) (bool, error) {
		display = disp
		return false, nil
	})
	res = r.Call(context.Background(), "run_command", `{"command":"git","args":["status"]}`)
	if !res.IsError || !strings.Contains(res.Content, "用户拒绝") {
		t.Fatalf("用户拒绝: %+v", res)
	}
	if !strings.Contains(display, "filter") || !strings.Contains(display, "textconv") {
		t.Fatalf("审批说明应写明可执行配置: %s", display)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("用户拒绝后不应执行 git status")
	}
}

func TestGitExecConfigFollowsInclude(t *testing.T) {
	ws := initPlainRepo(t)
	if err := os.WriteFile(filepath.Join(ws, ".git", "extra.cfg"), []byte("[filter \"e\"]\n\tclean = /tmp/evil\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, ws, "config", "include.path", "extra.cfg")
	if !UntrustedGitExecConfig(ws) {
		t.Fatal("include 进来的 filter 应被识别")
	}
	if err := os.WriteFile(filepath.Join(ws, ".git", "extra.cfg"), []byte("[user]\n\tname = t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if UntrustedGitExecConfig(ws) {
		t.Fatal("include 文件去掉 filter 后不应再拦截")
	}
}

func TestGitExecConfigKeys(t *testing.T) {
	keys := [][]string{
		{"core.sshCommand", "evil"},
		{"gpg.program", "evil"},
		{"diff.py.textconv", "evil"},
	}
	for _, kv := range keys {
		ws := initPlainRepo(t)
		gitRun(t, ws, "config", kv[0], kv[1])
		if !UntrustedGitExecConfig(ws) {
			t.Errorf("%s 应需要审批", kv[0])
		}
	}
}

func TestGitLogDoesNotRunGpgProgram(t *testing.T) {
	ws := initPlainRepo(t)
	evil := filepath.Join(ws, "evil.sh")
	marker := filepath.Join(ws, "pwned")
	if err := os.WriteFile(evil, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, ws, "add", "a.txt")
	gitRun(t, ws, "commit", "-qm", "init")
	tree := strings.TrimSpace(gitOut(t, ws, "rev-parse", "HEAD^{tree}"))
	parent := strings.TrimSpace(gitOut(t, ws, "rev-parse", "HEAD"))
	body := fmt.Sprintf("tree %s\nparent %s\nauthor t <t@t> 1700000000 +0000\ncommitter t <t@t> 1700000000 +0000\ngpgsig -----BEGIN PGP SIGNATURE-----\n Version: fake\n not-a-real-signature\n -----END PGP SIGNATURE-----\n\nsigned\n", tree, parent)
	if err := os.WriteFile(filepath.Join(ws, "commit.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	hash := gitOut(t, ws, "hash-object", "-t", "commit", "-w", "commit.txt")
	gitRun(t, ws, "reset", "-q", "--soft", strings.TrimSpace(hash))
	gitRun(t, ws, "config", "gpg.program", evil)
	gitRun(t, ws, "config", "log.showSignature", "true")
	os.Remove(marker)
	raw := exec.Command("git", "log", "-1", "--format=%s")
	raw.Dir = ws
	raw.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if _, err := raw.CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("对照：未加固的 git log 应执行 gpg.program")
	}
	os.Remove(marker)
	res := execCommandStream(context.Background(), ws, "git", []string{"log", "-1", "--format=%s"}, time.Minute)
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("加固后的 git log 仍执行了 gpg.program: %s", res.Content)
	}
	if !strings.Contains(res.Content, "signed") {
		t.Fatalf("git log 应仍输出提交说明: %s", res.Content)
	}
}

func TestGitBlameSkipsTextconv(t *testing.T) {
	ws := initPlainRepo(t)
	evil := filepath.Join(ws, "evil.sh")
	marker := filepath.Join(ws, "pwned")
	if err := os.WriteFile(evil, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, ws, "add", "a.txt")
	gitRun(t, ws, "commit", "-qm", "init")
	gitRun(t, ws, "config", "diff.evil.textconv", evil)
	if err := os.WriteFile(filepath.Join(ws, ".gitattributes"), []byte("* diff=evil\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Remove(marker)
	raw := exec.Command("git", "blame", "a.txt")
	raw.Dir = ws
	raw.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	_, _ = raw.CombinedOutput()
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("对照：未加固的 git blame 应执行 textconv")
	}
	os.Remove(marker)
	res := execCommandStream(context.Background(), ws, "git", []string{"blame", "a.txt"}, time.Minute)
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("加固后的 git blame 仍执行了 textconv: %s", res.Content)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}
