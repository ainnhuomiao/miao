package tui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/llm"
	"github.com/ainnhuomiao/miao/internal/tools"
)

var tuiPNG = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{3}, 2048)...)

// fakeClip 替换剪贴板命令：have 是“已安装”的工具，out 是各工具的输出。
func fakeClip(t *testing.T, have map[string][]byte) *[]string {
	t.Helper()
	var calls []string
	oldRun, oldLook, oldOS := clipRun, clipLookPath, clipGOOS
	clipGOOS = "linux"
	clipLookPath = func(name string) (string, error) {
		if _, ok := have[name]; ok {
			return "/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	clipRun = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if out := have[name]; out != nil {
			return out, nil
		}
		return nil, errors.New("no image")
	}
	t.Cleanup(func() { clipRun, clipLookPath, clipGOOS = oldRun, oldLook, oldOS })
	return &calls
}

func TestReadClipboardImageOrder(t *testing.T) {
	calls := fakeClip(t, map[string][]byte{"wl-paste": nil, "xclip": tuiPNG})
	data, err := readClipboardImage(context.Background())
	if err != nil || !bytes.Equal(data, tuiPNG) {
		t.Fatalf("应从 xclip 读到图片：%v", err)
	}
	if len(*calls) != 2 || !strings.HasPrefix((*calls)[0], "wl-paste") || !strings.Contains((*calls)[1], "-selection clipboard -t image/png -o") {
		t.Fatalf("应先 wl-paste 再 xclip：%v", *calls)
	}
	fakeClip(t, map[string][]byte{"wl-paste": []byte("just text")})
	if data, err := readClipboardImage(context.Background()); data != nil || err != nil {
		t.Fatal("文本不算图片")
	}
	fakeClip(t, map[string][]byte{})
	if _, err := readClipboardImage(context.Background()); !errors.Is(err, errNoClipTools) {
		t.Fatal("没有工具应报错")
	}
}

func TestCtrlVPastesImageElseText(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	fakeClip(t, map[string][]byte{"wl-paste": tuiPNG})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("Ctrl+V 应读剪贴板")
	}
	m.Update(cmd())
	if len(m.img.pending) != 1 || !strings.Contains(m.activityView(), "[图片 1]") {
		t.Fatalf("应出现图片标签：%q", m.activityView())
	}
	// 没有工具：灰字提示，输入框照常处理（文本粘贴）
	m.Update(clipImageMsg{noTools: true})
	if !strings.Contains(m.viewport.View(), clipNoToolsHint) || len(m.img.pending) != 1 {
		t.Fatal("应提示需要 wl-paste 或 xclip")
	}
	// 括号粘贴的文本不受影响
	m.Update(tea.PasteMsg{Content: "粘贴的文字"})
	if m.input.Value() != "粘贴的文字" {
		t.Fatalf("文本粘贴应照常：%q", m.input.Value())
	}
}

func TestBackspaceRemovesLastImage(t *testing.T) {
	m := newTestModel(t, 100, 30, "ok")
	m.addPendingImage("a.png", tuiPNG)
	m.addPendingImage("b.png", tuiPNG)
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(m.img.pending) != 1 {
		t.Fatal("空输入 Backspace 应移除最后一张")
	}
	m.input.SetValue("x")
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(m.img.pending) != 1 || m.input.Value() != "" {
		t.Fatal("有文字时 Backspace 正常删字")
	}
}

func TestImageLimitsAndChipsWidth(t *testing.T) {
	m := newTestModel(t, 40, 20, "ok")
	for i := 0; i < llm.MaxImages+1; i++ {
		m.addPendingImage("a.png", tuiPNG)
	}
	if len(m.img.pending) != llm.MaxImages || !strings.Contains(m.viewport.View(), "最多 5 张") {
		t.Fatal("超过 5 张应拒绝")
	}
	if m.addPendingImage("big.png", append(append([]byte{}, tuiPNG...), make([]byte, llm.MaxImageBytes)...)) {
		t.Fatal("超大图片应拒绝")
	}
	for _, line := range strings.Split(m.view(), "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("超宽：%q", line)
		}
	}
	if len(strings.Split(m.view(), "\n")) != 20 {
		t.Fatal("标签行不应改变布局高度")
	}
}

func TestSendWithImagesAndUnsupportedModel(t *testing.T) {
	t.Setenv("MIAO_VISION", "")
	m := newTestModel(t, 100, 30, "ok")
	m.addPendingImage("剪贴板.png", tuiPNG)
	m.input.SetValue("这是什么")
	m.submit()
	collectStream(t, m)
	h := m.ag.History()
	if len(h[0].Images) != 1 || !strings.Contains(m.viewport.View(), "[图片 1: 剪贴板.png 3KB]") || len(m.img.pending) != 0 {
		t.Fatalf("应随消息发送图片并显示：%+v", h[0].Images)
	}

	m2 := newTestModel(t, 100, 30, "ok")
	m2.modelName = "deepseek-chat"
	m2.addPendingImage("a.png", tuiPNG)
	m2.input.SetValue("看")
	m2.submit()
	collectStream(t, m2)
	if len(m2.ag.History()[0].Images) != 0 || !strings.Contains(m2.viewport.View(), "当前模型 deepseek-chat 不支持图片，已忽略") {
		t.Fatal("不支持图片的模型应丢弃图片")
	}
}

func TestAtImageAttach(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "图片.png"), tuiPNG, 0o644)
	reg, _ := tools.NewRegistry(dir)
	ag := agent.New(&fakeClient{deltas: []llm.Delta{{Content: "ok"}}}, "x", reg)
	m := New(ag, "gpt-4o")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.input.SetValue("看 @图片.png")
	m.submit()
	collectStream(t, m)
	var im []llm.Image
	for _, msg := range ag.History() {
		if len(msg.Images) > 0 {
			im = msg.Images
			break
		}
	}
	if len(im) != 1 || im[0].Name != "图片.png" {
		t.Fatalf("@图片 应作为图片附加：%+v history=%+v", im, ag.History())
	}
	if strings.Contains(m.viewport.View(), "不是文本文件") {
		t.Fatal("不应提示二进制")
	}
}
