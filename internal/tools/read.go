package tools

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"
)

const defaultReadLines = 2000

// openForRead 打开工作区内的普通文件：拒绝符号链接越界、敏感文件、设备/管道。
func (r *Registry) openForRead(rel string) (*os.File, string, error) {
	full, err := r.ResolvePath(rel)
	if err != nil {
		return nil, "", err
	}
	if err := r.checkNoSymlinkEscape(full); err != nil {
		return nil, "", err
	}
	relToRoot, _ := filepath.Rel(r.workspace, full)
	if isSensitive(relToRoot) {
		return nil, "", fmt.Errorf("禁止读取敏感文件: %s", relToRoot)
	}
	f, err := openRegular(full)
	if err != nil {
		return nil, "", err
	}
	return f, relToRoot, nil
}

func readFileWindow(f *os.File, offset, limit int) (string, error) {
	head := make([]byte, 512)
	n, err := f.Read(head)
	if n == 0 && err == io.EOF {
		return "（第 0-0 行，共 0 行。）", nil
	}
	if err != nil && err != io.EOF {
		return "", err
	}
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return "", fmt.Errorf("二进制文件，拒绝读取")
	}
	if !utf8.Valid(head[:n]) {
		return "", fmt.Errorf("不是 UTF-8 文本")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if offset < 1 {
		offset = 1
	}
	maxTake := limit
	if maxTake <= 0 {
		maxTake = defaultReadLines
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxReadBytes+1)
	var b strings.Builder
	total, taken, start, end := 0, 0, 0, 0
	bytesUsed := 0
	for sc.Scan() {
		total++
		if total < offset || taken >= maxTake {
			continue
		}
		line := strings.TrimRight(sc.Text(), "\r")
		row := fmt.Sprintf("%6d|%s\n", total, line)
		if bytesUsed+len(row) > maxReadBytes && taken > 0 {
			continue
		}
		b.WriteString(row)
		bytesUsed += len(row)
		taken++
		if start == 0 {
			start = total
		}
		end = total
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if start == 0 {
		start, end = 0, 0
	}
	footer := fmt.Sprintf("（第 %d-%d 行，共 %d 行。", start, end, total)
	if end < total {
		next := end + 1
		if next < offset {
			next = offset
		}
		footer += fmt.Sprintf("用 offset=%d 继续。", next)
	}
	footer += "）"
	out := strings.TrimRight(b.String(), "\n")
	if out != "" {
		out += "\n"
	}
	return out + footer, nil
}

// openRegular 打开普通文件：先解析符号链接，再以 O_NOFOLLOW|O_NONBLOCK 打开真实路径（最后一段被替换成链接则失败，
// FIFO 不会阻塞），最后对已打开的 fd 做 Stat，拒绝目录、设备、管道等非普通文件。
func openRegular(full string) (*os.File, error) {
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return nil, fmt.Errorf("无法读取: %w", err)
	}
	f, err := os.OpenFile(real, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("读取失败: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("读取失败: %w", err)
	}
	switch {
	case info.IsDir():
		_ = f.Close()
		return nil, fmt.Errorf("目标是目录，请用 list_dir")
	case !info.Mode().IsRegular():
		_ = f.Close()
		return nil, fmt.Errorf("拒绝读取设备或管道文件")
	}
	return f, nil
}

// readRegularFile 与 os.ReadFile 相同，但只接受普通文件（见 openRegular）。
func readRegularFile(full string) ([]byte, error) {
	f, err := openRegular(full)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
