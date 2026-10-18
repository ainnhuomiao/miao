package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLastLine(t *testing.T) {
	cases := map[string]string{
		"a\nb\n":         "b",
		"a\n\n  \n":      "a",
		"10%\r50%\r100%": "100%",
		"":               "",
	}
	for in, want := range cases {
		if got := lastLine(in); got != want {
			t.Errorf("lastLine(%q)=%q want %q", in, got, want)
		}
	}
}

func TestCommandTimeoutFor(t *testing.T) {
	if commandTimeoutFor(0) != 2*time.Minute || commandTimeoutFor(5) != 5*time.Second || commandTimeoutFor(99999) != maxCommandTimeout {
		t.Fatal("timeout mapping wrong")
	}
}

func TestExecStreamProgressAndTimeout(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	ctx := WithProgress(context.Background(), func(l string) { mu.Lock(); lines = append(lines, l); mu.Unlock() })
	res := execCommandStream(ctx, t.TempDir(), "sh", []string{"-c", "echo hello; echo world"}, time.Minute)
	if res.IsError || !strings.Contains(res.Content, "world") || !strings.Contains(res.Content, "[exit code: 0]") {
		t.Fatalf("res=%+v", res)
	}
	mu.Lock()
	if len(lines) == 0 {
		t.Fatal("no progress")
	}
	mu.Unlock()
	res = execCommandStream(context.Background(), t.TempDir(), "sleep", []string{"5"}, 200*time.Millisecond)
	if !res.IsError || !strings.Contains(res.Content, "超时") || !strings.Contains(res.Content, "[exit code:") {
		t.Fatalf("want timeout, got %+v", res)
	}
}

func TestBackgroundJob(t *testing.T) {
	r, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.KillJobs()
	res := r.startBackground("sh", []string{"-c", "echo started; sleep 30"}, "sh")
	if res.IsError || !strings.Contains(res.Content, "bg1") || !strings.Contains(res.Content, "started") {
		t.Fatalf("start: %+v", res)
	}
	registerCommandOutput(r)
	out := r.Call(context.Background(), "command_output", `{}`)
	if !strings.Contains(out.Content, "bg1") || !strings.Contains(out.Content, "运行中") {
		t.Fatalf("list: %+v", out)
	}
	out = r.Call(context.Background(), "command_output", `{"id":"bg1","kill":true}`)
	if !strings.Contains(out.Content, "已结束") {
		t.Fatalf("kill: %+v", out)
	}
}

func TestTimeoutKillsChildren(t *testing.T) {
	start := time.Now()
	res := execCommandStream(context.Background(), t.TempDir(), "sh", []string{"-c", "sleep 30 & sleep 30; wait"}, 300*time.Millisecond)
	if !res.IsError || time.Since(start) > 2*time.Second {
		t.Fatalf("子进程没被一起结束: %v %+v", time.Since(start), res)
	}
}
