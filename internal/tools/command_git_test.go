package tools

import (
	"context"
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
