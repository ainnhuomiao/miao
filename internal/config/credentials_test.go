package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveCredentialsPermsAndAtomic(t *testing.T) {
	home := filepath.Join(t.TempDir(), "mh")
	t.Setenv("MIAO_HOME", home)
	c := Credentials{Provider: "deepseek", Protocol: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", APIKey: "sk-secret"}
	if err := SaveCredentials(c); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(home, "credentials")
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("文件权限应为 0600：%v", info.Mode().Perm())
	}
	if d, _ := os.Stat(home); d.Mode().Perm() != 0o700 {
		t.Fatalf("目录权限应为 0700：%v", d.Mode().Perm())
	}
	got, err := LoadCredentials()
	if err != nil || got != c {
		t.Fatalf("读回不一致：%+v %v", got, err)
	}
	// 覆盖写：不留临时文件，inode 换新（rename 而不是原地截断）
	before, _ := os.Stat(p)
	c.APIKey = "sk-new"
	if err := SaveCredentials(c); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	if os.SameFile(before, after) {
		t.Fatal("应通过临时文件 + rename 原子替换")
	}
	entries, _ := os.ReadDir(home)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".credentials-") {
			t.Fatalf("残留临时文件：%s", e.Name())
		}
	}
	if got, _ := LoadCredentials(); got.APIKey != "sk-new" {
		t.Fatalf("got %+v", got)
	}
}

func TestCredentialsSymlinkRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MIAO_HOME", home)
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, []byte("api_key=stolen\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(home, "credentials")); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(Credentials{APIKey: "k"}); !errors.Is(err, ErrCredentialsSymlink) {
		t.Fatalf("应拒绝写入符号链接：%v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "api_key=stolen\n" {
		t.Fatal("符号链接目标不能被改写")
	}
	if _, err := LoadCredentials(); !errors.Is(err, ErrCredentialsSymlink) {
		t.Fatalf("应拒绝读取符号链接：%v", err)
	}
}

func TestSaveCredentialsRejectsNewline(t *testing.T) {
	t.Setenv("MIAO_HOME", t.TempDir())
	if err := SaveCredentials(Credentials{APIKey: "a\nbase_url=http://evil"}); err == nil {
		t.Fatal("应拒绝换行注入")
	}
}

func TestKeyLookupOrder(t *testing.T) {
	clearConnEnv(t)
	os.Unsetenv("MIAO_API_KEY")
	os.Unsetenv("AGENT_API_KEY")
	os.Unsetenv("DEEPSEEK_API_KEY")
	if err := os.WriteFile(".env", []byte("MIAO_API_KEY=from-dotenv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 只有 .env
	cfg, err := Load()
	if err != nil || cfg.APIKey != "from-dotenv" || cfg.KeySource != KeyFromDotEnv {
		t.Fatalf(".env 兜底：%q %q %v", cfg.APIKey, cfg.KeySource, err)
	}
	os.Unsetenv("MIAO_API_KEY") // 上一次 Load 把 .env 写进了进程环境

	// credentials 优先于 .env，并带上自己的连接信息
	if err := SaveCredentials(Credentials{Provider: "anthropic", Protocol: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-x", APIKey: "from-creds"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil || cfg.APIKey != "from-creds" || cfg.KeySource != KeyFromCredentials {
		t.Fatalf("credentials 应优先于 .env：%q %q %v", cfg.APIKey, cfg.KeySource, err)
	}
	if cfg.BaseURL != "https://api.anthropic.com" || cfg.Protocol != "anthropic" || cfg.Model != "claude-x" {
		t.Fatalf("应使用 credentials 里的连接信息：%+v", cfg)
	}
	os.Unsetenv("MIAO_API_KEY")

	// 真实环境变量最优先，此时不混用 credentials 的地址
	t.Setenv("MIAO_API_KEY", "from-env")
	cfg, err = Load()
	if err != nil || cfg.APIKey != "from-env" || cfg.KeySource != KeyFromEnv {
		t.Fatalf("环境变量应最优先：%q %q %v", cfg.APIKey, cfg.KeySource, err)
	}
	if cfg.BaseURL != defaultBaseURL {
		t.Fatalf("Key 来自环境变量时不应套用 credentials 的地址：%s", cfg.BaseURL)
	}
}

func TestDotEnvBaseURLIgnoredWithCredentials(t *testing.T) {
	clearConnEnv(t)
	os.Unsetenv("MIAO_API_KEY")
	os.Unsetenv("MIAO_BASE_URL")
	os.Unsetenv("AGENT_BASE_URL")
	os.Unsetenv("DEEPSEEK_BASE_URL")
	if err := SaveCredentials(Credentials{Provider: "openai", Protocol: "openai", BaseURL: "https://mine.example/v1", APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".env", []byte("MIAO_BASE_URL=https://evil.example\nAGENT_BASE_URL=https://evil2.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, notes, err := LoadWithNotes()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://mine.example/v1" {
		t.Fatalf("工作区 .env 的地址必须忽略：%s", cfg.BaseURL)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "已忽略工作区 .env 中的 AGENT_BASE_URL") {
		t.Fatalf("notes=%q", notes)
	}
}

func TestMissingKeyMessage(t *testing.T) {
	clearConnEnv(t)
	os.Unsetenv("MIAO_API_KEY")
	_, err := Load()
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("err=%v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "miao init") || !strings.Contains(msg, "MIAO_API_KEY") || strings.Contains(msg, "cp .env.example") {
		t.Fatalf("提示文字：%s", msg)
	}
}

func TestCredentialsStringHidesKey(t *testing.T) {
	c := Credentials{Provider: "x", APIKey: "sk-hide-me"}
	for _, s := range []string{c.String(), fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c)} {
		if strings.Contains(s, "sk-hide-me") {
			t.Fatalf("打印泄露了 key：%s", s)
		}
	}
}
