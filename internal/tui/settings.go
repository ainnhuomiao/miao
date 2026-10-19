package tui

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ainnhuomiao/miao/internal/config"
)

type settingsState struct {
	loaded   time.Time
	features []config.FeatureSetting
	err      error
}

func (m *Model) settingsMode() (string, bool) {
	value := m.input.Value()
	if !strings.HasPrefix(value, "/settings ") || strings.ContainsAny(value, "\n") {
		return "", false
	}
	query := strings.TrimSpace(strings.TrimPrefix(value, "/settings "))
	if strings.ContainsAny(query, " \t") {
		return "", false
	}
	return query, true
}

func (m *Model) loadSettings() {
	if !m.settings.loaded.IsZero() && time.Since(m.settings.loaded) < 3*time.Second {
		return
	}
	user, err := config.LoadUserFile()
	m.settings = settingsState{loaded: time.Now(), err: err}
	if err == nil {
		m.settings.features = config.FeatureSettings(user)
	}
}

func (m *Model) settingsMatches(query string) []paletteCmd {
	m.loadSettings()
	query = strings.ToLower(query)
	var result []paletteCmd
	for _, feature := range m.settings.features {
		if query != "" && !strings.HasPrefix(feature.Name, query) && !strings.Contains(feature.Title, query) && !isSubsequence(query, feature.Name) {
			continue
		}
		state := "关"
		if feature.Enabled {
			state = "开"
		}
		result = append(result, paletteCmd{name: feature.Name, desc: feature.Title + " · 下次启动：" + state + "（Enter 切换）"})
	}
	return result
}

func (m *Model) runSettings(args []string) tea.Cmd {
	if len(args) == 0 {
		m.settings = settingsState{}
		m.input.SetValue("/settings ")
		m.input.CursorEnd()
		m.fitInput()
		m.syncPalette()
		return nil
	}
	if len(args) > 2 {
		m.notice("用法：/settings，或 /settings <功能> [on/off]")
		return nil
	}
	if m.streaming {
		m.notice("正在生成，稍后再修改启动设置。")
		return nil
	}
	m.settings = settingsState{} // 修改前重新读取，保留其它入口保存的值
	m.loadSettings()
	if m.settings.err != nil {
		m.notice("读取设置失败：" + m.settings.err.Error())
		return nil
	}
	for _, feature := range m.settings.features {
		if args[0] != feature.Name && args[0] != feature.Key {
			continue
		}
		enabled := !feature.Enabled
		if len(args) == 2 {
			switch strings.ToLower(args[1]) {
			case "on", "true", "1":
				enabled = true
			case "off", "false", "0":
				enabled = false
			default:
				m.notice("开关值使用 on / off / true / false / 1 / 0。")
				return nil
			}
		}
		if err := config.SetUserValue(feature.Key, json.RawMessage(strconv.FormatBool(enabled))); err != nil {
			m.notice("保存设置失败：" + err.Error())
			return nil
		}
		m.settings = settingsState{}
		state := "关闭"
		if enabled {
			state = "启用"
		}
		m.notice("已保存：" + state + feature.Title + "；重启后生效，环境变量和命令行仍优先。")
		m.syncPalette()
		return nil
	}
	m.notice("没有这个功能设置；输入 /settings 查看可选项。")
	return nil
}

func (m *Model) handleSettingsKey(msg tea.KeyPressMsg, matches []paletteCmd) (bool, tea.Cmd) {
	if _, ok := m.settingsMode(); !ok || msg.String() != "enter" || len(matches) == 0 {
		return false, nil
	}
	return true, m.runSettings([]string{matches[m.palette.sel].name})
}
