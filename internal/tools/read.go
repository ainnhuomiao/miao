package tools

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

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

// readTextLimited 最多读 max 字节，遇到 NUL 视为二进制。不把整个文件读进内存。
func readTextLimited(r io.Reader, max int) (string, bool, error) {
	head := make([]byte, 512)
	n, err := r.Read(head)
	if n == 0 && err == io.EOF {
		return "", false, nil
	}
	if err != nil && err != io.EOF {
		return "", false, err
	}
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return "", false, fmt.Errorf("二进制文件，拒绝读取")
	}
	if !utf8.Valid(head[:n]) {
		return "", false, fmt.Errorf("不是 UTF-8 文本")
	}
	var buf bytes.Buffer
	buf.Write(head[:n])
	if err == io.EOF {
		return buf.String(), false, nil
	}
	remain := max - n
	if remain < 0 {
		remain = 0
	}
	copied, err := io.Copy(&buf, io.LimitReader(r, int64(remain)+1))
	if err != nil {
		return "", false, err
	}
	data := buf.Bytes()
	truncated := copied > int64(remain)
	if truncated && len(data) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(data[cut]) {
			cut--
		}
		data = data[:cut]
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", false, fmt.Errorf("二进制文件，拒绝读取")
	}
	return string(data), truncated, nil
}
