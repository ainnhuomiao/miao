package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ainnhuomiao/miao/internal/appdir"
)

// ProtocolVersion 是 initialize 时声明的 MCP 协议版本。
const ProtocolVersion = "2025-06-18"

// maxLine 是单条 JSON-RPC 消息的最大字节数。
const maxLine = 32 << 20

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("MCP 错误 %d: %s", e.Code, e.Message) }

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

// Client 是一个 stdio MCP 服务器连接。
type Client struct {
	name  string
	cmd   *exec.Cmd
	stdin io.WriteCloser
	logf  *os.File

	wmu sync.Mutex // 串行写 stdin

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcResult

	done    chan struct{} // 读循环结束（进程退出或 stdout 关闭）
	doneErr error
	exited  chan struct{}
	closing sync.Once
}

// Tool 是 tools/list 返回的一个工具。
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// stderrLog 打开 ~/.miao/state/mcp/<name>.log；失败则返回 nil（丢弃 stderr）。
func stderrLog(name string) *os.File {
	dir, err := appdir.StateDir()
	if err != nil {
		return nil
	}
	dir = filepath.Join(dir, "mcp")
	if os.MkdirAll(dir, 0o700) != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dir, sanitize(name)+".log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil
	}
	return f
}

// Spawn 启动 stdio 服务器进程（独立进程组）并开始读取响应。不做握手。
func Spawn(sc ServerConfig) (*Client, error) {
	if sc.Command == "" {
		return nil, errors.New("缺少 command")
	}
	cmd := exec.Command(sc.Command, sc.Args...)
	cmd.Env = buildEnv(os.Environ(), sc.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c := &Client{
		name:    sc.Name,
		cmd:     cmd,
		pending: map[int64]chan rpcResult{},
		done:    make(chan struct{}),
		exited:  make(chan struct{}),
	}
	// stderr 直接指向文件（或 /dev/null），由内核写入，永远不会阻塞子进程。
	if f := stderrLog(sc.Name); f != nil {
		c.logf = f
		cmd.Stderr = f
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// 自建管道而非 StdoutPipe：Wait 不会在读完之前关掉它。
	stdout, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw
	err = cmd.Start()
	_ = pw.Close()
	if err != nil {
		_ = stdout.Close()
		if c.logf != nil {
			_ = c.logf.Close()
		}
		return nil, err
	}
	c.stdin = stdin
	go func() {
		_ = cmd.Wait()
		close(c.exited)
	}()
	go func() {
		c.readLoop(stdout)
		_ = stdout.Close()
	}()
	return c, nil
}

func (c *Client) readLoop(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		hasID := len(msg.ID) > 0 && string(msg.ID) != "null"
		switch {
		case msg.Method != "" && hasID:
			c.answerServerRequest(msg)
		case msg.Method != "":
			// 服务器通知（日志、进度、list_changed…）暂时忽略。
		case hasID:
			id, err := strconv.ParseInt(string(msg.ID), 10, 64)
			if err != nil {
				continue
			}
			c.mu.Lock()
			ch := c.pending[id]
			delete(c.pending, id)
			c.mu.Unlock()
			if ch != nil {
				if msg.Error != nil {
					ch <- rpcResult{err: msg.Error}
				} else {
					ch <- rpcResult{result: msg.Result}
				}
			}
		}
	}
	err := sc.Err()
	if err == nil {
		err = errors.New("服务器已退出")
	}
	c.mu.Lock()
	c.doneErr = err
	for id, ch := range c.pending {
		ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.done)
}

// answerServerRequest 回应服务器发来的请求：ping 回空结果，其余回 method not found。
func (c *Client) answerServerRequest(msg rpcMessage) {
	resp := rpcMessage{JSONRPC: "2.0", ID: msg.ID}
	if msg.Method == "ping" {
		resp.Result = json.RawMessage(`{}`)
	} else {
		resp.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	_ = c.write(resp)
}

func (c *Client) write(msg rpcMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.stdin.Write(data)
	return err
}

// Notify 发送通知（无 id）。
func (c *Client) Notify(method string, params any) error {
	msg := rpcMessage{JSONRPC: "2.0", Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		msg.Params = raw
	}
	return c.write(msg)
}

// Call 发送请求并等待响应；ctx 取消时发送 notifications/cancelled。
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	msg := rpcMessage{JSONRPC: "2.0", Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		msg.Params = raw
	}
	ch := make(chan rpcResult, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		err := c.doneErr
		c.mu.Unlock()
		return nil, err
	default:
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()
	msg.ID = json.RawMessage(strconv.FormatInt(id, 10))
	if err := c.write(msg); err != nil {
		c.forget(id)
		return nil, fmt.Errorf("写入请求失败: %w", err)
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		c.forget(id)
		_ = c.Notify("notifications/cancelled", map[string]any{"requestId": id, "reason": ctx.Err().Error()})
		return nil, ctx.Err()
	}
}

func (c *Client) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// Initialize 完成握手：initialize + notifications/initialized。
func (c *Client) Initialize(ctx context.Context) error {
	_, err := c.Call(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": appdir.Name, "version": "0.1.0"},
	})
	if err != nil {
		return err
	}
	return c.Notify("notifications/initialized", nil)
}

// ListTools 获取全部工具（自动翻页）。
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	cursor := ""
	for page := 0; page < 100; page++ {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		raw, err := c.Call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var res struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("解析 tools/list 失败: %w", err)
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			return all, nil
		}
		cursor = res.NextCursor
	}
	return all, nil
}

// CallTool 调用工具，返回原始 result。
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return c.Call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
}

// Close 关闭 stdin，等待退出；超时则向整个进程组发 SIGTERM，再不退出就 SIGKILL。
func (c *Client) Close() {
	c.closing.Do(func() {
		_ = c.stdin.Close()
		pgid := c.cmd.Process.Pid
		select {
		case <-c.exited:
		case <-time.After(500 * time.Millisecond):
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			select {
			case <-c.exited:
			case <-time.After(2 * time.Second):
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				<-c.exited
			}
		}
		// 进程组里可能还有孙进程，统一清理。
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		if c.logf != nil {
			_ = c.logf.Close()
		}
	})
}
