package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ainnhuomiao/miao/internal/childenv"
)

// bgJob 是一个后台运行的命令。
type bgJob struct {
	id      string
	display string
	cmd     *exec.Cmd
	out     *liveBuffer
	start   time.Time
	done    chan struct{}
	err     error
}

type jobTable struct {
	mu   sync.Mutex
	next int
	jobs map[string]*bgJob
}

var jobTables sync.Map // *Registry -> *jobTable

func (r *Registry) jobs() *jobTable {
	v, _ := jobTables.LoadOrStore(r, &jobTable{jobs: map[string]*bgJob{}})
	return v.(*jobTable)
}

// startBackground 在后台启动命令，等 2 秒收集开头输出后返回任务 id。
func (r *Registry) startBackground(name string, args []string, display string) Result {
	t := r.jobs()
	t.mu.Lock()
	running := 0
	for _, j := range t.jobs {
		select {
		case <-j.done:
		default:
			running++
		}
	}
	if running >= 4 {
		t.mu.Unlock()
		return Result{Content: "后台任务已达上限 4 个，请先用 command_output 的 kill 结束不用的任务", IsError: true}
	}
	t.next++
	id := fmt.Sprintf("bg%d", t.next)
	t.mu.Unlock()

	name, args = wrapSandbox(r.Workspace(), name, args)
	cmd := exec.Command(name, args...)
	cmd.Dir = r.Workspace()
	cmd.Env = childenv.Environ()
	out := &liveBuffer{progress: func(string) {}}
	cmd.Stdout, cmd.Stderr = out, out
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return Result{Content: "启动失败: " + err.Error(), IsError: true}
	}
	j := &bgJob{id: id, display: display, cmd: cmd, out: out, start: time.Now(), done: make(chan struct{})}
	go func() { j.err = cmd.Wait(); close(j.done) }()
	t.mu.Lock()
	t.jobs[id] = j
	t.mu.Unlock()

	select {
	case <-j.done:
	case <-time.After(2 * time.Second):
	}
	return Result{Content: fmt.Sprintf("已在后台启动 %s（任务 %s）。用 command_output 查看输出或结束它。\n%s", display, id, j.status())}
}

func (j *bgJob) status() string {
	state := fmt.Sprintf("运行中（%s）", time.Since(j.start).Round(time.Second))
	done := false
	code := 0
	select {
	case <-j.done:
		done = true
		state = "已结束"
		if j.err != nil {
			state += "（" + j.err.Error() + "）"
			code = commandExitCode(j.err)
		}
	default:
	}
	text := strings.TrimRight(j.out.String(), "\n")
	text = truncateOutput(text, maxCommandOutput)
	if text == "" {
		text = "(暂无输出)"
	}
	out := "状态：" + state + "\n" + text
	if done {
		return withExitCode(out, code)
	}
	return out
}

// KillJobs 结束所有后台任务（程序退出时调用），连同子进程一起。
func (r *Registry) KillJobs() {
	t := r.jobs()
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, j := range t.jobs {
		_ = killGroup(j.cmd)
	}
}

// RunningJobs 返回仍在运行的后台任务数。
func (r *Registry) RunningJobs() int {
	t := r.jobs()
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, j := range t.jobs {
		select {
		case <-j.done:
		default:
			n++
		}
	}
	return n
}

// setProcessGroup 让命令自成进程组，超时/kill 时能连子进程一起结束。
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if cmd.Cancel != nil { // 只有 CommandContext 创建的命令才能设 Cancel
		cmd.Cancel = func() error { return killGroup(cmd) }
	}
}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}

// registerCommandOutput 注册 command_output：查看或结束后台任务。
func registerCommandOutput(r *Registry) {
	r.Register(Tool{
		Def: toolDef("command_output", "查看或结束后台 run_command 任务。不带 id 列出全部；给 id 看该任务输出；kill=true 结束它。输出过长时同样保留开头和末尾。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":   map[string]any{"type": "string", "description": "后台任务 id，如 bg1"},
				"kill": map[string]any{"type": "boolean", "description": "为 true 时结束该任务"},
			},
		}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				ID   string `json:"id"`
				Kill bool   `json:"kill"`
			}
			if len(args) > 0 {
				if err := json.Unmarshal(args, &in); err != nil {
					return Result{Content: "参数解析失败: " + err.Error(), IsError: true}
				}
			}
			t := r.jobs()
			t.mu.Lock()
			defer t.mu.Unlock()
			if in.ID == "" {
				if len(t.jobs) == 0 {
					return Result{Content: "没有后台任务"}
				}
				var lines []string
				for i := 1; i <= t.next; i++ {
					if j, ok := t.jobs[fmt.Sprintf("bg%d", i)]; ok {
						st, _, _ := strings.Cut(j.status(), "\n")
						lines = append(lines, j.id+"  "+j.display+"  "+st)
					}
				}
				return Result{Content: strings.Join(lines, "\n")}
			}
			j, ok := t.jobs[in.ID]
			if !ok {
				return Result{Content: "没有这个后台任务: " + in.ID, IsError: true}
			}
			if in.Kill {
				select {
				case <-j.done:
				default:
					_ = killGroup(j.cmd)
					select {
					case <-j.done:
					case <-time.After(2 * time.Second):
					}
				}
			}
			return Result{Content: j.status()}
		},
	})
}
