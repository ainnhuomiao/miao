package tools

import "context"

type progressKey struct{}

// ProgressFunc 接收工具运行中的实时输出（目前用于 run_command 的最新一行）。
type ProgressFunc func(line string)

// WithProgress 把实时输出回调挂到 ctx 上，工具通过 progressFrom 取用。
func WithProgress(ctx context.Context, fn ProgressFunc) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

func progressFrom(ctx context.Context) ProgressFunc {
	if fn, ok := ctx.Value(progressKey{}).(ProgressFunc); ok && fn != nil {
		return fn
	}
	return func(string) {}
}
