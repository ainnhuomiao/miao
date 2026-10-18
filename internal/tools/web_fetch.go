package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
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

// webFetchAllowPrivate 为 true 时允许访问内网地址（仅测试用）。
var webFetchAllowPrivate = false

// webFetchClient 在拨号时检查真实 IP，DNS 解析结果和重定向后的地址都会被检查，
// 拒绝 localhost、内网、链路本地等地址，防止模型借此访问本机服务。
var webFetchClient = &http.Client{
	Timeout: webFetchTimeout,
	Transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         webFetchDial,
		TLSHandshakeTimeout: 10 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("重定向次数过多")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return fmt.Errorf("重定向到不支持的协议 %s", req.URL.Scheme)
		}
		if err := checkHostPublic(req.Context(), req.URL.Hostname()); err != nil {
			return err
		}
		return checkRedirectPolicy(req)
	},
}

// webFetchDial 拨号前检查目标 IP。本机代理（如 127.0.0.1:7890）是用户自己配置的，
// 连代理本身放行；经代理访问的目标域名在 webFetch 里另行解析检查。
func webFetchDial(ctx context.Context, network, address string) (net.Conn, error) {
	d := &net.Dialer{Timeout: webFetchTimeout}
	if isProxyAddr(address) {
		return d.DialContext(ctx, network, address)
	}
	d.Control = func(_, addr string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("无法识别地址 %s", host)
		}
		if !webFetchAllowPrivate && isPrivateIP(ip) {
			return fmt.Errorf("不允许访问内网地址 %s", ip)
		}
		return nil
	}
	return d.DialContext(ctx, network, address)
}

func isProxyAddr(address string) bool {
	for _, scheme := range []string{"http", "https"} {
		req := &http.Request{URL: &url.URL{Scheme: scheme, Host: "example.com"}}
		p, err := http.ProxyFromEnvironment(req)
		if err != nil || p == nil {
			continue
		}
		port := p.Port()
		if port == "" {
			port = map[string]string{"http": "80", "https": "443", "socks5": "1080"}[p.Scheme]
		}
		if net.JoinHostPort(p.Hostname(), port) == address {
			return true
		}
	}
	return false
}

// checkHostPublic 解析域名并确认所有 IP 都不是内网地址（经代理时拨号检查看不到真实目标）。
func checkHostPublic(ctx context.Context, host string) error {
	if webFetchAllowPrivate {
		return nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil // 解析失败交给代理或拨号去报错
	}
	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return fmt.Errorf("不允许访问内网地址 %s（%s）", host, ip.IP)
		}
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
		(ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xC0 == 64) // 100.64.0.0/10 运营商级 NAT
}

func registerWebFetch(r *Registry) {
	r.Register(Tool{
		Def: toolDef("web_fetch", "抓取公开 http/https 网页并转成 Markdown。需要某页正文时用；先搜再用这个打开结果。拒绝内网地址。输出上限 100KiB，被截断时从已有内容判断是否够用，不够就换更具体的 URL。", map[string]any{
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
			raw := strings.TrimSpace(in.URL)
			ctx, res, ok := r.gateWebFetch(ctx, raw, args)
			if !ok {
				return res
			}
			out, err := webFetch(ctx, raw)
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
	if err := checkHostPublic(ctx, u.Hostname()); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "miao/1.0 (+web_fetch)")
	req.Header.Set("Accept", "text/html,text/markdown,text/plain,application/json;q=0.9,*/*;q=0.5")
	resp, err := webFetchClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("抓取超时（%s）: %s", webFetchTimeout, u)
		}
		return "", fmt.Errorf("抓取失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
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
	return s[:cut] + fmt.Sprintf("\n\n…（已截断：保留开头，原文 %d 字节，只保留前 %dKiB。换更具体的 URL 再抓。）", len(s), n>>10)
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
