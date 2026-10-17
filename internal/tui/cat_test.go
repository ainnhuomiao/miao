package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

func TestCatHalfwidthDot(t *testing.T) {
	if runewidth.RuneWidth('･') != 1 {
		t.Fatalf("･ 应算 1 列，实际 %d", runewidth.RuneWidth('･'))
	}
	// 布局用 lipgloss（ω 算 1 列，跟 kitty 一致）；runewidth 在 zh_CN 下会把 ω 当 2 列，别用它算猫。
	if ansi.StringWidth(catFace) != 7 || ansi.StringWidth(catBlink) != 7 {
		t.Fatal("睁眼和眨眼宽度必须一样，否则顶栏会跳")
	}
}

// MIAO_DUMP=目录 时导出带颜色的画面，用来截图给设计看。
func dumpColors(t *testing.T) {
	if os.Getenv("MIAO_DUMP") != "" {
		lipgloss.SetColorProfile(termenv.TrueColor)
		setTheme("mocha")
		t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii); setTheme("auto") })
	}
}

func TestWelcomeCat(t *testing.T) {
	dumpColors(t)
	m := newTestModel(t, 100, 30, "")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "( ･ω･ )") || !strings.Contains(v, welcomeHint) {
		t.Fatalf("空会话应显示小猫：\n%s", v)
	}
	if strings.Contains(v, "已就绪") {
		t.Fatal("小猫替代了「已就绪」提示")
	}
	if os.Getenv("MIAO_DUMP") != "" {
		_ = os.WriteFile(os.Getenv("MIAO_DUMP")+"/empty.ansi", []byte(m.View()), 0o644)
	}

	short := newTestModel(t, 100, 16, "")
	sv := ansi.Strip(short.View())
	if strings.Contains(sv, "( ･ω･ )") || !strings.Contains(sv, welcomeHint) {
		t.Fatalf("矮于 20 行只留提示：\n%s", sv)
	}

	m.messages = append(m.messages, chatMessage{role: roleUser, content: "hi", done: true})
	if strings.Contains(ansi.Strip(m.View()), welcomeHint) {
		t.Fatal("发出消息后小猫应消失")
	}
}

func TestHeaderCatDegrade(t *testing.T) {
	for _, tc := range []struct {
		w          int
		face, name bool
	}{{100, true, true}, {57, true, false}, {50, false, false}} {
		t.Setenv("MIAO_CONTEXT_WINDOW", "128000")
		m := newTestModel(t, tc.w, 24, "")
		h := ansi.Strip(strings.SplitN(m.headerView(), "\n", 2)[0])
		if strings.Contains(h, catFace) != tc.face || strings.Contains(h, "miao") != tc.name || !strings.Contains(h, "128k") {
			t.Errorf("宽 %d：%q", tc.w, h)
		}
		if ansi.StringWidth(h) > tc.w {
			t.Errorf("宽 %d 时顶栏超宽：%q", tc.w, h)
		}
	}
}

func TestCatBlink(t *testing.T) {
	dumpColors(t)
	t.Setenv("MIAO_CONTEXT_WINDOW", "128000")
	m := newTestModel(t, 100, 24, "")
	m.ctxTokens = 41000
	m.streaming = true
	m.streamStart = time.Now().Add(-blinkEvery + blinkLength/2)
	if !strings.Contains(m.catLogo(2), catBlink) {
		t.Fatal("周期末尾应眨眼")
	}
	m.streamStart = time.Now().Add(-time.Second)
	if !strings.Contains(m.catLogo(2), catFace) {
		t.Fatal("其余时间睁眼")
	}
	if os.Getenv("MIAO_DUMP") != "" {
		m.messages = append(m.messages, chatMessage{role: roleUser, content: "hi", done: true})
		_ = os.WriteFile(os.Getenv("MIAO_DUMP")+"/header.ansi", []byte(m.headerView()), 0o644)
		m.streamStart = time.Now().Add(-blinkEvery + blinkLength/2)
		_ = os.WriteFile(os.Getenv("MIAO_DUMP")+"/header_blink.ansi", []byte(m.headerView()), 0o644)
	}
}
