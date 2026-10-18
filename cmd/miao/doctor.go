package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/llm"
)

func runDoctor(cfg config.Config, notes []string, loadErr error) error {
	ok := 0
	fail := 0
	check := func(name, detail string, good bool) {
		mark := "ok"
		if !good {
			mark = "!!"
			fail++
		} else {
			ok++
		}
		fmt.Printf("%s  %s\n    %s\n", mark, name, detail)
	}

	if loadErr != nil && loadErr != config.ErrMissingAPIKey {
		check("配置", loadErr.Error(), false)
	} else {
		check("配置", "工作区 "+cfg.Workspace, true)
	}
	check("API Key", maskKey(cfg.APIKey), cfg.APIKey != "")
	check("Base URL", cfg.BaseURL, cfg.BaseURL != "")
	check("模型", cfg.Model, cfg.Model != "")
	if cfg.FallbackModel != "" {
		check("备用模型", cfg.FallbackModel, true)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if cfg.APIKey != "" {
		client := llm.NewClient(cfg.Protocol, cfg.APIKey, cfg.BaseURL, cfg.Model)
		llm.ApplySettings(client, 12*time.Second, 0, false)
		if lister, okc := client.(llm.ModelLister); okc {
			names, err := lister.ListModels(ctx)
			if err != nil {
				check("API 连通", err.Error(), false)
			} else {
				check("API 连通", fmt.Sprintf("列出 %d 个模型", len(names)), true)
			}
		} else {
			check("API 连通", "当前协议不支持列出模型，跳过探测", true)
		}
	} else {
		check("API 连通", "没有 Key，跳过", false)
	}

	_, bwrapErr := exec.LookPath("bwrap")
	sandboxOn := strings.TrimSpace(strings.ToLower(config.Getenv("SANDBOX")))
	on := sandboxOn == "1" || sandboxOn == "true" || sandboxOn == "on" || sandboxOn == "yes"
	switch {
	case bwrapErr != nil && on:
		check("bubblewrap", "已开 MIAO_SANDBOX 但找不到 bwrap", false)
	case bwrapErr != nil:
		check("bubblewrap", "未安装（可选，MIAO_SANDBOX=1 时使用）", true)
	default:
		check("bubblewrap", "已安装", true)
	}

	clip := firstLookPath(clipboardBins...)
	check("剪贴板", nonempty(clip, "未找到 "+strings.Join(clipboardBins, " / ")+"（粘贴图片需要）"), true)

	check("终端 stdin", ttyLabel(os.Stdin), isTerminal(os.Stdin))
	check("终端 stdout", ttyLabel(os.Stdout), isTerminal(os.Stdout))

	for _, bin := range []string{"go", "git", "rg"} {
		p, err := exec.LookPath(bin)
		check(bin, nonempty(p, "未找到"), err == nil)
	}

	for _, n := range notes {
		fmt.Println("note", n)
	}
	fmt.Printf("\n%d 项通过，%d 项需要处理。\n", ok, fail)
	if fail > 0 {
		return fmt.Errorf("doctor 发现 %d 个问题", fail)
	}
	return nil
}

func maskKey(k string) string {
	if k == "" {
		return "未设置"
	}
	if len(k) <= 6 {
		return "***"
	}
	return k[:3] + "***" + k[len(k)-2:]
}

// clipboardBins 与 TUI 读剪贴板图片用的工具一致（见 internal/tui/images.go）。
var clipboardBins = []string{"wl-paste", "xclip", "pngpaste"}

func firstLookPath(names ...string) string {
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

func nonempty(v, empty string) string {
	if v == "" {
		return empty
	}
	return v
}

func ttyLabel(f *os.File) string {
	if isTerminal(f) {
		return "是 TTY"
	}
	return "不是 TTY（管道或重定向）"
}
