package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ainnhuomiao/miao/internal/appdir"
)

const (
	auditFileName = "audit.log"
	maxAuditField = 240
)

var maxAuditBytes int64 = 1 << 20 // 1 MiB，超出则轮换成 .old

func auditCommand(workspace, display, decision string) {
	auditEvent(workspace, "run_command", display, decision)
}

// skipAudit 在 `go test` 二进制里默认跳过，避免把测试审批写进用户的 audit.log。
var skipAudit = strings.HasSuffix(os.Args[0], ".test")

func auditEvent(workspace, tool, display, decision string) {
	if skipAudit {
		return
	}
	dir, err := appdir.DataDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	path := filepath.Join(dir, auditFileName)
	if info, err := os.Stat(path); err == nil && info.Size() >= maxAuditBytes {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s\t%s\t%s\t%s\t%s\n",
		time.Now().Format(time.RFC3339), decision, tool, workspace, clipAudit(display))
}

func clipAudit(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= maxAuditField {
		return s
	}
	r := []rune(s)
	return string(r[:maxAuditField-1]) + "…"
}
