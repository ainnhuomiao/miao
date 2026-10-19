package config

// FeatureSetting 是供设置菜单显示的一项已保存启动设置。
type FeatureSetting struct {
	Name, Key, Title string
	Enabled          bool
}

// FeatureSettings 只读取保存值及默认值；实际运行时仍有环境变量和命令行覆盖。
func FeatureSettings(user UserFile) []FeatureSetting {
	return []FeatureSetting{
		{"mcp", "mcp_enabled", "MCP 扩展", defaultTrue(user.MCPEnabled)},
		{"hooks", "hooks_enabled", "Hooks", defaultTrue(user.HooksEnabled)},
		{"sessions", "sessions_enabled", "会话保存", defaultTrue(user.SessionsEnabled)},
		{"memory", "project_memory_enabled", "项目记忆", defaultTrue(user.ProjectMemoryEnabled)},
		{"commands", "custom_commands_enabled", "自定义命令", defaultTrue(user.CustomCommandsEnabled)},
		{"shell", "shell_enabled", "! shell 命令", defaultTrue(user.ShellEnabled)},
		{"images", "images_enabled", "图片输入", defaultTrue(user.ImagesEnabled)},
		{"diagnostics", "diagnostics_enabled", "编辑后诊断", defaultTrue(user.DiagnosticsEnabled)},
		{"snapshots", "snapshots_enabled", "文件撤销快照", defaultTrue(user.SnapshotsEnabled)},
		{"bell", "bell_enabled", "提示音", defaultTrue(user.BellEnabled)},
		{"plain", "plain", "简洁 REPL", user.Plain != nil && *user.Plain},
	}
}
