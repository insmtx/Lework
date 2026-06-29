package todo

import "context"

// reporterContextKey 用于在 context 中保存本次运行的 todo reporter。
type reporterContextKey struct{}

// ContextWithReporter 将本次运行的 todo reporter 写入 ctx。
func ContextWithReporter(ctx context.Context, reporter Reporter) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, reporterContextKey{}, reporter)
}

// ReporterFrom 从 ctx 中读取本次运行的 todo reporter。
func ReporterFrom(ctx context.Context) (Reporter, bool) {
	if ctx == nil {
		return nil, false
	}
	reporter, ok := ctx.Value(reporterContextKey{}).(Reporter)
	if !ok || reporter == nil {
		return nil, false
	}
	return reporter, true
}
