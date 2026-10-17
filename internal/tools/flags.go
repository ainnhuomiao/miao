package tools

// HasDangerousFlag 报告命令参数里是否带有能写文件或执行任意程序的参数
// （如 go 的 -exec、-toolexec、-o）。权限白名单放行前必须先过这一关。
func HasDangerousFlag(name string, args []string) bool {
	for _, a := range args {
		if dangerousFlag(name, a) {
			return true
		}
	}
	return false
}
