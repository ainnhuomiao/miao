package tui

import "miao/internal/subagent"

// subWebRow 是 /subagents 菜单里的「允许联网」开关行。
func subWebRow(cfg subagent.Config) paletteCmd {
	desc := "关（子代理只用本地只读工具）· Enter 切换"
	if cfg.AllowWeb {
		desc = "开（子代理可免审批 web_fetch，注意数据外传风险）· Enter 切换"
	}
	return paletteCmd{name: subRowWeb, desc: desc}
}
