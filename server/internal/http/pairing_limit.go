package http

import (
	"net"
	"sync"
	"time"
)

// pairingAttempt 是有界匿名入口的短期计数，不保存码或业务身份。
type pairingAttempt struct {
	expires time.Time
	count   int
}
type pairingLimiter struct {
	mu       sync.Mutex
	attempts map[string]pairingAttempt
}

func newPairingLimiter() *pairingLimiter {
	return &pairingLimiter{attempts: make(map[string]pairingAttempt)}
}

func (l *pairingLimiter) Allow(remote string, now time.Time) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, entry := range l.attempts {
		if !now.Before(entry.expires) {
			delete(l.attempts, key)
		}
	}
	entry, found := l.attempts[host]
	if !found {
		if len(l.attempts) >= 2048 {
			return false
		}
		entry.expires = now.Add(time.Minute)
	}
	if entry.count >= 30 {
		return false
	}
	entry.count++
	l.attempts[host] = entry
	return true
}
