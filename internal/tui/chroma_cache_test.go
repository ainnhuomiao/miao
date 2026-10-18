package tui

import (
	"bytes"
	"testing"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

const chromaSample = "package main\n\nimport \"fmt\"\n\n// 注释\nfunc main() {\n\tx := 42 + 3.5\n\tfmt.Println(\"hi\\n\", x, nil, true)\n}\n"

func highlight(t testing.TB, name string) string {
	t.Helper()
	l := lexers.Get("go")
	it, err := l.Tokenise(nil, chromaSample)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := formatters.Get(name).Format(&b, styles.Get("monokai"), it); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestCachedChromaMatchesTerminal256(t *testing.T) {
	want := highlight(t, "terminal256")
	for i := 0; i < 2; i++ { // 第二次走缓存
		if got := highlight(t, chromaFormatterName); got != want {
			t.Fatalf("round %d differs:\nwant %q\ngot  %q", i, want, got)
		}
	}
}

func TestMarkdownCodeBlockSameAsBefore(t *testing.T) {
	md := "```go\n" + chromaSample + "```\n"
	got := renderMarkdown(md, 80)
	mdRenderer = nil
	if again := renderMarkdown(md, 80); again != got {
		t.Fatal("rendering not stable")
	}
	if !bytes.Contains([]byte(got), []byte("\x1b[")) {
		t.Fatal("code block should be highlighted")
	}
}

func BenchmarkChromaTerminal256(b *testing.B) {
	for i := 0; i < b.N; i++ {
		highlight(b, "terminal256")
	}
}

func BenchmarkChromaCached(b *testing.B) {
	for i := 0; i < b.N; i++ {
		highlight(b, chromaFormatterName)
	}
}
