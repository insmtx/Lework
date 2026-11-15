package authz

import "context"

// principalKey 是 context 里 Principal 的私有键类型。
//
// 用空结构体而不是字符串做键：别的包无法构造同类型的键，也就不会覆盖掉
// 中间件放进去的身份。
type principalKey struct{}

// WithPrincipal 把已认证身份放进 context，交给下游 handler 读取。
//
// 只应由鉴权中间件在 Resolve 成功之后调用；handler 不得自己伪造身份，否则
// 「谁在请求」就同时存在两份真相。
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom 读取中间件放入的身份；没有经过鉴权时为 false。
//
// 返回 false 表示「这个请求根本没有经过鉴权中间件」，调用方应拒绝而不是
// 继续当成匿名请求：控制面的所有 /api 路由都要求明确身份。
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
