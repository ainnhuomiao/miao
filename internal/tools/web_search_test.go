package tools

import "testing"

func TestParseDDG(t *testing.T) {
	html := `<html><body>
	<a class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev">The Go Programming Language</a>
	<a class="result__snippet">Go is an open source</a>
	</body></html>`
	hits := parseDDG(html)
	if len(hits) != 1 || hits[0].title != "The Go Programming Language" {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].href != "https://go.dev" {
		t.Fatalf("href = %q", hits[0].href)
	}
}
