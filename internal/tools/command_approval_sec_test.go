package tools

import (
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
