package helper

import (
	"bytes"
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// 测试 NewAuthenticator 清除原 token，并只接受匹配的 Bearer metadata。
func TestAuthenticatorClearsTokenAndAuthorizesMatchingMetadata(t *testing.T) {
	token := bytes.Repeat([]byte("a"), 32)
	authenticator, err := NewAuthenticator(token)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}
	if !bytes.Equal(token, make([]byte, 32)) {
		t.Fatal("NewAuthenticator retained the caller token bytes")
	}
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(
		"authorization", "Bearer "+string(bytes.Repeat([]byte("a"), 32)),
	))
	if err := authenticator.Authorize(ctx); err != nil {
		t.Fatalf("Authorize(valid) error = %v", err)
	}
}

// 测试 Authenticator 对缺失、错误和取消请求返回有限 gRPC code。
func TestAuthenticatorRejectsMissingWrongAndCancelledCredentials(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  func() context.Context
		code codes.Code
	}{
		{name: "missing", ctx: func() context.Context { return t.Context() }, code: codes.Unauthenticated},
		{name: "wrong", ctx: func() context.Context {
			return metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer wrong"))
		}, code: codes.Unauthenticated},
		{name: "cancelled", ctx: func() context.Context {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			return ctx
		}, code: codes.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			authenticator, err := NewAuthenticator(bytes.Repeat([]byte("b"), 32))
			if err != nil {
				t.Fatal(err)
			}
			if err := authenticator.Authorize(test.ctx()); status.Code(err) != test.code {
				t.Fatalf("Authorize() error = %v, want code %v", err, test.code)
			}
		})
	}
}
