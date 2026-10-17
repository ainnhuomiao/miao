package agent

import "fmt"

// Undo 撤销最近一次 write_file/edit_file 修改，返回恢复的文件路径。
func (a *Agent) Undo() (string, error) {
	if a.tools == nil {
		return "", fmt.Errorf("没有可以撤销的修改")
	}
	return a.tools.Undo()
}

// SetSnapshotDir 设置撤销快照目录并清空撤销栈（切换会话时调用）。
func (a *Agent) SetSnapshotDir(dir string) {
	if a.tools != nil {
		a.tools.SetSnapshotDir(dir)
	}
}
