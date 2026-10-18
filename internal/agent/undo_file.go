package agent

import "fmt"

// UndoFile 撤销本会话对单个文件的全部修改（恢复到第一次修改前）。
func (a *Agent) UndoFile(rel string) error {
	if a.tools == nil {
		return fmt.Errorf("没有可以撤销的修改")
	}
	return a.tools.UndoFile(rel)
}

// HasFileSnapshot 报告这个文件有没有可撤销的快照。
func (a *Agent) HasFileSnapshot(rel string) bool {
	return a.tools != nil && a.tools.HasSnapshot(rel)
}
