package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/subagent"
	"github.com/ainnhuomiao/miao/internal/tools"
)

func setupSubMgr(t *testing.T, cfg subagent.Config) (*subagent.Manager, *tools.Registry) {
	t.Helper()
	t.Setenv("MIAO_HOME", t.TempDir())
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

func selectSubRow(t *testing.T, m *Model, name string) {
	t.Helper()
	for i, row := range m.paletteMatches() {
		if row.name == name {
			m.palette.sel = i
			m.paletteScroll()
			return
		}
	}
	t.Fatalf("找不到菜单项 %q：%v", name, paletteNames(m))
}

func TestSubagentsMenuSavesConfig(t *testing.T) {
	mgr, reg := setupSubMgr(t, subagent.Defaults())
	m := newTestModel(t, 100, 30, "ok")
	m.runCommand("/subagents")
	if !m.paletteOpen() || len(m.paletteMatches()) != 5 || !strings.Contains(m.paletteView(), "思考强度") {
		t.Fatalf("应打开紧凑的设置菜单：%q", m.paletteView())
	}
	if strings.Contains(m.paletteView(), "deepseek-v4-pro") {
		t.Fatal("模型候选不应出现在一级菜单")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !mgr.Config().Enabled || !reg.Has("task") {
		t.Fatal("Enter 应开启并注册 task")
	}
	selectSubRow(t, m, subRowMax)
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: '+', Text: "+"})
	if mgr.Config().Max != 5 || m.input.Value() != "/subagents " {
		t.Fatalf("并发调整不应改输入：%d %q", mgr.Config().Max, m.input.Value())
	}
	for i := 0; i < 10; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	}
	if c, _ := subagent.LoadConfig(); c.Max != 1 {
		t.Fatalf("应夹到 1 并保存：%+v", c)
	}
	selectSubRow(t, m, subRowWeb)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if c, _ := subagent.LoadConfig(); !c.AllowWeb || !mgr.SubTools().Has("web_fetch") {
		t.Fatal("应开启联网并保存")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mgr.Config().AllowWeb || mgr.SubTools().Has("web_fetch") {
		t.Fatal("再次 Enter 应关闭联网")
	}

	selectSubRow(t, m, subRowModel)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if page, _ := m.subagentsPage(); page != "model" {
		t.Fatal("应进入模型二级菜单")
	}
	selectSubRow(t, m, "test-model")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mgr.Config().Model != "test-model" || m.input.Value() != subagentsPrefix {
		t.Fatalf("应保存模型并返回一级菜单：%+v %q", mgr.Config(), m.input.Value())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	selectSubRow(t, m, subRowFollow)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mgr.Config().Model != "" {
		t.Fatal("跟随主模型应清空 model")
	}

	selectSubRow(t, m, subRowThinking)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if page, _ := m.subagentsPage(); page != "thinking" {
		t.Fatal("应进入思考强度二级菜单")
	}
	selectSubRow(t, m, "auto")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cfg, _ := subagent.LoadConfig(); cfg.Thinking != "auto" {
		t.Fatal("应独立保存 auto（服务端默认）")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	selectSubRow(t, m, subRowFollowThinking)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mgr.Config().Thinking != "" {
		t.Fatal("跟随主代理应清空 thinking")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if page, _ := m.subagentsPage(); page != "" || !m.paletteOpen() {
		t.Fatal("Esc 应返回一级而不是关闭菜单")
	}
	selectSubRow(t, m, subRowEnabled)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mgr.Config().Enabled || !reg.Has("task") {
		t.Fatal("关闭后仍保留 task 定义")
	}
}

func TestSubagentSubmenusQuerySelectedModelWithoutChangingMain(t *testing.T) {
	mgr, _ := setupSubMgr(t, subagent.Defaults())
	t.Setenv("MIAO_MODELS", "main-model,child-model-mini,child-model")
	var queried string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queried = r.URL.Path
		fmt.Fprint(w, `{"id":"child-model","capabilities":{"thinking":{"supported":true,"types":{"adaptive":{"supported":true},"disabled":{"supported":false}}},"effort":{"supported":true,"low":{"supported":true},"high":{"supported":false},"max":{"supported":false}}}}`)
	}))
	defer srv.Close()
	c := llm.NewSwappable(llm.NewAnthropic("test", srv.URL, "main-model"))
	if err := c.SetThinking("high"); err != nil {
		t.Fatal(err)
	}
	m := New(agent.New(c, "test", nil), "main-model")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.openSubagentsMenu()
	selectSubRow(t, m, subRowModel)
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	typeText(m, "child-model")
	if names := paletteNames(m); len(names) != 2 || names[0] != "child-model" {
		t.Fatalf("模型列表应支持筛选且完整名称优先：%v", names)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "/subagents model child-model" {
		t.Fatalf("模型应支持 Tab 补全：%q", m.input.Value())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mgr.Config().Model != "child-model" {
		t.Fatal("子代理模型未保存")
	}
	selectSubRow(t, m, subRowThinking)
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runCmd(m, cmd)
	if queried != "/v1/models/child-model" {
		t.Fatalf("应查询子代理所选模型：%q", queried)
	}
	if names := paletteNames(m); !slices.Contains(names, "low") || slices.Contains(names, "high") || slices.Contains(names, "max") {
		t.Fatalf("应使用子代理模型自己的能力：%v", names)
	}
	selectSubRow(t, m, "low")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if saved, err := subagent.LoadConfig(); err != nil || saved.Thinking != "low" || saved.Model != "child-model" {
		t.Fatalf("应保存独立模型和强度：%+v %v", saved, err)
	}
	if c.Model() != "main-model" || c.Thinking() != "high" {
		t.Fatal("子代理设置不应改动主代理配置")
	}

	for _, width := range []int{60, 80, 140} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		for _, page := range []string{"", "model", "thinking"} {
			m.openSubagentsPage(page, "")
			for _, line := range strings.Split(m.paletteView(), "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("%s 菜单超过终端宽度 %d：%q", page, width, line)
				}
			}
		}
	}
}
