package config

import "testing"

func TestGetenvFallback(t *testing.T) {
	t.Setenv("AGNET_X_TEST", "old")
	t.Setenv("MIAO_X_TEST", "new")
	if got := Getenv("X_TEST"); got != "new" {
		t.Fatalf("新名应优先：%q", got)
	}
	t.Setenv("MIAO_X_TEST", "")
	if got := Getenv("X_TEST"); got != "" {
		t.Fatalf("新名已设置（即使为空）也应优先：%q", got)
	}
	if got := Getenv("Z_UNSET_TEST"); got != "" {
		t.Fatalf("都没设置应为空：%q", got)
	}
}

func TestGetenvOldOnly(t *testing.T) {
	t.Setenv("AGNET_ONLY_TEST", "old")
	if got := Getenv("ONLY_TEST"); got != "old" {
		t.Fatalf("新名未设置时应回退旧名：%q", got)
	}
}
