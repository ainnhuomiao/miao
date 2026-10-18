package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/setup"
)

const secretKey = "sk-very-secret-123456"

type fakeTester struct {
	calls int
	got   config.Credentials
	err   error
}

func (f *fakeTester) test(_ context.Context, c config.Credentials) (config.Credentials, error) {
	f.calls++
	f.got = c
	return c, f.err
}

type fakeSaver struct{ saved []config.Credentials }

func (f *fakeSaver) save(c config.Credentials) (string, error) {
	f.saved = append(f.saved, c)
	return "已保存到 ~/.miao/credentials", nil
}

func key(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

// runTest 执行进入测试时返回的命令里的第一个（连接测试本身），把结果喂回向导。
func runTest(t *testing.T, w *wizard, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("应返回测试命令")
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		msg = b[0]()
	}
	tm, ok := msg.(wizardTestMsg)
	if !ok {
		t.Fatalf("应得到测试结果，得到 %T", msg)
	}
	w.Update(tm)
}

func plain(w *wizard) string { return stripANSI(w.View(80, 24)) }

func TestWizardHappyPath(t *testing.T) {
	ft, fs := &fakeTester{}, &fakeSaver{}
	w := newWizard(true, ft.test, fs.save)
	v := plain(w)
	if !strings.Contains(v, "=^･ω･^=") || !strings.Contains(v, "第一次见面，先配一下 key") || !strings.Contains(v, "↑↓ 选择 · Enter 确认") {
		t.Fatalf("第一屏：%s", v)
	}
	w.Update(key("down"))
	w.Update(key("up"))
	w.Update(key("enter")) // DeepSeek，不问地址
	if w.step != wizKey {
		t.Fatalf("DeepSeek 应直接进入填 key：%v", w.step)
	}
	w.Update(tea.PasteMsg{Content: secretKey})
	v = plain(w)
	if strings.Contains(v, secretKey) || strings.Contains(v, "sk-") {
		t.Fatalf("key 不能明文显示：%s", v)
	}
	if !strings.Contains(v, strings.Repeat("•", 5)) {
		t.Fatalf("应显示 •：%s", v)
	}
	cmd := w.Update(key("enter"))
	if w.step != wizTesting {
		t.Fatalf("应进入测试：%v", w.step)
	}
	if !strings.Contains(plain(w), "正在测试连接") {
		t.Fatal("测试中提示")
	}
	// 测试中小猫眨眼
	w.Update(wizardBlinkMsg{gen: w.gen})
	if !strings.Contains(plain(w), catBlink) {
		t.Fatal("测试中小猫应眨眼")
	}
	runTest(t, w, cmd)
	if ft.calls != 1 || ft.got.APIKey != secretKey || ft.got.Provider != "deepseek" || ft.got.BaseURL != "https://api.deepseek.com" {
		t.Fatalf("测试参数：%+v", ft.got)
	}
	if !w.ok || len(fs.saved) != 1 || !strings.Contains(plain(w), "● 连上了") {
		t.Fatalf("成功后应保存并提示：%s", plain(w))
	}
	if strings.Contains(w.View(80, 24), secretKey) {
		t.Fatal("成功页不能出现 key")
	}
	w.Update(wizardDoneMsg{})
	if !w.done || w.saved.APIKey != secretKey {
		t.Fatal("应完成")
	}
}

func TestWizardFailureRetryAndBack(t *testing.T) {
	ft, fs := &fakeTester{err: &setup.TestError{Kind: setup.KindAuth, Err: &llm.APIError{StatusCode: 401, Message: "bad"}}}, &fakeSaver{}
	w := newWizard(true, ft.test, fs.save)
	w.Update(key("enter"))
	w.Update(tea.PasteMsg{Content: secretKey})
	runTest(t, w, w.Update(key("enter")))
	v := plain(w)
	if w.ok || !strings.Contains(v, "● key 错了") || !strings.Contains(v, "Enter 重试 · Esc 返回改 key") {
		t.Fatalf("失败页：%s", v)
	}
	if len(fs.saved) != 0 {
		t.Fatal("失败不能保存")
	}
	// 重试
	ft.err = errors.New("dial tcp: connection refused")
	runTest(t, w, w.Update(key("enter")))
	if ft.calls != 2 || w.ok {
		t.Fatal("应重试一次")
	}
	// 返回改 key
	w.Update(key("esc"))
	if w.step != wizKey {
		t.Fatalf("Esc 应回到填 key：%v", w.step)
	}
	w.Update(key("esc"))
	if w.step != wizProvider {
		t.Fatalf("再 Esc 回到选服务商：%v", w.step)
	}
	w.Update(key("esc"))
	if w.cancelled {
		t.Fatal("首次启动时 Esc 不应退出向导")
	}
}

func TestWizardStaleResultIgnored(t *testing.T) {
	ft := &fakeTester{}
	w := newWizard(true, ft.test, (&fakeSaver{}).save)
	w.Update(key("enter"))
	w.Update(tea.PasteMsg{Content: "k"})
	w.Update(key("enter"))
	w.Update(wizardTestMsg{gen: w.gen - 1})
	if w.step != wizTesting {
		t.Fatal("过期的测试结果应忽略")
	}
}

func TestWizardOpenAIAsksBaseURL(t *testing.T) {
	ft := &fakeTester{}
	w := newWizard(true, ft.test, (&fakeSaver{}).save)
	w.Update(key("down"))
	w.Update(key("down"))
	w.Update(key("enter"))
	if w.step != wizBaseURL {
		t.Fatalf("OpenAI 兼容应先问地址：%v", w.step)
	}
	w.Update(tea.PasteMsg{Content: "not-a-url"})
	w.Update(key("enter"))
	if w.step != wizBaseURL || !strings.Contains(plain(w), "http") {
		t.Fatal("地址不合法应留在本步并提示")
	}
	w.baseURL.SetValue("https://my.example/v1/")
	w.Update(key("enter"))
	w.Update(tea.PasteMsg{Content: secretKey})
	runTest(t, w, w.Update(key("enter")))
	if ft.got.BaseURL != "https://my.example/v1" || ft.got.Protocol != "openai" {
		t.Fatalf("%+v", ft.got)
	}
	w2 := newWizard(true, ft.test, (&fakeSaver{}).save)
	w2.Update(key("enter"))
	w2.Update(key("enter"))
	if w2.step != wizKey || !strings.Contains(plain(w2), "key 不能为空") {
		t.Fatal("空 key 不能提交")
	}
}

func TestLoginCommandInTUI(t *testing.T) {
	ft, fs := &fakeTester{}, &fakeSaver{}
	loginTester, loginSaver = ft.test, fs.save
	var swapped config.Credentials
	SetLoginHandler(func(c config.Credentials) (string, error) { swapped = c; return "claude-x", nil })
	t.Cleanup(func() { loginTester, loginSaver, loginHandler = nil, nil, nil })

	m := newTestModel(t, 100, 30, "ok")
	if !strings.Contains(strings.Join(paletteNamesFor(m, "/log"), ","), "/login") {
		t.Fatal("/login 应出现在命令菜单")
	}
	if !strings.Contains(helpText, "/login") {
		t.Fatal("/help 里应有 /login")
	}
	typeText(m, "/login")
	m.Update(key("enter"))
	if m.login == nil {
		t.Fatal("/login 应打开向导")
	}
	if !strings.Contains(stripANSI(m.View().Content), "换个 key 或服务商") {
		t.Fatal("应显示向导")
	}
	m.Update(key("down"))
	m.Update(key("enter")) // Anthropic
	m.Update(tea.PasteMsg{Content: secretKey})
	if strings.Contains(m.View().Content, secretKey) {
		t.Fatal("界面不能出现 key")
	}
	_, cmd := m.Update(key("enter"))
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		msg = b[0]()
	}
	m.Update(msg)
	m.Update(wizardDoneMsg{})
	if m.login != nil || swapped.Provider != "anthropic" || m.modelName != "claude-x" {
		t.Fatalf("应切换客户端：%+v model=%s", swapped, m.modelName)
	}
	all := stripANSI(m.View().Content)
	for _, msg := range m.messages {
		all += msg.content
	}
	if strings.Contains(all, secretKey) {
		t.Fatal("对话记录里不能出现 key")
	}
	if !strings.Contains(all, "已切换到 Anthropic") {
		t.Fatalf("应提示已切换：%s", all)
	}

	// Esc 取消
	typeText(m, "/login")
	m.Update(key("enter"))
	m.Update(key("esc"))
	if m.login != nil {
		t.Fatal("/login 里 Esc 应取消")
	}
}

func paletteNamesFor(m *Model, prefix string) []string {
	typeText(m, prefix)
	names := paletteNames(m)
	m.input.SetValue("")
	m.syncPalette()
	return names
}
