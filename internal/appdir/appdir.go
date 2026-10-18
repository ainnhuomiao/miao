// Package appdir 统一管理程序名和用户级目录。
//
// 所有用户级文件都放在同一个家目录下：$MIAO_HOME，未设置时为 ~/.miao。
//
//	~/.miao/config.json   用户配置（主题、子代理、hooks 等），以及 mcp.json、commands/ 等
//	~/.miao/credentials   API Key（0600）
//	~/.miao/data/         会话、导出、审计日志
//	~/.miao/state/        运行状态（MCP 日志、迁移标记等）
//
// 旧版本的 XDG 目录（$XDG_CONFIG_HOME/miao 等）和更早的 agnet 目录会在首次启动时复制过来，
// 见 Migrate。只用标准库。
package appdir

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// Name 是程序名。
	Name = "miao"
	// OldName 是改名前的名字，只用于迁移和兼容读取。
	OldName = "agnet"
	// ProjectDirName 是工作区里的项目级目录。
	ProjectDirName = "." + Name
	// OldProjectDirName 是旧的项目级目录，新目录不存在时作为回退读取。
	OldProjectDirName = "." + OldName
	// HomeEnv 用来覆盖用户级家目录。
	HomeEnv = "MIAO_HOME"
	// CredentialsName 是家目录下保存 API Key 的文件名。
	CredentialsName = "credentials"
)

// Home 返回用户级家目录：$MIAO_HOME（转成绝对路径），未设置时为 ~/.miao。
func Home() (string, error) {
	if h := strings.TrimSpace(os.Getenv(HomeEnv)); h != "" {
		return filepath.Abs(h)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ProjectDirName), nil
}

func sub(name string) (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, name), nil
}

// ConfigDir 返回用户配置目录，即家目录本身（~/.miao）。
func ConfigDir() (string, error) { return Home() }

// DataDir 返回数据目录 ~/.miao/data（会话、导出、审计日志）。
func DataDir() (string, error) { return sub("data") }

// StateDir 返回状态目录 ~/.miao/state。
func StateDir() (string, error) { return sub("state") }

// CredentialsPath 返回 ~/.miao/credentials。
func CredentialsPath() (string, error) { return sub(CredentialsName) }

// OldDataDir 返回最早的旧数据目录 $XDG_DATA_HOME/agnet，用于兼容会话里的旧绝对路径。
func OldDataDir() (string, error) { return join(dataBase, OldName) }

// OldDataDirs 返回所有可能出现在旧会话绝对路径里的数据目录：
// $XDG_DATA_HOME/miao 和 $XDG_DATA_HOME/agnet。
func OldDataDirs() []string {
	var out []string
	for _, n := range []string{Name, OldName} {
		if d, err := join(dataBase, n); err == nil {
			out = append(out, d)
		}
	}
	return out
}

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

// samePath 判断两个路径是否指向同一位置（先比清理后的字符串，再比解析符号链接后的结果）。
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA == nil && errB == nil && ra == rb {
		return true
	}
	if ia, err := os.Stat(a); err == nil {
		if ib, err := os.Stat(b); err == nil {
			return os.SameFile(ia, ib)
		}
	}
	return false
}

// IsGlobalWorkspace 报告工作区是不是用户家目录 $HOME 或 $MIAO_HOME 本身，
// 或者它的 .miao 就是全局家目录。这种工作区不当项目看待，避免项目配置和全局配置混在一起。
func IsGlobalWorkspace(workspace string) bool {
	if workspace == "" {
		return false
	}
	ws, err := filepath.Abs(workspace)
	if err != nil {
		return false
	}
	if h, err := os.UserHomeDir(); err == nil && samePath(ws, h) {
		return true
	}
	if g, err := Home(); err == nil {
		if samePath(ws, g) || samePath(filepath.Join(ws, ProjectDirName), g) {
			return true
		}
	}
	return false
}

// ProjectDir 返回工作区里的项目级目录：.miao 存在就用它，否则 .agnet 存在就回退到它，
// 都不存在返回 .miao。不会自动移动用户的项目文件。
// 工作区是 $HOME 或 $MIAO_HOME（见 IsGlobalWorkspace）时返回空串：没有项目目录。
func ProjectDir(workspace string) string {
	if IsGlobalWorkspace(workspace) {
		return ""
	}
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

// Tilde 是 tilde 的导出版本，供界面显示路径。
func Tilde(p string) string { return tilde(p) }
