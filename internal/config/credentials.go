package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ainnhuomiao/miao/internal/appdir"
)

// Credentials 是 ~/.miao/credentials 里保存的连接信息。只由 miao init、首次启动向导和 /login 写入，
// 绝不写进工作区 .env。
type Credentials struct {
	Provider string // deepseek / anthropic / openai
	Protocol string // openai / anthropic
	BaseURL  string
	Model    string
	APIKey   string
}

// ErrCredentialsSymlink 表示 credentials 是符号链接，拒绝读写。
var ErrCredentialsSymlink = errors.New("credentials 是符号链接，出于安全考虑拒绝读写")

// CredentialsPath 返回 ~/.miao/credentials（或 $MIAO_HOME/credentials）。
func CredentialsPath() (string, error) { return appdir.CredentialsPath() }

// LoadCredentials 读取 credentials；文件不存在返回零值和 nil。不跟随符号链接。
func LoadCredentials() (Credentials, error) {
	var c Credentials
	p, err := CredentialsPath()
	if err != nil {
		return c, err
	}
	info, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return c, ErrCredentialsSymlink
	}
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return c, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "provider":
			c.Provider = v
		case "protocol":
			c.Protocol = v
		case "base_url":
			c.BaseURL = v
		case "model":
			c.Model = v
		case "api_key":
			c.APIKey = v
		}
	}
	return c, sc.Err()
}

// SaveCredentials 原子写入 credentials：目录 0700，文件 0600；先写同目录临时文件并 fsync，再 rename。
// 目标是符号链接时拒绝写入。
func SaveCredentials(c Credentials) error {
	p, err := CredentialsPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(p); err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return ErrCredentialsSymlink
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s 不是普通文件", p)
		}
	}
	for _, v := range []string{c.Provider, c.Protocol, c.BaseURL, c.Model, c.APIKey} {
		if strings.ContainsAny(v, "\r\n") {
			return errors.New("连接信息里不能有换行")
		}
	}
	var b strings.Builder
	b.WriteString("# miao 连接信息，由 miao init / /login 生成。不要提交到仓库。\n")
	fmt.Fprintf(&b, "provider=%s\nprotocol=%s\nbase_url=%s\nmodel=%s\napi_key=%s\n",
		c.Provider, c.Protocol, c.BaseURL, c.Model, c.APIKey)

	tmp, err := os.CreateTemp(dir, ".credentials-*")
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
	if _, err := tmp.WriteString(b.String()); err != nil {
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
	if err := os.Rename(tmpName, p); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// String 打印时隐藏 Key，防止误把它写进日志或错误信息。
func (c Credentials) String() string {
	k := "未设置"
	if c.APIKey != "" {
		k = "***"
	}
	return fmt.Sprintf("{provider=%s protocol=%s base_url=%s model=%s api_key=%s}", c.Provider, c.Protocol, c.BaseURL, c.Model, k)
}

// GoString 同 String，%#v 时也不泄露 Key。
func (c Credentials) GoString() string { return c.String() }
