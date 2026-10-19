package tui

import "path/filepath"

// syncSnapshotDir 让撤销快照跟随当前会话：存到 <会话目录>/snapshots/<会话 id>，切换会话时清空撤销栈。
func (m *Model) syncSnapshotDir() {
	if m.ag == nil {
		return
	}
	dir := ""
	if m.store != nil && m.sess != nil {
		dir = filepath.Join(m.store.Dir(), "snapshots", m.sess.ID)
	}
	m.ag.SetSnapshotDir(dir)
	m.resetRewindPoints()
}

// undo 撤销最近一次文件修改，并插一条灰字提示。
func (m *Model) undo() {
	if m.options.DisableSnapshots {
		m.notice("文件撤销快照已关闭。可在配置中设置 snapshots_enabled 为 true 后重启启用。")
		return
	}
	if m.streaming {
		m.notice("正在生成，稍后再撤销。")
		return
	}
	rel, err := m.ag.Undo()
	if err != nil {
		m.notice(err.Error())
		return
	}
	m.notice("已撤销 " + rel)
}
