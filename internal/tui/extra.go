package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ainnhuomiao/miao/internal/agent"
	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/llm"
)

func (m *Model) ringBell() {
	if m.options.DisableBell || strings.HasSuffix(os.Args[0], ".test") {
		return
	}
	_, _ = fmt.Fprint(os.Stderr, "\a")
}

func priceOverride(key string) float64 {
	v := strings.TrimSpace(config.Getenv(key))
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (m *Model) applyPlan() tea.Cmd {
	if m.ag.Mode() != agent.ModePlan {
		m.notice("当前不是只读规划模式。先按 Shift+Tab 切到「只读规划」，让模型出方案后再 /apply。")
		return nil
	}
	if len(m.ag.History()) == 0 {
		m.notice("还没有方案可以执行。")
		return nil
	}
	m.ag.SetMode(agent.ModeAcceptEdits)
	m.notice("已切到「自动同意编辑」，开始按刚才的方案执行。")
	return m.sendMessage("请按刚才的方案动手执行。", "请按刚才给出的方案动手修改代码：该写文件就写，该改就改。不要再只给方案。")
}

func (m *Model) showCost() {
	if m.usage == nil && m.subUsage == nil && m.costUSD == 0 {
		m.notice("本会话还没有用量数据。")
		return
	}
	var b strings.Builder
	b.WriteString("本会话花费（估算）\n")
	fmt.Fprintf(&b, "  模型：%s\n", m.modelName)
	if m.usage != nil {
		fmt.Fprintf(&b, "  主循环输入：%s token", formatTokens(m.usage.PromptTokens))
		if hit, ok := formatCacheHit(m.lastReq); ok {
			fmt.Fprintf(&b, "（最近一次缓存 %s）", hit)
		} else if m.usage.PromptCacheHitTokens > 0 {
			fmt.Fprintf(&b, "（缓存命中 %s）", formatTokens(m.usage.PromptCacheHitTokens))
		}
		b.WriteByte('\n')
		fmt.Fprintf(&b, "  主循环输出：%s token\n", formatTokens(m.usage.CompletionTokens))
	}
	if m.subUsage != nil && (m.subUsage.PromptTokens > 0 || m.subUsage.CompletionTokens > 0) {
		fmt.Fprintf(&b, "  子代理输入：%s token", formatTokens(m.subUsage.PromptTokens))
		if hit, ok := formatCacheHit(m.subUsage); ok {
			fmt.Fprintf(&b, "（缓存 %s）", hit)
		}
		b.WriteByte('\n')
		fmt.Fprintf(&b, "  子代理输出：%s token\n", formatTokens(m.subUsage.CompletionTokens))
	}
	if m.costUSD > 0 {
		fmt.Fprintf(&b, "  累计：%s\n", llm.FormatCost(m.costUSD))
	} else {
		b.WriteString("  累计：未知单价（可设 MIAO_PRICE_IN / MIAO_PRICE_OUT）\n")
	}
	m.notice(strings.TrimSpace(b.String()))
}

func (m *Model) initAgentsMD() {
	if m.options.DisableProjectMemory {
		m.notice("项目记忆已关闭。可在配置中设置 project_memory_enabled 为 true 后重启启用。")
		return
	}
	ws := m.ag.Workspace()
	content, err := agent.GenerateAgentsMD(ws)
	if err != nil {
		m.notice("生成 AGENTS.md 失败：" + err.Error())
		return
	}
	if err := agent.WriteAgentsMD(ws, content, false); err != nil {
		if os.IsExist(err) {
			m.notice("已有 AGENTS.md，未覆盖。删掉后再 /init。")
			return
		}
		m.notice("写入 AGENTS.md 失败：" + err.Error())
		return
	}
	m.ag.ApplyAgentsMD(content, false)
	m.notice("已写入 AGENTS.md，并已加入当前会话。")
}

func (m *Model) trustProject() {
	ws := m.ag.Workspace()
	pf, err := config.TrustProject(ws)
	if err != nil {
		if os.IsNotExist(err) {
			m.notice("当前工作区没有 .miao/config.json。")
			return
		}
		m.notice("信任项目配置失败：" + err.Error())
		return
	}
	if len(pf.AllowCommands) > 0 {
		m.ag.SetAllowedCommands(append(m.ag.AllowedCommands(), pf.AllowCommands...))
		_ = os.Setenv("MIAO_ALLOW_COMMANDS", strings.Join(m.ag.AllowedCommands(), ","))
	}
	if pf.Model != "" {
		m.switchModel(pf.Model)
	}
	m.notice("已信任本仓库的项目配置。allow_commands 立即生效。")
}

type bangDoneMsg struct {
	cmd string
	out string
	err error
}

func (m *Model) runBang(cmd string) tea.Cmd {
	if m.options.DisableShell {
		m.notice("! 命令已关闭。可在配置中设置 shell_enabled 为 true 后重启启用。")
		return nil
	}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		m.notice("用法：!<命令>  例如 !git status")
		return nil
	}
	if m.ag.Mode() == agent.ModePlan {
		m.notice("只读规划模式下不能执行 ! 命令。")
		return nil
	}
	if m.streaming {
		m.notice("正在生成，稍后再执行 ! 命令。")
		return nil
	}
	m.messages = append(m.messages, chatMessage{role: roleUser, content: "!" + cmd, done: true})
	m.refreshViewport(true)
	ws := m.ag.Workspace()
	ctx, cancel := context.WithCancel(context.Background())
	m.streaming, m.shelling = true, true
	m.streamStart = time.Now()
	m.cancel = cancel
	return tea.Batch(func() tea.Msg {
		tctx, timeout := context.WithTimeout(ctx, 2*time.Minute)
		defer timeout()
		c := exec.CommandContext(tctx, "sh", "-c", cmd)
		c.Dir = ws
		out, err := c.CombinedOutput()
		if tctx.Err() != nil {
			err = tctx.Err()
		}
		text := strings.TrimSpace(string(out))
		if len(text) > 16<<10 {
			text = text[:16<<10] + "\n…（已截断）"
		}
		return bangDoneMsg{cmd: cmd, out: text, err: err}
	}, m.spinner.Tick)
}

func (m *Model) handleBangDone(msg bangDoneMsg) (tea.Model, tea.Cmd) {
	m.streaming, m.shelling = false, false
	m.cancel = nil
	if errors.Is(msg.err, context.Canceled) {
		m.notice("已取消 !" + msg.cmd)
		return m, nil
	}
	body := msg.out
	if body == "" {
		body = "(无输出)"
	}
	m.ag.RecordShell(msg.cmd, msg.out, msg.err)
	if msg.err != nil {
		m.messages = append(m.messages, chatMessage{
			role: roleError, content: fmt.Sprintf("!%s\n%s\n%s", msg.cmd, body, msg.err.Error()), done: true,
		})
	} else {
		m.messages = append(m.messages, chatMessage{
			role: roleNotice, content: fmt.Sprintf("!%s\n%s", msg.cmd, body), done: true,
		})
	}
	m.saveCurrentSession()
	m.refreshViewport(true)
	return m, nil
}
