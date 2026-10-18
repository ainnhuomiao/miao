package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// PoC：run_command 的子进程原先继承完整环境，`env` 能打印出 MIAO_API_KEY。
func TestRunCommandEnvDoesNotLeakAPIKey(t *testing.T) {
	t.Setenv("MIAO_API_KEY", "sk-run-leak")
	t.Setenv("AGNET_API_KEY", "sk-old-leak")
	t.Setenv("OPENAI_API_KEY", "sk-openai-leak")
	res := execCommandStream(context.Background(), t.TempDir(), "env", nil, time.Minute)
	if res.IsError {
		t.Fatal(res.Content)
	}
	for _, s := range []string{"sk-run-leak", "sk-old-leak", "sk-openai-leak"} {
		if strings.Contains(res.Content, s) {
			t.Fatalf("子进程环境泄露 %s", s)
		}
	}
	if !strings.Contains(res.Content, "PATH=") {
		t.Fatal("普通变量（PATH）应保留")
	}
}

func TestBackgroundCommandEnvDoesNotLeakAPIKey(t *testing.T) {
	t.Setenv("MIAO_API_KEY", "sk-bg-leak")
	r, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.startBackground("env", nil, "env")
	t.Cleanup(func() { jobTables.Delete(r) })
	for _, j := range r.jobs().jobs {
		<-j.done
		if strings.Contains(j.out.String(), "sk-bg-leak") {
			t.Fatal("后台命令环境泄露 MIAO_API_KEY")
		}
		if !strings.Contains(j.out.String(), "PATH=") {
			t.Fatal("后台命令普通变量应保留")
		}
	}
}
