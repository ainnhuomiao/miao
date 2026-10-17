// Package repl 实现终端里的交互循环（Read-Eval-Print Loop）。
package repl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"

	"agnet/internal/agent"
	"agnet/internal/llm"
)

// ANSI 转义序列，用于给终端输出上色。
const (
	colorReset  = "\033[0m"
	colorDim    = "\033[2m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
)

// REPL 负责读输入、打印流式输出、处理斜杠命令和 Ctrl+C。
type REPL struct {
	ag    *agent.Agent
	model string
	in    *bufio.Reader
	out   io.Writer

	// 正在进行的请求的取消函数。信号处理 goroutine 和主循环都会访问，
	// 所以用互斥锁保护。
	mu     sync.Mutex
	cancel context.CancelFunc
}

// New 创建 REPL。model 只用于启动时展示。
func New(ag *agent.Agent, model string) *REPL {
	return &REPL{
		ag:    ag,
		model: model,
		in:    bufio.NewReader(os.Stdin),
		out:   os.Stdout,
	}
}

// Run 启动交互循环，直到用户输入 /exit、按 Ctrl+C（等待输入时）或 stdin 关闭。
func (r *REPL) Run(ctx context.Context) error {
	r.printBanner()

	// signal.Notify 把 Ctrl+C 从"默认直接杀死进程"改成"投递到 channel"。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	stop := make(chan struct{})
	defer close(stop)
	go r.watchSignals(sigCh, stop)

	for {
		fmt.Fprintf(r.out, "\n%s你 ›%s ", colorCyan, colorReset)

		line, err := r.in.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintf(r.out, "\n%s再见！%s\n", colorDim, colorReset)
				return nil
			}
			return fmt.Errorf("读取输入失败: %w", err)
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			quit, cmdErr := r.handleCommand(input)
			if cmdErr != nil {
				r.printWarn(cmdErr.Error())
			}
			if quit {
				return nil
			}
			continue
		}

		r.chat(ctx, input)
	}
}

// watchSignals 是运行在独立 goroutine 里的信号处理循环。
// 正在生成回答时只取消本次请求；等待输入时则退出程序。
func (r *REPL) watchSignals(sigCh <-chan os.Signal, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-sigCh:
			r.mu.Lock()
			cancel := r.cancel
			r.mu.Unlock()

			if cancel != nil {
				cancel()
				continue
			}
			fmt.Fprintf(r.out, "\n%s再见！%s\n", colorDim, colorReset)
			os.Exit(130) // 130 是 shell 中"被 SIGINT 中断"的惯例退出码
		}
	}
}

// chat 完成一轮对话，并实时打印模型输出。
func (r *REPL) chat(ctx context.Context, input string) {
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.cancel = nil
		r.mu.Unlock()
	}()

	fmt.Fprintf(r.out, "%sagent ›%s ", colorGreen, colorReset)

	inReasoning := false
	hasContent := false
	err := r.ag.Send(reqCtx, input, func(d llm.Delta) error {
		if d.Reasoning != "" {
			if !inReasoning {
				fmt.Fprint(r.out, colorDim)
				inReasoning = true
			}
			fmt.Fprint(r.out, d.Reasoning)
		}
		if d.Content != "" {
			if inReasoning {
				fmt.Fprint(r.out, colorReset+"\n\n")
				inReasoning = false
			}
			fmt.Fprint(r.out, d.Content)
			hasContent = true
		}
		return nil
	})
	if inReasoning {
		fmt.Fprint(r.out, colorReset)
	}
	fmt.Fprintln(r.out)

	switch {
	case errors.Is(err, context.Canceled):
		r.printWarn("已取消本次回答。")
	case err != nil:
		r.printWarn("请求失败: " + err.Error())
	case !hasContent:
		fmt.Fprintf(r.out, "%s(模型没有返回文本内容)%s\n", colorDim, colorReset)
	}
}

func (r *REPL) handleCommand(input string) (quit bool, err error) {
	fields := strings.Fields(input)
	switch fields[0] {
	case "/exit", "/quit":
		fmt.Fprintf(r.out, "%s再见！%s\n", colorDim, colorReset)
		return true, nil
	case "/help":
		r.printHelp()
		return false, nil
	case "/clear":
		r.ag.Reset()
		fmt.Fprintln(r.out, "对话历史已清空。")
		return false, nil
	case "/history":
		r.printHistory()
		return false, nil
	default:
		return false, fmt.Errorf("未知命令 %s，输入 /help 查看帮助", fields[0])
	}
}

func (r *REPL) printBanner() {
	fmt.Fprintf(r.out, "%sGoAgent%s · 用 Go 写的终端 AI 助手\n", colorCyan, colorReset)
	fmt.Fprintf(r.out, "%s模型: %s%s\n", colorDim, r.model, colorReset)
	fmt.Fprintf(r.out, "%s输入 /help 查看命令；等待输入时按 Ctrl+C 退出%s\n", colorDim, colorReset)
}

func (r *REPL) printHelp() {
	fmt.Fprint(r.out, `可用命令:
  /help     显示这份帮助
  /clear    清空对话历史
  /history  查看当前对话历史
  /exit     退出程序（也可以按 Ctrl+C）

提示: 生成回答时按 Ctrl+C 只会取消这一次回答，不会退出程序。
`)
}

func (r *REPL) printHistory() {
	history := r.ag.History()
	if len(history) == 0 {
		fmt.Fprintln(r.out, "（还没有对话）")
		return
	}
	for i, m := range history {
		label := "你"
		if m.Role == llm.RoleAssistant {
			label = "agent"
		}
		content := strings.ReplaceAll(m.Content, "\n", " ")
		fmt.Fprintf(r.out, "%2d. %-5s %s\n", i+1, label, truncate(content, 60))
	}
}

// truncate 按"字符"而不是字节截断。
// 中文在 UTF-8 中占 3 个字节，直接切字节会产生乱码。
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func (r *REPL) printWarn(msg string) {
	fmt.Fprintf(r.out, "%s%s%s\n", colorYellow, msg, colorReset)
}
