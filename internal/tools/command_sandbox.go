package tools

import (
	"os/exec"
	"strings"

	"github.com/ainnhuomiao/miao/internal/config"
)

// wrapSandbox 在 bubblewrap 可用时把命令关进「根只读、工作区可写」的命名空间。
// 默认关闭；设 MIAO_SANDBOX=1 开启。go / git 需要工作区外的缓存目录，不套沙箱。
func wrapSandbox(dir, name string, args []string) (string, []string) {
	if !sandboxEnabled() || !sandboxable(name) {
		return name, args
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return name, args
	}
	wrapped := []string{
		"--die-with-parent",
		"--unshare-pid",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--bind", dir, dir,
		"--chdir", dir,
		"--",
		name,
	}
	return bwrap, append(wrapped, args...)
}

func sandboxEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(config.Getenv("SANDBOX")))
	switch v {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

func sandboxable(name string) bool {
	switch name {
	case "go", "git", "bwrap":
		return false
	default:
		return true
	}
}
