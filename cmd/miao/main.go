// package main 表示这是可执行程序。Go 会从 main 函数开始执行。
package main

// import 导入要使用的包。这里的依赖全部来自 Go 标准库或本项目。
import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/llm"
)

func main() {
	// 环境变量由你在终端里设置。Getenv 只读取，不修改环境或配置文件。
	// 本版不自动读取仓库中的 .env，密钥不会进入对话历史。
	key := os.Getenv("MIAO_API_KEY")
	baseURL := envOr("MIAO_BASE_URL", "https://api.deepseek.com")
	model := envOr("MIAO_MODEL", "deepseek-chat")
	client, err := llm.New(key, baseURL, model)
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动失败：", err)
		os.Exit(1) // 非零退出码让终端知道程序失败了。
	}

	// os.Stdin / Stdout 分别是终端输入和输出。把它们作为参数传入，测试就能替换它们。
	if err := run(context.Background(), client, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "运行失败：", err)
		os.Exit(1)
	}
}

// envOr 返回非空环境变量；没有设置时使用 fallback 默认值。
func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// run 是最简单的交互循环：读取一行 → 判断命令 → 请求回答 → 再读取。
// io.Reader / io.Writer 是标准接口；它们也能表示字符串、文件或测试缓冲区。
// 没有额外 goroutine：所有操作依次发生，便于跟踪执行顺序。
func run(ctx context.Context, client llm.Client, input io.Reader, output io.Writer) error {
	a := agent.New(client)
	reader := bufio.NewReader(input)
	if _, err := fmt.Fprintln(output, "miao · 学习版\n输入文字开始对话；/help 查看帮助；Ctrl+C 退出。"); err != nil {
		return err
	}
	for {
		if _, err := fmt.Fprint(output, "\n你 > "); err != nil {
			return err
		}
		// ReadString 会读取到换行符；管道最后一行没有换行时，仍可能返回文字和 EOF。
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("读取输入: %w", readErr)
		}
		text := strings.TrimSpace(line)
		if text == "" && errors.Is(readErr, io.EOF) {
			return nil
		}
		switch text {
		case "":
			continue
		case "/exit":
			return nil
		case "/clear":
			a.Reset()
			if _, err := fmt.Fprintln(output, "对话已清空。"); err != nil {
				return err
			}
			continue
		case "/help":
			if _, err := fmt.Fprintln(output, "/clear 清空对话；/exit 退出。当前只有文字对话，不会读写文件或执行命令。"); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(text, "/") {
			if _, err := fmt.Fprintln(output, "未知命令，输入 /help 查看帮助。"); err != nil {
				return err
			}
			continue
		}

		if _, err := fmt.Fprint(output, "miao > "); err != nil {
			return err
		}
		// 这个匿名函数就是回调：客户端收到一段文字，就立即把它写到终端。
		// 闭包可以使用外层函数的 output 变量。
		err := a.Send(ctx, text, func(part string) error {
			_, err := fmt.Fprint(output, part)
			return err
		})
		if _, writeErr := fmt.Fprintln(output); writeErr != nil {
			return writeErr
		}
		if err != nil {
			// 网络错误后允许重新输入；失败的这一轮没有加入对话历史。
			if _, writeErr := fmt.Fprintln(output, "回答失败（本轮未加入历史）：", err); writeErr != nil {
				return writeErr
			}
		}
	}
}
