// Package appdir 统一管理程序名和用户级目录（配置、数据、状态、项目目录），
// 并负责把旧名字 agnet 的目录迁移到新名字 miao。只用标准库。
package appdir

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Name 是程序名，也是各用户目录下的子目录名。
	Name = "miao"
	// OldName 是改名前的名字，只用于迁移和兼容读取。
	OldName = "agnet"
	// ProjectDirName 是工作区里的项目级目录。
	ProjectDirName = "." + Name
	// OldProjectDirName 是旧的项目级目录，新目录不存在时作为回退读取。
	OldProjectDirName = "." + OldName
)

// configBase 返回 $XDG_CONFIG_HOME（默认 ~/.config），与 os.UserConfigDir 一致。
func configBase() (string, error) { return os.UserConfigDir() }

func dataBase() (string, error) { return xdgBase("XDG_DATA_HOME", ".local", "share") }

func stateBase() (string, error) { return xdgBase("XDG_STATE_HOME", ".local", "state") }

func xdgBase(env string, rel ...string) (string, error) {
	if x := os.Getenv(env); x != "" {
		return x, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{home}, rel...)...), nil
}

func join(base func() (string, error), name string) (string, error) {
	b, err := base()
	if err != nil {
		return "", err
	}
	return filepath.Join(b, name), nil
}

// ConfigDir 返回 $XDG_CONFIG_HOME/miao。
func ConfigDir() (string, error) { return join(configBase, Name) }

// DataDir 返回 $XDG_DATA_HOME/miao（默认 ~/.local/share/miao）。
func DataDir() (string, error) { return join(dataBase, Name) }

// OldDataDir 返回旧数据目录 $XDG_DATA_HOME/agnet，用于兼容会话里的旧绝对路径。
func OldDataDir() (string, error) { return join(dataBase, OldName) }

// StateDir 返回 $XDG_STATE_HOME/miao（默认 ~/.local/state/miao）。
func StateDir() (string, error) { return join(stateBase, Name) }

// ProjectDir 返回工作区里的项目级目录：.miao 存在就用它，否则 .agnet 存在就回退到它，
// 都不存在返回 .miao。不会自动移动用户的项目文件。
func ProjectDir(workspace string) string {
	p := filepath.Join(workspace, ProjectDirName)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	old := filepath.Join(workspace, OldProjectDirName)
	if _, err := os.Stat(old); err == nil {
		return old
	}
	return p
}

// Migrate 把旧的配置/数据/状态目录迁移到新名字；新目录已存在时什么都不做（绝不覆盖）。
// 返回给用户看的一次性提示。
func Migrate() []string {
	var notes []string
	for _, base := range []func() (string, error){configBase, dataBase, stateBase} {
		b, err := base()
		if err != nil {
			continue
		}
		oldDir, newDir := filepath.Join(b, OldName), filepath.Join(b, Name)
		moved, err := migrateDir(oldDir, newDir)
		if err != nil {
			notes = append(notes, fmt.Sprintf("迁移 %s 到 %s 失败：%v", tilde(oldDir), tilde(newDir), err))
		} else if moved {
			notes = append(notes, fmt.Sprintf("已把配置从 %s 迁移到 %s", tilde(oldDir), tilde(newDir)))
		}
	}
	return notes
}

// migrateDir 在 newDir 不存在且 oldDir 是目录时迁移；先 os.Rename，跨设备失败时递归复制后删除旧目录。
func migrateDir(oldDir, newDir string) (bool, error) {
	if _, err := os.Lstat(newDir); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	info, err := os.Stat(oldDir)
	if err != nil || !info.IsDir() {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(oldDir, newDir); err == nil {
		return true, nil
	}
	tmp := newDir + ".migrating"
	_ = os.RemoveAll(tmp)
	if err := copyTree(oldDir, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return false, err
	}
	if _, err := os.Lstat(newDir); err == nil {
		_ = os.RemoveAll(tmp)
		return false, nil
	}
	if err := os.Rename(tmp, newDir); err != nil {
		_ = os.RemoveAll(tmp)
		return false, err
	}
	_ = os.RemoveAll(oldDir)
	return true, nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			return copyFile(p, target, info.Mode().Perm())
		}
		return nil
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// tilde 把家目录前缀显示成 ~。
func tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}
