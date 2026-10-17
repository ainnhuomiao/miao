package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func pacPos(t *testing.T, s string) int {
	t.Helper()
	plain := []rune(stripANSI(s))
	for i, r := range plain {
		if r == 'ᗧ' || r == '●' {
			return i
		}
	}
	t.Fatalf("没找到吃豆人：%q", s)
	return -1
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			esc = true
		case esc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestPacmanMeterPositions(t *testing.T) {
	cases := []struct {
		name        string
		used, total int
		wantPos     int
	}{
		{"0%", 0, 1000, 0},
		{"50%", 500, 1000, 8},
		{"100%", 1000, 1000, 15},
		{"超出窗口封顶", 5000, 1000, 15},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := pacmanMeter(c.used, c.total, 16, false)
			if w := lipgloss.Width(s); w != 16 {
				t.Fatalf("宽度=%d，想要 16：%q", w, s)
			}
			if p := pacPos(t, s); p != c.wantPos {
				t.Fatalf("吃豆人位置=%d，想要 %d", p, c.wantPos)
			}
		})
	}
}

func TestPacmanMeterMouthAndInvalid(t *testing.T) {
	if !strings.Contains(pacmanMeter(1, 10, 16, true), "●") {
		t.Fatal("闭嘴帧应显示 ●")
	}
	if pacmanMeter(1, 0, 16, false) != "" || pacmanMeter(1, 10, 1, false) != "" {
		t.Fatal("总量为 0 或格数不足时应返回空")
	}
}

func TestContextWindowLookup(t *testing.T) {
	t.Setenv("AGNET_CONTEXT_WINDOW", "")
	if contextWindow("deepseek-flash") != 128_000 {
		t.Fatal("deepseek 应按 128k")
	}
	if contextWindow("mystery-model") != 0 {
		t.Fatal("未知模型应返回 0")
	}
	t.Setenv("AGNET_CONTEXT_WINDOW", "32000")
	if contextWindow("mystery-model") != 32000 {
		t.Fatal("AGNET_CONTEXT_WINDOW 应覆盖")
	}
	t.Setenv("AGNET_CONTEXT_WINDOW", "abc")
	if contextWindow("deepseek-flash") != 128_000 {
		t.Fatal("非法覆盖值应被忽略")
	}
}

func TestContextViewUnknownAndOverflow(t *testing.T) {
	t.Setenv("AGNET_CONTEXT_WINDOW", "")
	m := newTestModel(t, 120, 30, "ok")
	m.modelName = "mystery-model"
	m.ctxTokens = 54_000
	v := stripANSI(m.contextView())
	if strings.ContainsAny(v, "ᗧ●") || !strings.Contains(v, "tok") {
		t.Fatalf("未知模型应只显示 token 数：%q", v)
	}
	m.modelName = "deepseek-flash"
	m.ctxTokens = 500_000
	if v := stripANSI(m.contextView()); !strings.Contains(v, "100%") {
		t.Fatalf("超出窗口百分比应封顶 100%%：%q", v)
	}
	if n := len(strings.Split(m.View(), "\n")); n != 30 {
		t.Fatalf("顶栏加吃豆人后渲染 %d 行，想要 30", n)
	}
}

func TestHeaderTrackFillsWidth(t *testing.T) {
	t.Setenv("AGNET_CONTEXT_WINDOW", "")
	for _, w := range []int{60, 100, 160} {
		m := newTestModel(t, w, 30, "ok")
		m.modelName = "deepseek-flash"
		m.ctxTokens = 0 // 0% 时豆子数 = 轨道长度 - 1
		h := strings.Split(m.headerView(), "\n")[0]
		if lw := lipgloss.Width(h); lw > w {
			t.Errorf("宽 %d：顶栏宽 %d 超出", w, lw)
		}
		beans := strings.Count(stripANSI(h), "•")
		if w >= 100 {
			if beans < 15 {
				t.Errorf("宽 %d：轨道 %d 格，少于 16 格保底", w, beans+1)
			}
			if beans+1 < w/2 {
				t.Errorf("宽 %d：轨道 %d 格，没有铺满顶栏", w, beans+1)
			}
		}
	}
}
