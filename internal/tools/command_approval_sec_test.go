package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 安全审查 PoC：这些命令原先在白名单里免审批，但能执行代码或读到密钥文件。
func TestApprovalBypassPoCs(t *testing.T) {
	ws := t.TempDir()
	mustApprove := [][]string{
		// go build/test/vet 会编译运行仓库代码（_test.go 的 init、cgo、vettool）
		{"go", "test", "./..."},
		{"go", "build"},
		{"go", "build", "./..."},
		{"go", "vet", "./..."},
		{"go", "vet", "-vettool=/tmp/evil", "./..."},
		{"go", "vet", "--vettool", "/tmp/evil"},
		// sort 只放行白名单参数
		{"sort", "--compress-program=sh", "a.txt"},
		{"sort", "--compress-program", "sh", "a.txt"},
		{"sort", "-T", "/tmp", "a.txt"},
		{"sort", "--temporary-directory=/tmp", "a.txt"},
		{"sort", "-o", "out.txt", "a.txt"},
		{"sort", "-ro", "out.txt", "a.txt"},
		{"sort", "--output=out.txt", "a.txt"},
		{"sort", "--files0-from=list"},
		{"sort", "--random-source=f", "a.txt"},
		{"sort", "-x", "a.txt"},
		// grep 递归会扫到 .env
		{"grep", "-r", "KEY"},
		{"grep", "-R", "KEY", "."},
		{"grep", "--recursive", "KEY"},
		{"grep", "--dereference-recursive", "KEY"},
		{"grep", "-rn", "KEY"},
		{"grep", "-nr", "KEY"},
		{"grep", "-inR", "KEY"},
		{"grep", "-d", "recurse", "KEY", "."},
		{"grep", "-drecurse", "KEY", "."},
		{"grep", "--directories=recurse", "KEY", "."},
		{"grep", "--directories", "recurse", "KEY", "."},
		{"grep", "--recurs", "KEY", "."},
		{"grep", "--rec", "KEY", "."},
		{"grep", "--direc=recurse", "KEY", "."},
		{"grep", "--dir", "recurse", "KEY", "."},
		{"grep", "--deref", "KEY", "."},
		{"grep", "--de", "KEY", "."},
		// diff -r / --recursive 会把目录里的 .env 打出来
		{"diff", "-r", ".", "sub"},
		{"diff", "--recursive", ".", "sub"},
		{"diff", "--recurs", ".", "sub"},
		{"diff", "-ur", "a", "b"},
		{"diff", "-ru", "a", "b"},
		{"diff", "-Naur", "old", "new"},
		// rg 关掉忽略规则/开启隐藏文件
		{"rg", "--hidden", "KEY"},
		{"rg", "-.", "KEY"},
		{"rg", "--no-ignore", "KEY"},
		{"rg", "--no-ignore-vcs", "KEY"},
		{"rg", "-u", "KEY"},
		{"rg", "-uu", "KEY"},
		{"rg", "-uuu", "KEY"},
		{"rg", "-nu", "KEY"},
		{"rg", "--unrestricted", "KEY"},
		{"rg", "-L", "KEY"},
		{"rg", "--follow", "KEY"},
	}
	for _, c := range mustApprove {
		if !needsApprovalIn(ws, c[0], c[1:]) {
			t.Errorf("%s 应需要审批", strings.Join(c, " "))
		}
		if c[0] != "go" && !HasDangerousFlag(c[0], c[1:]) {
			t.Errorf("%s 应被 HasDangerousFlag 识别（ALLOW_COMMANDS 也不能放行）", strings.Join(c, " "))
		}
	}
	if !HasDangerousFlag("go", []string{"vet", "-vettool=/tmp/evil"}) {
		t.Error("go vet -vettool 应被 HasDangerousFlag 识别")
	}

	stillFree := [][]string{
		{"go", "version"},
		{"go", "env", "GOPATH"},
		{"go", "list", "./..."},
		{"sort", "a.txt"},
		{"sort", "-rn", "a.txt"},
		{"sort", "-k2", "-t,", "a.txt"},
		{"sort", "-k", "2,2n", "-t", ",", "a.txt"},
		{"sort", "-t", "-o", "a.txt"},
		{"sort", "--key=2", "--field-separator=:", "-u", "a.txt"},
		{"sort", "-hVsfbu", "a.txt"},
		{"grep", "-n", "foo", "a.txt"},
		{"grep", "-in", "foo", "a.txt"},
		{"grep", "-e", "-r", "a.txt"},
		{"grep", "-erm", "a.txt"},
		{"grep", "-A", "3", "foo", "a.txt"},
		{"grep", "--", "-r", "a.txt"},
		{"grep", "--dir=read", "KEY", "a.txt"},
		{"grep", "--di=skip", "KEY", "a.txt"},
		{"grep", "--directories=read", "KEY", "a.txt"},
		{"grep", "--devices=skip", "KEY", "a.txt"},
		{"diff", "-u", "a", "b"},
		{"diff", "-q", "a.txt", "b.txt"},
		{"diff", "-U", "3", "a", "b"},
		{"diff", "-I", "r", "a", "b"},
		{"diff", "--exclude", "-r", "a", "b"},
		{"diff", "--", "-r", "a", "b"},
		{"rg", "foo"},
		{"rg", "-n", "foo"},
		{"rg", "-tgo", "foo"},
		{"rg", "-e", "-u", "a.txt"},
		{"rg", "-r", "u", "foo"},
		{"rg", "-g", "*.go", "foo"},
		{"git", "status"},
		{"git", "diff", "--stat"},
	}
	for _, c := range stillFree {
		if needsApprovalIn(ws, c[0], c[1:]) {
			t.Errorf("%s 不应需要审批", strings.Join(c, " "))
		}
	}
}

// diff . sub 不带 -r 也会把两边的 .env 打出来；参数只有目录名，路径检查发现不了。
func TestDiffDirectoryPairReadsEnv(t *testing.T) {
	if _, err := exec.LookPath("diff"); err != nil {
		t.Skip("no diff")
	}
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".env"), []byte("SECRET=real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "sub", ".env"), []byte("SECRET=fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("diff", ".", "sub")
	cmd.Dir = ws
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "SECRET=real") {
		t.Fatalf("对照：diff . sub 应打印 .env：%s", out)
	}
	if !needsApprovalIn(ws, "diff", []string{".", "sub"}) {
		t.Fatal("diff . sub 应需要审批")
	}
	if HasDangerousFlag("diff", []string{".", "sub"}) {
		t.Fatal("目录比较没有危险标志，靠工作区判断")
	}
	if !ImplicitCommandRisk(ws, "diff", []string{".", "sub"}) {
		t.Fatal("两个目录应被 ImplicitCommandRisk 拦住")
	}
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if needsApprovalIn(ws, "diff", []string{"-u", "a.txt", "b.txt"}) {
		t.Fatal("比较两个文件不应需要审批")
	}
}
