package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	webFetchMaxOutput = 100 << 10 // 返回给模型的上限 100KiB
	webFetchMaxBody   = 5 << 20   // 最多下载 5MiB
	webFetchTimeout   = 15 * time.Second
)

// webFetchClient 可在测试中替换。
var webFetchClient = &http.Client{Timeout: webFetchTimeout}

func registerWebFetch(r *Registry) {
	r.Register(Tool{
		Def: toolDef("web_fetch", "抓取一个 http/https 网页，HTML 会转成 Markdown 返回（上限 100KiB，超时 15 秒）。适合查文档、看报错说明。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "完整网址，如 https://pkg.go.dev/net/http"},
			},
			"required": []string{"url"},
		}),
		Run: func(ctx context.Context, args json.RawMessage) Result {
			var in struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return Result{Content: "参数解析失败: " + err.Error(), IsError: true}
			}
			out, err := webFetch(ctx, strings.TrimSpace(in.URL))
			if err != nil {
				return Result{Content: err.Error(), IsError: true}
			}
			return Result{Content: out}
		},
	})
}

func webFetch(ctx context.Context, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("url 非法，只支持 http/https 完整网址: %q", raw)
	}
	ctx, cancel := context.WithTimeout(ctx, webFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "agnet/1.0 (+web_fetch)")
	req.Header.Set("Accept", "text/html,text/markdown,text/plain,application/json;q=0.9,*/*;q=0.5")
	resp, err := webFetchClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("抓取超时（%s）: %s", webFetchTimeout, u)
		}
		return "", fmt.Errorf("抓取失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, u)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, webFetchMaxBody))
	if err != nil {
		return "", fmt.Errorf("读取失败: %v", err)
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mt == "" {
		mt = http.DetectContentType(body)
		mt, _, _ = mime.ParseMediaType(mt)
	}
	var text string
	switch {
	case mt == "text/html" || mt == "application/xhtml+xml":
		text = htmlToMarkdown(string(body))
	case strings.HasPrefix(mt, "text/") || mt == "application/json" || strings.HasSuffix(mt, "+json") || strings.HasSuffix(mt, "+xml") || mt == "application/xml":
		text = string(body)
	default:
		return "", fmt.Errorf("不支持的内容类型 %s（只能抓文本和网页）", mt)
	}
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
	}
	header := fmt.Sprintf("URL: %s\n\n", resp.Request.URL)
	return header + truncateUTF8(strings.TrimSpace(text), webFetchMaxOutput), nil
}

func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n\n…（已截断，原文 %d 字节，只保留前 %dKiB）", len(s), n>>10)
}

var (
	reBlankLines = regexp.MustCompile(`\n{3,}`)
	reSpaces     = regexp.MustCompile(`[ \t\r\f\v]+`)
)

// htmlToMarkdown 把 HTML 粗略转成 Markdown：保留标题、段落、列表、链接、代码、引用，丢掉脚本样式和导航。
func htmlToMarkdown(src string) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return src
	}
	root := doc
	if m := findFirst(doc, atom.Main); m != nil {
		root = m
	} else if a := findFirst(doc, atom.Article); a != nil {
		root = a
	} else if b := findFirst(doc, atom.Body); b != nil {
		root = b
	}
	var c mdConv
	if t := findFirst(doc, atom.Title); t != nil && root != doc {
		if title := strings.TrimSpace(textOf(t)); title != "" {
			c.b.WriteString("# " + title + "\n\n")
		}
	}
	c.node(root)
	out := reBlankLines.ReplaceAllString(c.b.String(), "\n\n")
	return strings.TrimSpace(out)
}

func findFirst(n *html.Node, a atom.Atom) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if f := findFirst(ch, a); f != nil {
			return f
		}
	}
	return nil
}

func textOf(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		sb.WriteString(textOf(ch))
	}
	return sb.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

type mdConv struct {
	b     strings.Builder
	list  []int // 每层列表：-1 表示无序，否则为有序当前序号
	inPre bool
}

func (c *mdConv) block() {
	s := c.b.String()
	if s == "" || strings.HasSuffix(s, "\n\n") {
		return
	}
	if strings.HasSuffix(s, "\n") {
		c.b.WriteString("\n")
	} else {
		c.b.WriteString("\n\n")
	}
}

func (c *mdConv) children(n *html.Node) {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.node(ch)
	}
}

func (c *mdConv) node(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		if c.inPre {
			c.b.WriteString(n.Data)
			return
		}
		t := reSpaces.ReplaceAllString(strings.ReplaceAll(n.Data, "\n", " "), " ")
		if strings.TrimSpace(t) == "" {
			if s := c.b.String(); s != "" && !strings.HasSuffix(s, " ") && !strings.HasSuffix(s, "\n") {
				c.b.WriteString(" ")
			}
			return
		}
		c.b.WriteString(t)
		return
	case html.DocumentNode:
		c.children(n)
		return
	case html.ElementNode:
	default:
		return
	}
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Noscript, atom.Nav, atom.Footer, atom.Header, atom.Svg, atom.Iframe, atom.Form, atom.Button, atom.Head, atom.Template:
		return
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		c.block()
		level := int(n.Data[1] - '0')
		c.b.WriteString(strings.Repeat("#", level) + " " + strings.TrimSpace(reSpaces.ReplaceAllString(strings.ReplaceAll(textOf(n), "\n", " "), " ")))
		c.block()
	case atom.P, atom.Div, atom.Section, atom.Table, atom.Dl:
		c.block()
		c.children(n)
		c.block()
	case atom.Br:
		c.b.WriteString("\n")
	case atom.Hr:
		c.block()
		c.b.WriteString("---")
		c.block()
	case atom.Tr:
		c.children(n)
		c.b.WriteString("\n")
	case atom.Td, atom.Th:
		c.b.WriteString("| ")
		c.children(n)
		c.b.WriteString(" ")
	case atom.Ul, atom.Ol:
		c.block()
		start := -1
		if n.DataAtom == atom.Ol {
			start = 0
		}
		c.list = append(c.list, start)
		c.children(n)
		c.list = c.list[:len(c.list)-1]
		c.block()
	case atom.Li:
		if s := c.b.String(); s != "" && !strings.HasSuffix(s, "\n") {
			c.b.WriteString("\n")
		}
		depth := len(c.list)
		indent := ""
		if depth > 1 {
			indent = strings.Repeat("  ", depth-1)
		}
		marker := "- "
		if depth > 0 && c.list[depth-1] >= 0 {
			c.list[depth-1]++
			marker = fmt.Sprintf("%d. ", c.list[depth-1])
		}
		c.b.WriteString(indent + marker)
		c.children(n)
	case atom.Pre:
		c.block()
		lang := ""
		if code := findFirst(n, atom.Code); code != nil {
			for _, cls := range strings.Fields(attr(code, "class")) {
				if l, ok := strings.CutPrefix(cls, "language-"); ok {
					lang = l
				}
			}
		}
		c.b.WriteString("```" + lang + "\n")
		c.inPre = true
		c.b.WriteString(strings.TrimRight(textOf(n), "\n"))
		c.inPre = false
		c.b.WriteString("\n```")
		c.block()
	case atom.Code:
		if c.inPre {
			c.children(n)
			return
		}
		c.b.WriteString("`" + textOf(n) + "`")
	case atom.Strong, atom.B:
		c.b.WriteString("**")
		c.children(n)
		c.b.WriteString("**")
	case atom.Em, atom.I:
		c.b.WriteString("*")
		c.children(n)
		c.b.WriteString("*")
	case atom.Blockquote:
		c.block()
		var sub mdConv
		sub.children(n)
		for _, line := range strings.Split(strings.TrimSpace(sub.b.String()), "\n") {
			c.b.WriteString("> " + line + "\n")
		}
		c.block()
	case atom.A:
		href := attr(n, "href")
		text := strings.TrimSpace(reSpaces.ReplaceAllString(strings.ReplaceAll(textOf(n), "\n", " "), " "))
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") || text == "" {
			c.b.WriteString(text)
			return
		}
		c.b.WriteString("[" + text + "](" + href + ")")
	case atom.Img:
		if alt := attr(n, "alt"); alt != "" {
			c.b.WriteString("![" + alt + "]")
		}
	default:
		c.children(n)
	}
}
