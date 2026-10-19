package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/hooks"
	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/mcp"
	"github.com/ainnhuomiao/miao/internal/subagent"
)

// runConfigAction 在读取凭据、创建客户端或启动扩展之前处理配置管理命令。
// 空参数或 show 交给主流程输出生效配置。
func runConfigAction(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 || len(args) == 1 && args[0] == "show" {
		return false, nil
	}
	switch args[0] {
	case "path":
		if len(args) != 1 {
			break
		}
		path, err := config.UserConfigPath()
		if err != nil {
			return true, err
		}
		_, err = fmt.Fprintln(out, path)
		return true, err
	case "keys":
		if len(args) != 1 {
			break
		}
		for _, field := range config.UserSettings() {
			if _, err := fmt.Fprintf(out, "%s\t%s\n", field.Key, field.Kind); err != nil {
				return true, err
			}
		}
		return true, nil
	case "set":
		if len(args) != 3 {
			break
		}
		value, err := configArgument(args[1], args[2])
		if err == nil && args[1] == "thinking" {
			var level string
			if err = json.Unmarshal(value, &level); err == nil {
				err = validateThinkingSetting(level)
			}
		}
		if err == nil {
			err = config.SetUserValue(args[1], value)
		}
		if err != nil {
			return true, err
		}
		_, err = fmt.Fprintf(out, "已保存 %s；下次启动生效，环境变量和命令行仍优先。\n", args[1])
		return true, err
	case "unset":
		if len(args) != 2 {
			break
		}
		if err := config.UnsetUserValue(args[1]); err != nil {
			return true, err
		}
		_, err := fmt.Fprintf(out, "已移除 %s 的用户配置；下次启动使用其它来源或默认值。\n", args[1])
		return true, err
	case "check":
		if len(args) != 1 {
			break
		}
		cfg, notes, err := config.LoadWithNotes()
		if err != nil && !errors.Is(err, config.ErrMissingAPIKey) {
			return true, err
		}
		if err := validateThinkingSetting(cfg.Thinking); err != nil {
			return true, err
		}
		if _, err := config.LoadProject(cfg.Workspace); err != nil {
			return true, err
		}
		if _, err := subagent.LoadConfig(); err != nil {
			return true, err
		}
		if _, err := mcp.LoadConfig(mcp.ConfigPath()); err != nil {
			return true, err
		}
		if _, err := hooks.Load(cfg.Workspace, nil); err != nil {
			return true, err
		}
		for _, note := range notes {
			if _, err := fmt.Fprintln(out, note); err != nil {
				return true, err
			}
		}
		_, err = fmt.Fprintln(out, "配置校验通过（未连接 API，未启动 MCP 或执行 hooks）。")
		return true, err
	}
	return true, errors.New("用法：miao config [show | path | keys | check | set <字段> <值> | unset <字段>]")
}

func validateThinkingSetting(level string) error {
	if _, err := llm.NormalizeThinking(level); err != nil {
		return errors.New("thinking 必须是 auto / none / minimal / low / medium / high / xhigh / max")
	}
	return nil
}

func configArgument(key, input string) (json.RawMessage, error) {
	for _, field := range config.UserSettings() {
		if field.Key != key {
			continue
		}
		if field.Kind == "string" {
			if strings.HasPrefix(input, `"`) {
				var value string
				if err := json.Unmarshal([]byte(input), &value); err != nil {
					return nil, errors.New("字符串值不是合法的 JSON 字符串")
				}
				return json.Marshal(value)
			}
			return json.Marshal(input)
		}
		return json.RawMessage(input), nil
	}
	return nil, errors.New("未知核心配置字段；运行 miao config keys 查看可修改字段")
}
