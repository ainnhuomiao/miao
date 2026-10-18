package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 超过上限的模型列表响应只读前 4 MiB，解析失败而不是整个读进内存。
func TestListModelsBodyCapped(t *testing.T) {
	pad := strings.Repeat("x", maxModelsBody+1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"` + pad + `"}]}`))
	}))
	defer srv.Close()
	if _, err := NewOpenAI("k", srv.URL, "m").ListModels(context.Background()); err == nil {
		t.Fatal("OpenAI：超大响应应被截断并报错")
	}
	if _, err := NewAnthropic("k", srv.URL, "m").ListModels(context.Background()); err == nil {
		t.Fatal("Anthropic：超大响应应被截断并报错")
	}
}
