package quota

import (
	"bytes"
	"context"
	"sync"
)

type MemoryCredentialProvider struct {
	mu     sync.RWMutex
	token  []byte
	closed bool
}

func NewMemoryCredentialProvider(token []byte) (*MemoryCredentialProvider, error) {
	if len(bytes.TrimSpace(token)) == 0 {
		return nil, ErrCredentialUnavailable
	}
	return &MemoryCredentialProvider{token: append([]byte(nil), token...)}, nil
}

func (provider *MemoryCredentialProvider) WithAccessToken(
	ctx context.Context,
	use func([]byte) error,
) error {
	if provider == nil || use == nil {
		return ErrCredentialUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	provider.mu.RLock()
	if provider.closed || len(provider.token) == 0 {
		provider.mu.RUnlock()
		return ErrCredentialUnavailable
	}
	lease := append([]byte(nil), provider.token...)
	provider.mu.RUnlock()
	defer clearBytes(lease)
	return use(lease)
}

func (provider *MemoryCredentialProvider) Replace(token []byte) error {
	if provider == nil || len(bytes.TrimSpace(token)) == 0 {
		return ErrCredentialUnavailable
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.closed {
		return ErrCredentialUnavailable
	}
	replacement := append([]byte(nil), token...)
	clearBytes(provider.token)
	provider.token = replacement
	return nil
}

func (provider *MemoryCredentialProvider) Close() {
	if provider == nil {
		return
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	clearBytes(provider.token)
	provider.token = nil
	provider.closed = true
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
