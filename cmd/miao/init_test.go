package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ainnhuomiao/miao/internal/config"
	"github.com/ainnhuomiao/miao/internal/setup"
)

func TestWantWizard(t *testing.T) {
	if !wantWizard(false, false, true, true) {
		t.Fatal("交互 TUI 应打开向导")
	}
	for _, c := range [][4]bool{{true, false, true, true}, {false, true, true, true}, {false, false, false, true}, {false, false, true, false}} {
		if wantWizard(c[0], c[1], c[2], c[3]) {
			t.Fatalf("非交互不应打开向导：%v", c)
		}
	}
	msg := config.ErrMissingAPIKey.Error()
	if !strings.Contains(msg, "miao init") || !strings.Contains(msg, "MIAO_API_KEY") {
		t.Fatalf("非交互报错要同时提到 miao init 和 MIAO_API_KEY：%s", msg)
	}
}

func TestInitFlow(t *testing.T) {
	const secret = "sk-init-secret-999"
	var out bytes.Buffer
	keys := []string{secret, secret}
	calls := 0
	var saved []config.Credentials
	err := initFlow(initIO{
		in:  bufio.NewReader(strings.NewReader("9\n3\nnope\nhttps://my.example/v1\n\n")),
		out: &out,
		readSecret: func() (string, error) {
			k := keys[0]
			keys = keys[1:]
			return k, nil
		},
		tester: func(_ context.Context, c config.Credentials) (config.Credentials, error) {
			calls++
			if calls == 1 {
				return c, &setup.TestError{Kind: setup.KindNetwork, Err: errors.New("dial tcp: refused")}
			}
			c.Model = "m1"
			return c, nil
		},
		saver: func(c config.Credentials) (string, error) {
			saved = append(saved, c)
			return "已保存到 ~/.miao/credentials", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"第一次见面，先配一下 key", "没有这个选项", "http://", "网络不通", "连上了"} {
		if !strings.Contains(s, want) {
			t.Fatalf("输出缺少 %q：%s", want, s)
		}
	}
	if strings.Contains(s, secret) {
		t.Fatal("输出里不能有 key")
	}
	if len(saved) != 1 || saved[0].BaseURL != "https://my.example/v1" || saved[0].APIKey != secret || saved[0].Model != "m1" {
		t.Fatalf("saved=%v", saved)
	}
}
