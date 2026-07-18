package singleinstance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// The desktop shutdown wait exposed to users is 15 seconds. Keep the
	// contender alive beyond that window so a fenced, draining owner can hand
	// the lock over without briefly allowing neither wake nor takeover.
	defaultNotifyTimeout = 20 * time.Second
	wakeMessage          = "wake\n"
	wakeAcknowledgement  = "ack\n"
)

var (
	ErrInvalidConfig = errors.New("invalid single instance config")
	ErrNotifyOwner   = errors.New("notify existing application instance")
)

type Config struct {
	Directory     string
	Name          string
	NotifyTimeout time.Duration
}

type Lease struct {
	lockFile    *os.File
	listener    net.Listener
	lockPath    string
	socketPath  string
	wake        chan struct{}
	done        chan struct{}
	closeOnce   sync.Once
	closeErr    error
	admissionMu sync.Mutex
	accepting   atomic.Bool
}

// DefaultConfig keeps runtime coordination files private and outside the
// database directory. No user content is written to either file.
func DefaultConfig() (Config, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve user config directory: %w", err)
	}
	return Config{
		Directory: filepath.Join(directory, "Codex Pulse", "runtime"),
		Name:      "com.sisyphussq.codexpulse",
	}, nil
}

// Acquire returns owner=false after a second process successfully wakes the
// existing owner. Only the process holding flock may remove a stale socket.
func Acquire(ctx context.Context, config Config) (*Lease, bool, error) {
	if ctx == nil || config.Directory == "" || config.Name == "" || filepath.Base(config.Name) != config.Name {
		return nil, false, ErrInvalidConfig
	}
	if config.NotifyTimeout <= 0 {
		config.NotifyTimeout = defaultNotifyTimeout
	}
	if err := os.MkdirAll(config.Directory, 0o700); err != nil {
		return nil, false, fmt.Errorf("create single instance directory: %w", err)
	}
	if err := os.Chmod(config.Directory, 0o700); err != nil {
		return nil, false, fmt.Errorf("protect single instance directory: %w", err)
	}
	lockPath := filepath.Join(config.Directory, config.Name+".lock")
	socketPath := shortSocketPath(config.Directory, config.Name)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open single instance lock: %w", err)
	}
	owned, err := acquireOrNotify(ctx, lockFile, socketPath, config.NotifyTimeout)
	if err != nil {
		_ = lockFile.Close()
		return nil, false, err
	}
	if !owned {
		_ = lockFile.Close()
		return nil, false, nil
	}
	if err := os.Chmod(lockPath, 0o600); err != nil {
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
		return nil, false, fmt.Errorf("protect single instance lock: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
		return nil, false, fmt.Errorf("remove stale single instance socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
		return nil, false, fmt.Errorf("listen for second instance: %w", err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
		return nil, false, fmt.Errorf("protect single instance socket: %w", err)
	}
	lease := &Lease{
		lockFile: lockFile, listener: listener, lockPath: lockPath, socketPath: socketPath,
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	lease.accepting.Store(true)
	go lease.serve()
	return lease, true, nil
}

func shortSocketPath(directory, name string) string {
	digest := sha256.Sum256([]byte(directory + "\x00" + name))
	return filepath.Join(os.TempDir(), "codex-pulse-"+hex.EncodeToString(digest[:12])+".sock")
}

func (lease *Lease) Wake() <-chan struct{} {
	if lease == nil {
		return nil
	}
	return lease.wake
}

func (lease *Lease) Done() <-chan struct{} {
	if lease == nil {
		return nil
	}
	return lease.done
}

// StopAcceptingWakes is the first shutdown fence. A contender that reaches the
// socket after this point receives no acknowledgement and keeps retrying flock
// until it can become the next owner.
func (lease *Lease) StopAcceptingWakes() {
	if lease != nil {
		lease.admissionMu.Lock()
		lease.accepting.Store(false)
		lease.admissionMu.Unlock()
	}
}

func (lease *Lease) Close() error {
	if lease == nil {
		return nil
	}
	lease.closeOnce.Do(func() {
		lease.admissionMu.Lock()
		lease.accepting.Store(false)
		lease.admissionMu.Unlock()
		close(lease.done)
		listenerErr := lease.listener.Close()
		if errors.Is(listenerErr, net.ErrClosed) {
			listenerErr = nil
		}
		removeSocketErr := os.Remove(lease.socketPath)
		if errors.Is(removeSocketErr, os.ErrNotExist) {
			removeSocketErr = nil
		}
		unlockErr := unix.Flock(int(lease.lockFile.Fd()), unix.LOCK_UN)
		fileErr := lease.lockFile.Close()
		// Never unlink the lock file. Removing it after unlock races a new
		// owner that may already hold the old inode and permits two owners.
		lease.closeErr = errors.Join(listenerErr, removeSocketErr, unlockErr, fileErr)
	})
	return lease.closeErr
}

func (lease *Lease) serve() {
	for {
		connection, err := lease.listener.Accept()
		if err != nil {
			select {
			case <-lease.done:
				return
			default:
				continue
			}
		}
		var buffer [len(wakeMessage)]byte
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		_, readErr := io.ReadFull(connection, buffer[:])
		lease.admissionMu.Lock()
		if readErr == nil && string(buffer[:]) == wakeMessage && lease.accepting.Load() {
			select {
			case lease.wake <- struct{}{}:
			default:
			}
			_ = connection.SetWriteDeadline(time.Now().Add(time.Second))
			_, _ = connection.Write([]byte(wakeAcknowledgement))
		}
		lease.admissionMu.Unlock()
		_ = connection.Close()
	}
}

func acquireOrNotify(ctx context.Context, lockFile *os.File, socketPath string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		lockErr := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if lockErr == nil {
			return true, nil
		}
		if !errors.Is(lockErr, unix.EWOULDBLOCK) && !errors.Is(lockErr, unix.EAGAIN) {
			return false, fmt.Errorf("acquire single instance lock: %w", lockErr)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, fmt.Errorf("%w: %v", ErrNotifyOwner, lastErr)
		}
		dialer := net.Dialer{Timeout: min(remaining, 100*time.Millisecond)}
		connection, err := dialer.DialContext(ctx, "unix", socketPath)
		if err == nil {
			_ = connection.SetWriteDeadline(time.Now().Add(min(remaining, 100*time.Millisecond)))
			_, writeErr := connection.Write([]byte(wakeMessage))
			var acknowledgement [len(wakeAcknowledgement)]byte
			_ = connection.SetReadDeadline(time.Now().Add(min(remaining, 100*time.Millisecond)))
			_, readErr := io.ReadFull(connection, acknowledgement[:])
			closeErr := connection.Close()
			if writeErr == nil && readErr == nil && string(acknowledgement[:]) == wakeAcknowledgement && closeErr == nil {
				return false, nil
			}
			lastErr = errors.Join(writeErr, readErr, closeErr)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return false, errors.Join(ErrNotifyOwner, ctx.Err())
		case <-time.After(min(remaining, 25*time.Millisecond)):
		}
	}
}
