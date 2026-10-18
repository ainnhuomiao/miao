package tui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/llm"
)

func newOpenAIModel(t *testing.T, srvURL, model string) (*Model, *llm.OpenAIClient) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("MIAO_MODELS", "")
	t.Setenv("MIAO_CONTEXT_WINDOW", "")
	c := llm.NewOpenAI("k", srvURL, model)
	m := New(agent.New(c, "测试助手", nil), model)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m, c
}

// runCmd 执行一个 tea.Cmd（含 Batch）并把结果消息喂回模型。
func runCmd(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runCmd(m, c)
		}
	case modelsMsg:
		m.Update(msg)
	}
}

func modelsServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"data":[{"id":"deepseek-chat"},{"id":"deepseek-reasoner"},{"id":"gpt-4.1-mini"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestModelPaletteShowsCurrent(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	typeText(m, "/mod")
	if got := paletteNames(m); len(got) == 0 || got[0] != "/model" {
		t.Fatalf("/mod 匹配 %v", got)
	}
	if v := ansi.Strip(m.View().Content); !strings.Contains(v, "当前：test-model") {
		t.Fatalf("菜单应显示当前模型:\n%s", v)
	}
}

func TestModelSecondLevelFetchFilterAndSwitch(t *testing.T) {
	srv := modelsServer(t)
	m, c := newOpenAIModel(t, srv.URL, "deepseek-chat")
	typeText(m, "/model")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.input.Value() != "/model " {
		t.Fatalf("Enter 应补全为 /model ，得 %q", m.input.Value())
	}
	runCmd(m, cmd)
	got := paletteNames(m)
	if len(got) != 3 || got[0] != "deepseek-chat" {
		t.Fatalf("二级列表 %v", got)
	}
	typeText(m, "gpt")
	if got := paletteNames(m); len(got) != 1 || got[0] != "gpt-4.1-mini" {
		t.Fatalf("筛选 gpt 得 %v", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modelName != "gpt-4.1-mini" || c.Model() != "gpt-4.1-mini" {
		t.Fatalf("未切换：tui=%s client=%s", m.modelName, c.Model())
	}
	if m.sess.Model != "gpt-4.1-mini" {
		t.Fatalf("会话模型未更新: %s", m.sess.Model)
	}
	v := ansi.Strip(m.View().Content)
	if !strings.Contains(v, "已切换到 gpt-4.1-mini") || !strings.Contains(v, "gpt-4.1-mini") {
		t.Fatalf("应显示切换提示:\n%s", v)
	}
	if contextWindow(m.modelName) != 1_000_000 {
		t.Fatal("上下文窗口应为 1M")
	}
}

func TestModelDirectSwitchUpdatesWindow(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	t.Setenv("MIAO_CONTEXT_WINDOW", "")
	before := ansi.Strip(m.contextView())
	typeText(m, "/model claude-x")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modelName != "claude-x" {
		t.Fatalf("modelName=%s", m.modelName)
	}
	after := ansi.Strip(m.contextView())
	if before == after || !strings.Contains(after, "200k") {
		t.Fatalf("上下文窗口未更新: %q -> %q", before, after)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "claude-x") {
		t.Fatal("顶栏应显示新模型名")
	}
	t.Setenv("MIAO_CONTEXT_WINDOW", "5000")
	if !strings.Contains(ansi.Strip(m.contextView()), "5k") && !strings.Contains(ansi.Strip(m.contextView()), "5.0k") {
		t.Fatalf("环境变量应优先: %q", ansi.Strip(m.contextView()))
	}
}

func TestModelFetchFailureFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	m, _ := newOpenAIModel(t, srv.URL, "my-model")
	runCmd(m, m.Init())
	if !m.models.done || m.models.fetched != nil {
		t.Fatal("拉取失败应标记完成且无缓存")
	}
	typeText(m, "/model ")
	got := paletteNames(m)
	if len(got) != len(knownModels)+1 || got[0] != "my-model" {
		t.Fatalf("兜底列表 %v", got)
	}
}

func TestModelEnvOverride(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	t.Setenv("MIAO_MODELS", "a-1, b-2")
	if m.fetchModelsCmd() != nil {
		t.Fatal("设置 MIAO_MODELS 时不应拉取")
	}
	typeText(m, "/model ")
	if got := paletteNames(m); len(got) != 3 || got[1] != "a-1" || got[2] != "b-2" {
		t.Fatalf("MIAO_MODELS 列表 %v", got)
	}
}

func TestModelRefuseWhileBusy(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.streaming = true
	m.switchModel("other")
	typeText(m, "/model ")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modelName != "test-model" {
		t.Fatalf("生成中不应切换，得 %s", m.modelName)
	}
	if !strings.Contains(m.messages[len(m.messages)-1].content, "暂不能切换模型") {
		t.Fatalf("应提示拒绝: %q", m.messages[len(m.messages)-1].content)
	}
	m.streaming = false
}

func TestModelRestoredOnLoad(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	typeText(m, "hi")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	collectStream(t, m)
	m.switchModel("deepseek-reasoner")
	m.saveCurrentSession()
	id := m.sess.ID
	m.switchModel("test-model")
	m.loadSession(id)
	if m.modelName != "deepseek-reasoner" {
		t.Fatalf("/load 应恢复模型，得 %s", m.modelName)
	}
}
