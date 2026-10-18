package tools

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// IsSensitivePath 报告相对路径是否是 .env / .git 这类不能读的敏感文件。
func IsSensitivePath(rel string) bool { return isSensitive(rel) }

// PreviewFile 只读预览工作区内的文件，最多 maxLines 行；与 read_file 同样的限制：
// 不越出工作区、不跟随符号链接、不读敏感文件、不读二进制。truncated 表示还有更多行。
func PreviewFile(workspace, rel string, maxLines int) (lines []string, truncated bool, err error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, false, fmt.Errorf("路径越出工作区")
	}
	if isSensitive(clean) {
		return nil, false, fmt.Errorf("敏感文件，不预览")
	}
	full := filepath.Join(workspace, clean)
	info, err := os.Lstat(full)
	if err != nil {
		return nil, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("符号链接，不预览")
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("不是普通文件")
	}
	r := &Registry{workspace: workspace}
	if err := r.checkNoSymlinkEscape(full); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(full, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		l := sc.Text()
		if strings.IndexByte(l, 0) >= 0 {
			return nil, false, fmt.Errorf("二进制文件，不预览")
		}
		if len(lines) >= maxLines {
			return lines, true, nil
		}
		lines = append(lines, l)
	}
	return lines, false, sc.Err()
}
