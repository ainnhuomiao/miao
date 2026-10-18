package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ainnhuomiao/miao/internal/llm"
)

// imageState 是输入框上方待发送的图片（Ctrl+V 粘贴的）。
type imageState struct {
	pending []llm.Image
}

// clipImageMsg 是异步读剪贴板图片的结果；data 为空表示剪贴板里没有图片。
type clipImageMsg struct {
	data    []byte
	noTools bool
}

const clipNoToolsHint = "剪贴板读取图片需要 wl-paste 或 xclip"

// clipRunner 执行剪贴板命令并返回 stdout；测试替换为假实现。
type clipRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

var (
	clipRun clipRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		var out bytes.Buffer
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdout = &out
		err := cmd.Run()
		return out.Bytes(), err
	}
	clipLookPath = exec.LookPath
	clipGOOS     = runtime.GOOS
)

var errNoClipTools = errors.New(clipNoToolsHint)

// readClipboardImage 依次尝试 wl-paste（Wayland）、xclip（X11）、macOS 的 pngpaste。
// 剪贴板里不是图片时返回 nil, nil；一个工具都没有时返回 errNoClipTools。
func readClipboardImage(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cands := [][]string{
		{"wl-paste", "--no-newline", "--type", "image/png"},
		{"xclip", "-selection", "clipboard", "-t", "image/png", "-o"},
	}
	if clipGOOS == "darwin" {
		cands = [][]string{{"pngpaste", "-"}}
	}
	found := false
	for _, c := range cands {
		if _, err := clipLookPath(c[0]); err != nil {
			continue
		}
		found = true
		out, err := clipRun(ctx, c[0], c[1:]...)
		if err != nil || len(out) == 0 {
			continue
		}
		if _, ok := llm.DetectImage(out); ok {
			return out, nil
		}
	}
	if !found {
		return nil, errNoClipTools
	}
	return nil, nil
}

func clipImageCmd() tea.Cmd {
	return func() tea.Msg {
		data, err := readClipboardImage(context.Background())
		return clipImageMsg{data: data, noTools: errors.Is(err, errNoClipTools)}
	}
}

// handleImageKey：Ctrl+V 先查剪贴板图片；输入为空时 Backspace 移除最后一张待发图片。
func (m *Model) handleImageKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "ctrl+v":
		return true, clipImageCmd()
	case "backspace":
		if m.input.Value() == "" && len(m.img.pending) > 0 {
			m.img.pending = m.img.pending[:len(m.img.pending)-1]
			return true, nil
		}
	}
	return false, nil
}

// onClipImage：有图片就加入待发列表；否则把 Ctrl+V 交给输入框按文本粘贴。
func (m *Model) onClipImage(msg clipImageMsg) tea.Cmd {
	if len(msg.data) == 0 {
		if msg.noTools {
			m.notice(clipNoToolsHint)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
		m.fitInput()
		m.syncPalette()
		return cmd
	}
	m.addPendingImage("剪贴板.png", msg.data)
	return nil
}

func (m *Model) addPendingImage(name string, data []byte) bool {
	if len(m.img.pending) >= llm.MaxImages {
		m.notice(fmt.Sprintf("一条消息最多 %d 张图片，已忽略", llm.MaxImages))
		return false
	}
	img, err := llm.NewImage(name, data)
	if err != nil {
		m.notice(err.Error())
		return false
	}
	m.img.pending = append(m.img.pending, img)
	return true
}

// imageChipsView 渲染待发图片标签 [图片 1] [图片 2]，占工具状态行，不改变布局高度。
func (m *Model) imageChipsView(w int) (string, bool) {
	if len(m.img.pending) == 0 || m.streaming {
		return "", false
	}
	chips := make([]string, len(m.img.pending))
	for i := range m.img.pending {
		chips[i] = fmt.Sprintf("[图片 %d]", i+1)
	}
	line := " " + strings.Join(chips, " ") + "  Backspace 移除"
	return mutedStyle.Render(truncateWidth(line, w)), true
}

// collectImages 合并待发图片和 @ 引用的图片，按数量上限与模型能力过滤，交给 Agent 随本条消息发送。
// 每张图片在用户消息下显示一行灰字 [图片 n: 名称 大小]。
func (m *Model) collectImages(prompt string) {
	imgs := m.img.pending
	m.img.pending = nil
	mention, warns := m.ag.MentionedImages(prompt)
	for _, w := range warns {
		m.notice(w)
	}
	imgs = append(imgs, mention...)
	if len(imgs) == 0 {
		m.ag.AttachImages(nil)
		return
	}
	if len(imgs) > llm.MaxImages {
		m.notice(fmt.Sprintf("一条消息最多 %d 张图片，多出的 %d 张已忽略", llm.MaxImages, len(imgs)-llm.MaxImages))
		imgs = imgs[:llm.MaxImages]
	}
	if !llm.SupportsVision(m.modelName) {
		m.notice(fmt.Sprintf("当前模型 %s 不支持图片，已忽略", m.modelName))
		m.ag.AttachImages(nil)
		return
	}
	for _, line := range imageLines(imgs) {
		m.notice(line)
	}
	m.ag.AttachImages(imgs)
}

// imageLines 是聊天记录里每张图片的灰字行。
func imageLines(imgs []llm.Image) []string {
	out := make([]string, len(imgs))
	for i, img := range imgs {
		out[i] = fmt.Sprintf("[图片 %d: %s %s]", i+1, img.Name, llm.FormatSize(len(img.Data)))
	}
	return out
}

func imageNotices(imgs []llm.Image) []chatMessage {
	var out []chatMessage
	for _, l := range imageLines(imgs) {
		out = append(out, chatMessage{role: roleNotice, content: l, done: true})
	}
	return out
}
