package main

import (
	"flag"

	"github.com/ainnhuomiao/miao/internal/config"
)

type featureFlags struct {
	sessions, projectMemory, customCommands, shell, images, diagnostics, snapshots, bell *bool
}

func registerFeatureFlags() featureFlags {
	return featureFlags{
		sessions:       flag.Bool("sessions", true, "自动保存 TUI 会话（-sessions=false 关闭）"),
		projectMemory:  flag.Bool("project-memory", true, "加载项目 AGENTS.md（-project-memory=false 关闭）"),
		customCommands: flag.Bool("custom-commands", true, "加载自定义斜杠命令（-custom-commands=false 关闭）"),
		shell:          flag.Bool("shell", true, "允许 TUI 的 ! shell 命令（-shell=false 关闭）"),
		images:         flag.Bool("images", true, "允许图片输入（-images=false 关闭）"),
		diagnostics:    flag.Bool("diagnostics", true, "编辑后自动诊断（-diagnostics=false 关闭）"),
		snapshots:      flag.Bool("snapshots", true, "保存文件撤销快照（-snapshots=false 关闭）"),
		bell:           flag.Bool("bell", true, "审批和完成时响铃（-bell=false 关闭）"),
	}
}

func (f featureFlags) apply(cfg *config.Config) {
	for _, field := range []struct {
		name          string
		value, target *bool
	}{
		{"sessions", f.sessions, &cfg.SessionsEnabled},
		{"project-memory", f.projectMemory, &cfg.ProjectMemoryEnabled},
		{"custom-commands", f.customCommands, &cfg.CustomCommandsEnabled},
		{"shell", f.shell, &cfg.ShellEnabled},
		{"images", f.images, &cfg.ImagesEnabled},
		{"diagnostics", f.diagnostics, &cfg.DiagnosticsEnabled},
		{"snapshots", f.snapshots, &cfg.SnapshotsEnabled},
		{"bell", f.bell, &cfg.BellEnabled},
	} {
		if isFlagSet(field.name) {
			*field.target = *field.value
		}
	}
}
