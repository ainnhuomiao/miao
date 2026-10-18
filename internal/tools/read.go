package tools

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	info, err := os.Lstat(full)
	if err != nil {
		return nil, "", fmt.Errorf("无法读取: %w", err)
	}
	if info.IsDir() {
		return nil, "", fmt.Errorf("目标是目录，请用 list_dir")
	}
	if info.Mode()&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket|os.ModeCharDevice) != 0 {
		return nil, "", fmt.Errorf("拒绝读取设备或管道文件")
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, "", fmt.Errorf("读取失败: %w", err)
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
