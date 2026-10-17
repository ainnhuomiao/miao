package tools

import (
	"context"
	"strings"
	"testing"
)

func TestParseTodosValidation(t *testing.T) {
	bad := []string{
		`{"todos":[{"content":"  ","status":"pending"}]}`,
		`{"todos":[{"content":"a","status":"doing"}]}`,
		`{"todos":[{"content":"a","status":"in_progress"},{"content":"b","status":"in_progress"}]}`,
		`not json`,
	}
	for _, b := range bad {
		if _, err := ParseTodos([]byte(b)); err == nil {
			t.Errorf("应拒绝 %s", b)
		}
	}
	many := `{"todos":[` + strings.TrimSuffix(strings.Repeat(`{"content":"x","status":"pending"},`, MaxTodos+1), ",") + `]}`
	if _, err := ParseTodos([]byte(many)); err == nil {
		t.Error("超过上限应拒绝")
	}
}

func TestTodoWriteUpdatesStore(t *testing.T) {
	s := NewTodoStore()
	var got []TodoItem
	s.OnChange(func(it []TodoItem) { got = it })
	tool := NewTodoWriteTool(s)
	res := tool.Run(context.Background(), []byte(`{"todos":[{"content":"读代码","status":"completed"},{"content":"写测试","status":"in_progress"},{"content":"提交","status":"pending"}]}`))
	if res.IsError {
		t.Fatal(res.Content)
	}
	if !strings.Contains(res.Content, "共 3 项") || !strings.Contains(res.Content, "已完成 1") {
		t.Fatalf("摘要不对：%s", res.Content)
	}
	if len(s.List()) != 3 || len(got) != 3 || got[1].Content != "写测试" {
		t.Fatalf("store 未更新：%v", s.List())
	}
	if res := tool.Run(context.Background(), []byte(`{"todos":[{"content":"","status":"pending"}]}`)); !res.IsError {
		t.Fatal("空 content 应报错")
	}
	if len(s.List()) != 3 {
		t.Fatal("失败调用不应改动 store")
	}
}

func TestTodoWriteRegistered(t *testing.T) {
	r, _ := NewRegistry(t.TempDir())
	RegisterBuiltins(r)
	found := false
	for _, d := range r.Definitions() {
		if d.Function.Name == "todo_write" {
			found = true
		}
	}
	if !found {
		t.Fatal("todo_write 未注册")
	}
}
