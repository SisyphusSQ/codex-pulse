package helper

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	minimumAuthTokenBytes = 32
	maximumAuthTokenBytes = 4096
)

var ErrAuthenticator = errors.New("helper authentication is unavailable")

// Authenticator 只保存 token 摘要，不保留可复用凭据。
type Authenticator struct {
	digest [sha256.Size]byte
}

func NewAuthenticator(token []byte) (*Authenticator, error) {
	defer clear(token)
	if len(token) < minimumAuthTokenBytes || len(token) > maximumAuthTokenBytes {
		return nil, ErrAuthenticator
	}
	digest := sha256.Sum256(token)
	return &Authenticator{digest: digest}, nil
}

func (authenticator *Authenticator) Authorize(ctx context.Context) error {
	if ctx == nil {
		return status.Error(codes.Unauthenticated, "authentication required")
	}
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	metadataValues, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "authentication required")
	}
	values := metadataValues.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return status.Error(codes.Unauthenticated, "authentication required")
	}
	candidate := []byte(strings.TrimPrefix(values[0], "Bearer "))
	defer clear(candidate)
	digest := sha256.Sum256(candidate)
	if subtle.ConstantTimeCompare(authenticator.digest[:], digest[:]) != 1 {
		return status.Error(codes.Unauthenticated, "authentication required")
	}
	return nil
}
