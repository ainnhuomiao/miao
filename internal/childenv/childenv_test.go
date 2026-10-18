package childenv

import (
	"slices"
	"testing"
)

func TestStripDropsSecrets(t *testing.T) {
	in := []string{"PATH=/bin", "MIAO_API_KEY=sk-x", "AGNET_API_KEY=sk-y", "OPENAI_BASE_URL=u",
		"GITHUB_TOKEN=t", "DB_PASSWORD=p", "AWS_SECRET_ACCESS_KEY=s", "HOME=/h", "GOPATH=/g", "broken"}
	got := Strip(in)
	want := []string{"PATH=/bin", "HOME=/h", "GOPATH=/g"}
	if !slices.Equal(got, want) {
		t.Fatalf("Strip = %v, want %v", got, want)
	}
}

func TestEnvironDropsAPIKey(t *testing.T) {
	t.Setenv("MIAO_API_KEY", "sk-leak")
	for _, kv := range Environ() {
		if kv == "MIAO_API_KEY=sk-leak" {
			t.Fatal("Environ leaked MIAO_API_KEY")
		}
	}
}

func TestGitArgsHardening(t *testing.T) {
	got := GitArgs([]string{"diff", "--stat"})
	want := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "diff.external=",
		"-c", "core.pager=cat", "-c", "core.editor=true",
		"diff", "--no-ext-diff", "--no-textconv", "--stat"}
	if !slices.Equal(got, want) {
		t.Fatalf("GitArgs = %v", got)
	}
	got = GitArgs([]string{"status", "-s"})
	if !slices.Equal(got[len(got)-2:], []string{"status", "-s"}) || got[1] != "core.fsmonitor=false" {
		t.Fatalf("GitArgs = %v", got)
	}
	if !slices.Contains(GitEnv(), "GIT_CONFIG_NOSYSTEM=1") {
		t.Fatal("GitEnv 应设置 GIT_CONFIG_NOSYSTEM=1")
	}
	if !slices.Contains(GitEnv(), "GIT_TERMINAL_PROMPT=0") {
		t.Fatal("GitEnv 应设置 GIT_TERMINAL_PROMPT=0")
	}
}
