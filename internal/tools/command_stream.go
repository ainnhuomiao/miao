package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	defaultCommandTimeout = 2 * time.Minute
	maxCommandTimeout     = 10 * time.Minute
	progressInterval      = 100 * time.Millisecond
)

// liveBuffer 收集命令输出，并节流地把最新一行推给 progress。
type liveBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	progress ProgressFunc
	last     time.Time
	pending  string
}

func (b *liveBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf.Len() < maxCommandOutput*2 { // 超出部分丢弃，最终仍会截断
		b.buf.Write(p)
	}
	if line := lastLine(string(p)); line != "" {
		b.pending = line
	}
	if b.pending != "" && time.Since(b.last) >= progressInterval {
		b.progress(b.pending)
		b.last, b.pending = time.Now(), ""
	}
	return len(p), nil
}

func (b *liveBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\r\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(strings.TrimRight(lines[i], "\r")); l != "" {
			if j := strings.LastIndex(l, "\r"); j >= 0 { // 进度条类输出取最后一段
				l = l[j+1:]
			}
			return l
		}
	}
	return ""
}

// execCommandStream 前台执行命令：输出实时推到工具行，超时后终止。
func execCommandStream(ctx context.Context, dir, name string, args []string, timeout time.Duration) Result {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = dir
	out := &liveBuffer{progress: progressFrom(ctx)}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	text := truncateOutput(strings.TrimRight(out.String(), "\n"), maxCommandOutput)
	if runCtx.Err() == context.DeadlineExceeded {
		return Result{Content: fmt.Sprintf("命令超时（>%s），已终止。长时间运行的命令可以设 background=true 放到后台。\n%s", timeout, text), IsError: true}
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		} else {
			text += "\n(" + err.Error() + ")"
		}
		return Result{Content: text, IsError: true}
	}
	if text == "" {
		text = "(无输出)"
	}
	return Result{Content: text}
}

// commandTimeoutFor 把模型给的秒数换成超时：默认 2 分钟，上限 10 分钟。
func commandTimeoutFor(sec int) time.Duration {
	if sec <= 0 {
		return defaultCommandTimeout
	}
	return min(time.Duration(sec)*time.Second, maxCommandTimeout)
}
