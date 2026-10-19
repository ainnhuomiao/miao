package tui

func (m *Model) sessionStorageNotice() string {
	if m.options.DisableSessionStorage {
		return "会话保存已关闭。可在配置中设置 sessions_enabled 为 true 后重启启用。"
	}
	return "会话存储不可用。"
}
