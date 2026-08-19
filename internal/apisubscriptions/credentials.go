package apisubscriptions

import (
	"fmt"
	"sync"
)

type APIKeyProvider interface {
	APIKey(service string) ([]byte, bool)
}

type CredentialEpochProvider interface {
	CredentialEpoch(service string) (string, bool)
}

type MemoryAPIKeys struct {
	mu     sync.RWMutex
	keys   map[string][]byte
	epochs map[string]string
	closed bool
}

type MemoryAPICredential struct {
	Key   []byte
	Epoch string
}

func NewMemoryAPIKeys(keys map[string][]byte) (*MemoryAPIKeys, error) {
	credentials := make(map[string]MemoryAPICredential, len(keys))
	for service, key := range keys {
		credentials[service] = MemoryAPICredential{Key: key, Epoch: "legacy-" + service}
	}
	return NewMemoryAPICredentials(credentials)
}

func NewMemoryAPICredentials(credentials map[string]MemoryAPICredential) (*MemoryAPIKeys, error) {
	result := &MemoryAPIKeys{
		keys: make(map[string][]byte, len(credentials)), epochs: make(map[string]string, len(credentials)),
	}
	for service, credential := range credentials {
		if !validService(service) {
			result.Close()
			return nil, fmt.Errorf("unknown API subscription service %q", service)
		}
		if len(credential.Key) == 0 && credential.Epoch == "" {
			continue
		}
		if len(credential.Key) == 0 || !validCredentialEpoch(credential.Epoch) {
			result.Close()
			return nil, fmt.Errorf("invalid API subscription credential %q", service)
		}
		result.keys[service] = append([]byte(nil), credential.Key...)
		result.epochs[service] = credential.Epoch
	}
	return result, nil
}

func (keys *MemoryAPIKeys) APIKey(service string) ([]byte, bool) {
	if keys == nil {
		return nil, false
	}
	keys.mu.RLock()
	defer keys.mu.RUnlock()
	if keys.closed {
		return nil, false
	}
	key, ok := keys.keys[service]
	if !ok || len(key) == 0 {
		return nil, false
	}
	return append([]byte(nil), key...), true
}

func (keys *MemoryAPIKeys) CredentialEpoch(service string) (string, bool) {
	keys.mu.RLock()
	defer keys.mu.RUnlock()
	if keys.closed {
		return "", false
	}
	if key, ok := keys.keys[service]; !ok || len(key) == 0 {
		return "", false
	}
	epoch, ok := keys.epochs[service]
	return epoch, ok && epoch != ""
}

func (keys *MemoryAPIKeys) Close() {
	if keys == nil {
		return
	}
	keys.mu.Lock()
	defer keys.mu.Unlock()
	if keys.closed {
		return
	}
	for service, key := range keys.keys {
		clear(key)
		delete(keys.keys, service)
		delete(keys.epochs, service)
	}
	keys.closed = true
}

func validService(service string) bool {
	return service == ServiceDeepSeek || service == ServiceOpenCodeGo
}
