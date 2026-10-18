package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/setup"
)

const (
	gray  = "\x1b[90m"
	green = "\x1b[32m"
	red   = "\x1b[31m"
	reset = "\x1b[0m"
)

// initIO 是 miao init 的输入输出，测试里换成假的。
type initIO struct {
	in         *bufio.Reader
	out        io.Writer
	readSecret func() (string, error) // 读 key：不回显明文，只显示 •
	tester     setup.Tester
	saver      func(config.Credentials) (string, error)
}

// runInit 是 `miao init`：和 TUI 向导一样的流程，用逐行提示完成，保存到 ~/.miao/credentials。
func runInit() error {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return errors.New("miao init 需要在交互终端里运行；脚本里请设置环境变量 MIAO_API_KEY")
	}
	in := bufio.NewReader(os.Stdin)
	return initFlow(initIO{
		in:         in,
		out:        os.Stdout,
		readSecret: func() (string, error) { return readMasked(os.Stdin, os.Stdout) },
		tester:     setup.DefaultTester,
		saver:      setup.Save,
	})
}

func initFlow(s initIO) error {
	out := s.out
	fmt.Fprintf(out, "=^･ω･^=  %s第一次见面，先配一下 key%s\n\n", gray, reset)
	fmt.Fprintln(out, "选一个服务商：")
	for i, p := range setup.Providers {
		fmt.Fprintf(out, "  %d) %s\n", i+1, p.Name)
	}
	var p setup.Provider
	for {
		line, err := prompt(s, fmt.Sprintf("输入序号 [1-%d，默认 1]：", len(setup.Providers)))
		if err != nil {
			return err
		}
		n := 1
		if line != "" {
			n, err = strconv.Atoi(line)
		}
		if err == nil && n >= 1 && n <= len(setup.Providers) {
			p = setup.Providers[n-1]
			break
		}
		fmt.Fprintf(out, "%s没有这个选项%s\n", red, reset)
	}

	baseURL := ""
	if p.NeedsBaseURL() {
		for {
			line, err := prompt(s, "接口地址（如 https://example.com/v1）：")
			if err != nil {
				return err
			}
			if err := setup.ValidateBaseURL(line); err != nil {
				fmt.Fprintf(out, "%s%s%s\n", red, err.Error(), reset)
				continue
			}
			baseURL = line
			break
		}
	}

	for {
		fmt.Fprintf(out, "%s 的 API Key：", p.Name)
		key, err := s.readSecret()
		if err != nil {
			return err
		}
		key = strings.TrimSpace(key)
		if key == "" {
			fmt.Fprintf(out, "%skey 不能为空%s\n", red, reset)
			continue
		}
		fmt.Fprintf(out, "%s正在测试连接…%s\n", gray, reset)
		creds, err := s.tester(context.Background(), setup.Credentials(p, key, baseURL))
		if err == nil {
			note, serr := s.saver(creds)
			if serr != nil {
				return serr
			}
			fmt.Fprintf(out, "%s●%s 连上了\n%s%s%s\n", green, reset, gray, note, reset)
			return nil
		}
		fmt.Fprintf(out, "%s●%s %s\n", red, reset, setup.Classify(err).Error())
		again, err := prompt(s, "重新输入 key？[Y/n]：")
		if err != nil {
			return err
		}
		if strings.EqualFold(again, "n") || strings.EqualFold(again, "no") {
			return errors.New("没有保存。之后可以再运行 `miao init`")
		}
	}
}

func prompt(s initIO, label string) (string, error) {
	fmt.Fprint(s.out, label)
	line, err := s.in.ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("输入已结束，没有保存")
	}
	return strings.TrimSpace(line), nil
}

// readMasked 把终端切到 raw 模式逐字读取，每个字符回显一个 •；支持退格、Ctrl+U，Ctrl+C 放弃。
func readMasked(in, out *os.File) (string, error) {
	fd := in.Fd()
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer func() { _ = term.Restore(fd, state) }()
	var buf []rune
	r := bufio.NewReader(in)
	for {
		c, _, err := r.ReadRune()
		if err != nil {
			fmt.Fprint(out, "\r\n")
			return "", err
		}
		switch c {
		case '\r', '\n':
			fmt.Fprint(out, "\r\n")
			return string(buf), nil
		case 3: // Ctrl+C
			fmt.Fprint(out, "\r\n")
			return "", errors.New("已取消，没有保存")
		case 4: // Ctrl+D
			if len(buf) == 0 {
				fmt.Fprint(out, "\r\n")
				return "", errors.New("输入已结束，没有保存")
			}
		case 127, 8:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Fprint(out, "\b \b")
			}
		case 21: // Ctrl+U
			fmt.Fprint(out, strings.Repeat("\b \b", len(buf)))
			buf = buf[:0]
		case 0x1b: // 粘贴时的括号序列等转义，忽略
		default:
			if c >= 0x20 {
				buf = append(buf, c)
				fmt.Fprint(out, "•")
			}
		}
	}
}
