package tui

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
)

// cachedTTY256 和 chroma 的 terminal256 输出一字不差，但把每种 token 的转义序列缓存起来。
// 原版每渲染一个代码块都要对样式里每种 token 在 256 色表里找最接近的颜色（findClosest），
// 长会话首次渲染时这是最大的热点。
const chromaFormatterName = "miao256"

type cachedTTY256 struct {
	mu    sync.Mutex
	cache map[*chroma.Style]map[chroma.TokenType]string
}

var chromaTTY = &cachedTTY256{cache: map[*chroma.Style]map[chroma.TokenType]string{}}

func init() { formatters.Register(chromaFormatterName, chromaTTY) }

// escape 返回某种 token 的颜色前缀；第一次遇到时交给原版 terminal256 算一次。
func (c *cachedTTY256) escape(style *chroma.Style, t chroma.TokenType) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.cache[style]
	if m == nil {
		m = map[chroma.TokenType]string{}
		c.cache[style] = m
	}
	if s, ok := m[t]; ok {
		return s, nil
	}
	var buf bytes.Buffer
	done := false
	it := func() chroma.Token {
		if done {
			return chroma.EOF
		}
		done = true
		return chroma.Token{Type: t}
	}
	if err := formatters.TTY256.Format(&buf, style, it); err != nil {
		return "", err
	}
	s := strings.TrimSuffix(buf.String(), "\033[0m")
	m[t] = s
	return s, nil
}

func (c *cachedTTY256) Format(w io.Writer, style *chroma.Style, it chroma.Iterator) error {
	for token := it(); token != chroma.EOF; token = it() {
		clr, err := c.escape(style, token.Type)
		if err != nil {
			return err
		}
		if clr != "" {
			fmt.Fprint(w, clr)
		}
		fmt.Fprint(w, token.Value)
		if clr != "" {
			fmt.Fprint(w, "\033[0m")
		}
	}
	return nil
}
