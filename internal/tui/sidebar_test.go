package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// gitRepo 在临时目录建一个有一次提交的仓库并切进去。
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
	}
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(s), 0o644)
	}
	write("src/a.go", "package a\n")
	write("src/gone.go", "package a\n")
	write("docs/readme.md", "hi\n")
	write(".gitignore", ".env\nbin/\n")
	run("init", "-q")
	run("add", ".")
	run("commit", "-qm", "init")
	write("src/a.go", "package a\n\nfunc A() {}\n")
	os.Remove("src/gone.go")
	write("src/new.go", "package a\n")
	write(".env", "SECRET=1\n")
	write("bin/miao", "x")
	return dir
}

func TestFileTreeShowsChanges(t *testing.T) {
	gitRepo(t)
	m := newTestModel(t, 120, 30, "ok")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if !m.side.open || !m.side.focus || !m.sidebarVisible() {
		t.Fatal("ctrl+b should open and focus the file tree")
	}
	ch := m.side.tree.changes
	if ch["src/a.go"].code != 'M' || ch["src/a.go"].add != 2 || ch["src/gone.go"].code != 'D' || ch["src/new.go"].code != 'A' {
		t.Fatalf("changes wrong: %+v", ch)
	}
	v := m.View()
	for i, line := range strings.Split(v, "\n") {
		if w := lipgloss.Width(line); w > 120 {
			t.Fatalf("line %d too wide: %d", i, w)
		}
	}
	for _, want := range []string{"改动 3 · Ctrl+B 关闭", "src/", "a.go", "+2 −0", "新", "gone.go", "docs/"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q", want)
		}
	}
	for _, bad := range []string{".env", "bin/", ".git/", "readme.md"} {
		if strings.Contains(v, bad) {
			t.Fatalf("view should not show %q", bad)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.side.focus || !m.side.open {
		t.Fatal("esc should only return focus")
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.sidebarVisible() || m.viewport.Width != 100 {
		t.Fatal("should hide under 110 cols")
	}
}

func selectTree(t *testing.T, m *Model, p string) {
	t.Helper()
	for i, r := range m.treeRows() {
		if r.n.path == p {
			m.side.tree.sel = i
			return
		}
	}
	t.Fatalf("%s not in tree", p)
}

func TestFileTreePreviewAndMention(t *testing.T) {
	gitRepo(t)
	m := newTestModel(t, 120, 30, "ok")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})

	selectTree(t, m, "src/a.go")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.View(), "+ func A() {}") {
		t.Fatalf("enter on changed file should show its diff in the drawer: %v %q", m.side.tree.preview != nil, m.View())
	}

	selectTree(t, m, "docs")
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	selectTree(t, m, "docs/readme.md")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if d := m.View(); !strings.Contains(d, "只读预览") || !strings.Contains(d, "1  hi") {
		t.Fatalf("unchanged file should get read-only preview: %q", d)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})
	if got := m.input.Value(); got != "@docs/readme.md " {
		t.Fatalf("@ should insert reference, got %q", got)
	}
	if m.side.focus {
		t.Fatal("@ should return focus to input")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.side.tree.preview != nil {
		t.Fatal("esc should close the preview")
	}
}
