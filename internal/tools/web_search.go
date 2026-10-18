package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	webSearchMaxResults = 8
	webSearchEndpoint   = "https://html.duckduckgo.com/html/"
)

func registerWebSearch(r *Registry) {
	r.Register(Tool{
		Def: toolDef("web_search", "公开网页搜索，返回标题、链接和摘要。查外部资料时用；打开某条结果再用 web_fetch。不要用来搜工作区里的代码。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "搜索关键词"},
			},
			"required": []string{"query"},
		}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return Result{Content: "参数解析失败: " + err.Error(), IsError: true}
			}
			q := strings.TrimSpace(in.Query)
			if q == "" {
				return Result{Content: "query 不能为空", IsError: true}
			}
			out, err := webSearch(ctx, q)
			if err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			return Result{Content: out}
		},
	})
}

func webSearch(ctx context.Context, query string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, webFetchTimeout)
	defer cancel()
	u, _ := url.Parse(webSearchEndpoint)
	if err := checkHostPublic(ctx, u.Hostname()); err != nil {
		return "", err
	}
	form := url.Values{"q": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "miao/0.1")
	resp, err := webFetchClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("搜索失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("搜索失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, webFetchMaxBody))
	if err != nil {
		return "", err
	}
	hits := parseDDG(string(body))
	if len(hits) == 0 {
		return "(没有搜索结果)", nil
	}
	var b strings.Builder
	for i, h := range hits {
		if i >= webSearchMaxResults {
			break
		}
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, h.title, h.href)
		if h.snippet != "" {
			fmt.Fprintf(&b, "   %s\n", h.snippet)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type ddgHit struct{ title, href, snippet string }

func parseDDG(page string) []ddgHit {
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return nil
	}
	var hits []ddgHit
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			var cls, href string
			for _, a := range n.Attr {
				switch a.Key {
				case "class":
					cls = a.Val
				case "href":
					href = a.Val
				}
			}
			if strings.Contains(cls, "result__a") && href != "" {
				title := strings.TrimSpace(nodeText(n))
				if title != "" {
					hits = append(hits, ddgHit{title: title, href: unwrapDDG(href)})
				}
			}
		}
		if n.Type == html.ElementNode && strings.Contains(nodeAttr(n, "class"), "result__snippet") {
			if len(hits) > 0 && hits[len(hits)-1].snippet == "" {
				hits[len(hits)-1].snippet = strings.TrimSpace(nodeText(n))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return hits
}

func unwrapDDG(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		return uddg
	}
	return href
}

func nodeAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(nodeText(c))
	}
	return b.String()
}
