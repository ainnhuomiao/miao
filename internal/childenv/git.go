package childenv

// gitHardening 关掉会让 git 执行仓库/全局配置里任意程序的入口：fsmonitor、hooks、外部 diff。
var gitHardening = []string{"-c", "core.fsmonitor=", "-c", "core.hooksPath=/dev/null", "-c", "diff.external="}

// GitArgs 在 git 参数前加上加固配置；diff/log/show 再关掉外部 diff 和 textconv 过滤器。
func GitArgs(args []string) []string {
	out := make([]string, 0, len(gitHardening)+len(args)+2)
	out = append(out, gitHardening...)
	if len(args) > 0 {
		switch args[0] {
		case "diff", "log", "show":
			out = append(out, args[0], "--no-ext-diff", "--no-textconv")
			return append(out, args[1:]...)
		}
	}
	return append(out, args...)
}

// GitEnv 是运行 git 子进程用的环境：去掉密钥变量，并忽略系统级 gitconfig。
func GitEnv() []string { return append(Environ(), "GIT_CONFIG_NOSYSTEM=1") }
