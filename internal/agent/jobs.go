package agent

// RunningJobs 返回正在运行的后台命令数。
func (a *Agent) RunningJobs() int {
	if a.tools == nil {
		return 0
	}
	return a.tools.RunningJobs()
}

// KillJobs 结束所有后台命令（退出时调用）。
func (a *Agent) KillJobs() {
	if a.tools != nil {
		a.tools.KillJobs()
	}
}
