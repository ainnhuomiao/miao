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
