package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/subagent"
	"github.com/ainnhuomiao/miao/internal/tools"
)

func setupSubMgr(t *testing.T, cfg subagent.Config) (*subagent.Manager, *tools.Registry) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	reg, err := tools.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr := subagent.New(reg, &fakeClient{}, nil, cfg)
	subagent.SetCurrent(mgr)
	t.Cleanup(func() { subagent.SetCurrent(nil) })
	return mgr, reg
}

func taskMsg(desc, live string) chatMessage {
	m := toolMsg("task", `{"description":"`+desc+`","prompt":"p"}`, "", false, false)
	m.toolLive = live
	return m
}

func TestSubagentActivityRow(t *testing.T) {
	setupSubMgr(t, subagent.Config{Enabled: true, Max: 3})
	m := newTestModel(t, 100, 30, "ok")
	withTodos(m, todoArgs5)
	m.messages = append(m.messages, taskMsg("甲", "读取 a.go"), taskMsg("乙", "搜索 grep 调用"), taskMsg("丙", subagent.ActivityQueued))
	v := m.activityView()
	if !strings.Contains(v, "⑂ 2/3 子代理") || !strings.Contains(v, "搜索 grep 调用") || strings.Contains(v, "☐") {
		t.Fatalf("工具行应显示子代理状态：%q", v)
	}
	// 实时命令输出优先
	live := toolMsg("run_command", `{"command":"go"}`, "", false, false)
	live.toolLive = "building..."
	m.messages = append(m.messages, live)
	if v := m.activityView(); strings.Contains(v, "⑂") || !strings.Contains(v, "building") {
		t.Fatalf("实时输出应优先：%q", v)
	}
	m.messages = m.messages[:len(m.messages)-1]

	m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	p := m.todoPanelView()
	for _, want := range []string{"▶ 写测试", "子代理（2 个运行中）", "甲 · 读取 a.go", "乙 · 搜索 grep 调用"} {
		if !strings.Contains(p, want) {
			t.Fatalf("面板缺 %q：%q", want, p)
		}
	}
}

func TestSubagentPanelWithoutTodos(t *testing.T) {
	setupSubMgr(t, subagent.Config{Enabled: true})
	m := newTestModel(t, 100, 30, "ok")
	m.messages = append(m.messages, taskMsg("甲", "思考中"))
	m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if p := m.todoPanelView(); !strings.Contains(p, "甲 · 思考中") || strings.Contains(p, "☐") {
		t.Fatalf("无清单时应只显示子代理分节：%q", p)
	}
}

func TestSubagentUsageCounted(t *testing.T) {
	setupSubMgr(t, subagent.Defaults())
	m := newTestModel(t, 100, 30, "ok")
	m.ctxTokens = 100
	ch := make(chan streamEventMsg)
	m.events = ch
	m.handleStreamEvent(streamEventMsg{kind: agent.EventSubagentUsage, usage: &llm.Usage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10}})
	if m.usage != nil {
		t.Fatalf("子代理用量不应混进主循环：%+v", m.usage)
	}
	if m.subUsage == nil || m.subUsage.TotalTokens != 10 || m.sub.tokens != 10 || m.ctxTokens != 100 {
		t.Fatalf("子代理用量应单独累计且不改 ctxTokens：sub=%+v ctx=%d", m.subUsage, m.ctxTokens)
	}
}

func TestSubagentsMenuSavesConfig(t *testing.T) {
	mgr, reg := setupSubMgr(t, subagent.Defaults())
	m := newTestModel(t, 100, 30, "ok")
	m.runCommand("/subagents")
	if !m.paletteOpen() || !strings.Contains(m.paletteView(), "并发上限") {
		t.Fatalf("应打开设置菜单：%q", m.paletteView())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // 开关
	if !mgr.Config().Enabled || !reg.Has("task") {
		t.Fatal("Enter 应开启并注册 task")
	}
	if c, _ := subagent.LoadConfig(); !c.Enabled {
		t.Fatal("应立即保存")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: '+', Text: "+"})
	if mgr.Config().Max != 5 || m.input.Value() != "/subagents " {
		t.Fatalf("→/+ 应加大上限且不改输入：%d %q", mgr.Config().Max, m.input.Value())
	}
	for i := 0; i < 10; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	}
	if c, _ := subagent.LoadConfig(); c.Max != 1 {
		t.Fatalf("应夹到 1 并保存：%+v", c)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // 允许联网
	if !strings.Contains(m.paletteView(), "允许联网") || mgr.SubTools().Has("web_fetch") {
		t.Fatal("默认应关闭联网")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if c, _ := subagent.LoadConfig(); !c.AllowWeb || !mgr.SubTools().Has("web_fetch") {
		t.Fatalf("Enter 应开启联网并保存：%+v", c)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mgr.Config().AllowWeb || mgr.SubTools().Has("web_fetch") {
		t.Fatal("再次 Enter 应关闭联网")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // 第一个具体模型
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mgr.Config().Model != "test-model" {
		t.Fatalf("应选择模型：%+v", mgr.Config())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mgr.Config().Model != "" {
		t.Fatal("跟随主模型应清空 model")
	}
	m.palette.sel = 0
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mgr.Config().Enabled {
		t.Fatal("再次 Enter 应关闭")
	}
	if !reg.Has("task") {
		t.Fatal("关闭后仍应保留 task 定义")
	}
	if !strings.Contains(helpText, "/subagents") {
		t.Fatal("/help 应列出 /subagents")
	}
}
