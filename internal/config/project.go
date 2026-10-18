package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ainnhuomiao/miao/internal/atomicfile"

	"github.com/ainnhuomiao/miao/internal/appdir"
)

// ProjectFile 是工作区里的项目级配置（不含 API Key）。
type ProjectFile struct {
	AllowCommands []string `json:"allow_commands"`
	MaxToolRounds int      `json:"max_tool_rounds"`
	Sandbox       *bool    `json:"sandbox"`
	Protocol      string   `json:"protocol"`
	Model         string   `json:"model"`
	TimeoutSec    int      `json:"timeout_seconds"`
}

// LoadProject 读取 <workspace>/.miao/config.json（没有 .miao 时读 .agnet）。
func LoadProject(workspace string) (ProjectFile, error) {
	pf, _, _, err := loadProjectFile(workspace)
	return pf, err
}

func loadProjectFile(workspace string) (ProjectFile, string, string, error) {
	var zero ProjectFile
	dir := appdir.ProjectDir(workspace)
	if dir == "" {
		// 工作区是 $HOME 或 $MIAO_HOME：没有项目配置
		return zero, "", "", nil
	}
	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, path, "", nil
		}
		return zero, path, "", err
	}
	var pf ProjectFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return zero, path, "", err
	}
	sum := sha256.Sum256(data)
	return pf, path, hex.EncodeToString(sum[:]), nil
}

var projectTrustMu sync.Mutex

func projectTrustPath() (string, error) {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "trusted_projects.json"), nil
}

func readProjectTrust() map[string]string {
	m := map[string]string{}
	p, err := projectTrustPath()
	if err != nil {
		return m
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}

func projectTrusted(path, hash string) bool {
	if path == "" || hash == "" {
		return false
	}
	projectTrustMu.Lock()
	defer projectTrustMu.Unlock()
	return readProjectTrust()[path] == hash
}

// ProjectTrustedFor 按当前文件内容判断工作区的 .miao/config.json 是否已被 /trust-project 信任
// （与 applyProject 放行 allow_commands 用的是同一判定）。没有项目配置时视为未信任。
func ProjectTrustedFor(workspace string) bool {
	_, path, hash, err := loadProjectFile(workspace)
	return err == nil && projectTrusted(path, hash)
}

// TrustProject 记住当前工作区 .miao/config.json 的哈希，之后允许它放宽权限。
func TrustProject(workspace string) (ProjectFile, error) {
	pf, path, hash, err := loadProjectFile(workspace)
	if err != nil {
		return pf, err
	}
	if hash == "" {
		return pf, os.ErrNotExist
	}
	if err := saveProjectTrust(path, hash); err != nil {
		return pf, err
	}
	return pf, nil
}

func saveProjectTrust(path, hash string) error {
	projectTrustMu.Lock()
	defer projectTrustMu.Unlock()
	p, err := projectTrustPath()
	if err != nil {
		return err
	}
	m := readProjectTrust()
	m[path] = hash
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WritePrivate(p, append(data, '\n'))
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(Getenv(key))
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return n
	}
	return def
}

func envFloat(key string) float64 {
	v := strings.TrimSpace(Getenv(key))
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func envDurationSec(key string, def time.Duration) time.Duration {
	n := envInt(key, 0)
	if n <= 0 {
		return def
	}
	return time.Duration(n) * time.Second
}
