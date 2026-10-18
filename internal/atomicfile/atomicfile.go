// Package atomicfile 提供私有文件的原子写入：同目录随机临时文件 → 0600 → fsync → rename。
package atomicfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrSymlink 表示目标文件或其所在目录是符号链接，拒绝写入。
var ErrSymlink = errors.New("目标是符号链接，拒绝写入")

// WritePrivate 原子写入 path：目录不存在时按 0700 创建；目录或目标是符号链接时拒绝；
// 先写同目录随机命名的临时文件（0600）并 fsync，再 rename 到目标，最后 fsync 目录。
func WritePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(dir); err != nil {
		return err
	} else if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s: %w", dir, ErrSymlink)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: %w", path, ErrSymlink)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s 不是普通文件", path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
