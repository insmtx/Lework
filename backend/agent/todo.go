package agent

import "context"

// TodoReporter exposes one execution's current runtime todo state to tools.
type TodoReporter interface {
	Snapshot(ctx context.Context, items []RuntimeTodoItem) error
	Update(ctx context.Context, items []RuntimeTodoItem, merge bool) error
	List() []RuntimeTodoItem
}

type todoReporterContextKey struct{}

// ContextWithTodoReporter attaches a runtime todo reporter to a tool context.
func ContextWithTodoReporter(
	ctx context.Context,
	reporter TodoReporter,
) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, todoReporterContextKey{}, reporter)
}

// TodoReporterFrom returns the runtime todo reporter attached to a tool context.
func TodoReporterFrom(ctx context.Context) (TodoReporter, bool) {
	if ctx == nil {
		return nil, false
	}
	reporter, ok := ctx.Value(todoReporterContextKey{}).(TodoReporter)
	return reporter, ok && reporter != nil
}
