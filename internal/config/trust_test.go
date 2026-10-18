package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectTrustRefusesSymlink(t *testing.T) {
	clearConnEnv(t)
	home := os.Getenv("MIAO_HOME")
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("orig"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(home, "trusted_projects.json")); err != nil {
		t.Fatal(err)
	}
	if err := saveProjectTrust("/x/.miao/config.json", "h"); err == nil {
		t.Fatal("符号链接目标应拒绝写入")
	}
	if b, _ := os.ReadFile(victim); string(b) != "orig" {
		t.Fatalf("victim 被改写：%q", b)
	}
}

func TestProjectTrustConcurrent(t *testing.T) {
	clearConnEnv(t)
	home := os.Getenv("MIAO_HOME")
	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func(i int) { done <- saveProjectTrust(filepath.Join("/p", string(rune('a'+i))), "h") }(i)
	}
	for i := 0; i < 16; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	m := readProjectTrust()
	if len(m) != 16 {
		t.Fatalf("并发写丢失记录：%d", len(m))
	}
	ents, _ := os.ReadDir(home)
	for _, e := range ents {
		if strings.Contains(e.Name(), "tmp") {
			t.Fatalf("残留临时文件 %s", e.Name())
		}
	}
	info, err := os.Stat(filepath.Join(home, "trusted_projects.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("权限应为 0600：%v %v", info, err)
	}
}
