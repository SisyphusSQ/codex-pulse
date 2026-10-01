package requestinfo

import "context"

type requestIDKey struct{}

// WithID 将经过校验的请求标识传给下游，避免 service 依赖 HTTP 框架。
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}
func ID(ctx context.Context) string { id, _ := ctx.Value(requestIDKey{}).(string); return id }
