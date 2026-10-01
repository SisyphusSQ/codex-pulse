// Package diagnostics 保存不含上游正文的本机刷新诊断；记录失败不改变业务结果。
package diagnostics

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

const logName = "refresh.jsonl"
const faultName = "refresh-faults.json"
const retention = 24 * time.Hour

var ErrPrivateLog = errors.New("private refresh diagnostics unavailable")
var versionPattern = regexp.MustCompile(`^(dev|development|v?[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9.-]{1,32})?)$`)
var tracePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Event 只接受有限分类和计数；原始 request 标识在 context 中变为不可逆关联摘要。
type Event struct {
	AtMS         int64  `json:"at_ms"`
	Version      string `json:"version,omitempty"`
	CodexVersion string `json:"codex_version,omitempty"`
	Trace        string `json:"trace,omitempty"`
	Source       string `json:"source"`
	Trigger      string `json:"trigger"`
	Stage        string `json:"stage"`
	Method       string `json:"method,omitempty"`
	Outcome      string `json:"outcome"`
	Reason       string `json:"reason,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitzero"`
	Attempt      int    `json:"attempt,omitzero"`
	RPCCode      *int64 `json:"rpc_code,omitempty"`
	ExitCode     *int   `json:"exit_code,omitempty"`
	NextDueAtMS  *int64 `json:"next_due_at_ms,omitempty"`
}

// Fault 保留首次异常，避免后续“刷新器不可用”覆盖真正的起点。
type Fault struct {
	First  Event `json:"first"`
	Latest Event `json:"latest"`
	Count  int64 `json:"count"`
}

type requestContext struct{ trace, source, trigger string }
type loggerKey struct{}
type requestKey struct{}

// Logger 由 Helper 拥有，与 SQLite 独立；固定四份日志总容量默认不超过 20 MiB。
type Logger struct {
	mu            sync.Mutex
	root          *os.Root
	file          *os.File
	version       string
	maxBytes      int64
	fileStartedAt time.Time
	faults        map[string]Fault
	closed        bool
	dropped       atomic.Uint64
}

// Open 只在调用方已经建立的私有 runtime 中创建固定的 logs 子目录。
func Open(runtimeDirectory, version string) (*Logger, error) {
	runtimeRoot, err := os.OpenRoot(runtimeDirectory)
	if err != nil {
		return nil, ErrPrivateLog
	}
	defer runtimeRoot.Close()
	info, err := runtimeRoot.Stat(".")
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		return nil, ErrPrivateLog
	}
	if err := runtimeRoot.Mkdir("logs", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, ErrPrivateLog
	}
	info, err = runtimeRoot.Lstat("logs")
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return nil, ErrPrivateLog
	}
	root, err := runtimeRoot.OpenRoot("logs")
	if err != nil {
		return nil, ErrPrivateLog
	}
	logger := &Logger{root: root, version: version, maxBytes: 5 << 20, faults: make(map[string]Fault)}
	_ = root.Remove(".refresh-faults.tmp")
	if !versionPattern.MatchString(version) {
		logger.version = "unknown"
	}
	logger.prune()
	file, err := logger.openFile(logName, os.O_CREATE|os.O_WRONLY|os.O_APPEND)
	if err != nil {
		root.Close()
		return nil, ErrPrivateLog
	}
	logger.file = file
	logger.fileStartedAt = logger.firstEventTime(logName)
	if saved, err := logger.openFile(faultName, os.O_RDONLY); err == nil {
		var faults map[string]Fault
		if json.NewDecoder(io.LimitReader(saved, 16<<10)).Decode(&faults) == nil {
			for _, source := range []string{"quota", "reset_credits", "runtime"} {
				if fault, ok := faults[source]; ok {
					fault.First = safeEvent(fault.First)
					fault.Latest = safeEvent(fault.Latest)
					fault.Count = max(1, fault.Count)
					logger.faults[source] = fault
				}
			}
		}
		saved.Close()
	}
	return logger, nil
}

func (logger *Logger) openFile(name string, flags int) (*os.File, error) {
	file, err := logger.root.OpenFile(name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, ErrPrivateLog
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		file.Close()
		return nil, ErrPrivateLog
	}
	var stat unix.Stat_t
	if unix.Fstat(int(file.Fd()), &stat) != nil || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
		file.Close()
		return nil, ErrPrivateLog
	}
	return file, nil
}

func (logger *Logger) firstEventTime(name string) time.Time {
	file, err := logger.openFile(name, os.O_RDONLY)
	if err != nil {
		return time.Now()
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, 4096))
	var event Event
	if scanner.Scan() && json.Unmarshal(scanner.Bytes(), &event) == nil && event.AtMS > 0 {
		return time.UnixMilli(event.AtMS)
	}
	return time.Now()
}

func (logger *Logger) prune() {
	for _, name := range []string{logName, logName + ".1", logName + ".2", logName + ".3"} {
		if info, err := logger.root.Lstat(name); err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > retention {
			_ = logger.root.Remove(name)
		}
	}
}

// WithLogger 将 Helper 管理的 sink 附加到已有 operation context。
func WithLogger(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// InheritLogger 将 runtime 拥有的 sink 带入独立 RPC context，仍保留请求的取消与 deadline。
func InheritLogger(ctx, parent context.Context) context.Context {
	logger, _ := parent.Value(loggerKey{}).(*Logger)
	if logger == nil {
		return ctx
	}
	return WithLogger(ctx, logger)
}

// WithRequest 只保留关联摘要、来源及 trigger；不持久化原始业务标识。
func WithRequest(ctx context.Context, requestID, source, trigger string) context.Context {
	digest := sha256.Sum256([]byte("refresh-diagnostic\x00" + requestID))
	return context.WithValue(ctx, requestKey{}, requestContext{hex.EncodeToString(digest[:16]), source, trigger})
}

// Emit 是 best-effort 诊断入口；不得将记录失败升级为业务刷新失败。
func Emit(ctx context.Context, event Event) {
	logger, _ := ctx.Value(loggerKey{}).(*Logger)
	if logger == nil {
		return
	}
	if event.Source == "" {
		event.Source = "runtime"
	}
	if event.Trigger == "" {
		event.Trigger = "startup"
	}
	if request, ok := ctx.Value(requestKey{}).(requestContext); ok {
		event.Trace = request.trace
		event.Source = request.source
		event.Trigger = request.trigger
	}
	event.AtMS = time.Now().UnixMilli()
	event.Version = logger.version
	logger.record(cloneEvent(safeEvent(event)))
}

// DroppedInContext 提供诊断缺失计数，不暴露文件路径或原始错误。
func DroppedInContext(ctx context.Context) uint64 {
	logger, _ := ctx.Value(loggerKey{}).(*Logger)
	if logger == nil {
		return 1
	}
	return logger.Dropped()
}

func safeEvent(event Event) Event {
	event.Source = allowed(event.Source, []string{"quota", "reset_credits", "runtime"})
	event.Trigger = allowed(event.Trigger, []string{"scheduled", "startup", "foreground", "wake", "manual", "recovery", "shutdown"})
	event.Stage = allowed(event.Stage, []string{"refresh", "cli_resolve", "cli_launch", "stderr_hint", "initialize", "rpc_write", "rpc_read", "quota_decode", "reset_decode", "reset_snapshot", "persist_attempt", "complete_claim", "recover_claim", "runner", "shutdown"})
	event.Outcome = allowed(event.Outcome, []string{"started", "succeeded", "failed", "cancelled", "skipped", "recovered", "stopped"})
	if event.Method != "" {
		event.Method = allowed(event.Method, []string{"initialize", "account/rateLimits/read", "account/read"})
	}
	if event.Reason != "" {
		event.Reason = allowed(event.Reason, []string{"timeout", "network_unavailable", "schema_incompatible", "account_identity_unavailable", "protocol_incompatible", "rpc_error", "cli_unavailable", "node_unavailable", "launch_failed", "unexpected_eof", "read_failed", "write_failed", "invalid_reset_snapshot", "store_busy", "store_io", "store_queue_full", "store_full", "store_read_only", "store_permission", "store_corrupt", "store_closed", "invalid_record", "claim_recovery", "worker_panic", "internal_error", "cancelled", "auth_required", "retry_after", "server_error", "http_429", "expired_available_credit", "invalid_credit_fields", "invalid_credit_count", "missing_credits", "invalid_quota_fields", "invalid_reset_fields", "detail_state_invalid", "ok"})
	}
	if !tracePattern.MatchString(event.Trace) {
		event.Trace = ""
	}
	if !versionPattern.MatchString(event.Version) {
		event.Version = "unknown"
	}
	if event.CodexVersion != "" && !versionPattern.MatchString(event.CodexVersion) {
		event.CodexVersion = "unknown"
	}
	event.DurationMS = max(0, event.DurationMS)
	event.Attempt = max(0, min(3, event.Attempt))
	return event
}

func allowed(value string, values []string) string {
	if slices.Contains(values, value) {
		return value
	}
	return "unknown"
}

func (logger *Logger) record(event Event) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		logger.dropped.Add(1)
		return
	}
	content, err := json.Marshal(event)
	if err != nil {
		logger.dropped.Add(1)
		return
	}
	content = append(content, '\n')
	if logger.file == nil {
		logger.file, err = logger.openFile(logName, os.O_CREATE|os.O_WRONLY|os.O_APPEND)
		if err == nil {
			logger.fileStartedAt = logger.firstEventTime(logName)
		}
	}
	var info os.FileInfo
	if err == nil {
		info, err = logger.file.Stat()
	}
	if err == nil && (info.Size()+int64(len(content)) > logger.maxBytes || time.Since(logger.fileStartedAt) > retention) {
		err = logger.rotate()
	}
	if err == nil {
		_, err = logger.file.Write(content)
	}
	if err != nil {
		logger.dropped.Add(1)
	}
	if event.Outcome == "failed" || event.Outcome == "succeeded" || event.Outcome == "recovered" {
		if event.Outcome == "failed" {
			fault, ok := logger.faults[event.Source]
			if !ok {
				fault.First = event
			}
			fault.Latest = event
			fault.Count++
			logger.faults[event.Source] = fault
		} else if event.Stage == "refresh" || (event.Stage == "runner" && event.Outcome == "recovered") {
			delete(logger.faults, event.Source)
		} else {
			return
		}
		if err := logger.saveFaults(); err != nil {
			logger.dropped.Add(1)
		}
	}
}

func (logger *Logger) rotate() error {
	if err := logger.file.Close(); err != nil {
		return err
	}
	logger.file = nil
	logger.prune()
	_ = logger.root.Remove(logName + ".3")
	for _, pair := range [][2]string{{logName + ".2", logName + ".3"}, {logName + ".1", logName + ".2"}, {logName, logName + ".1"}} {
		if err := logger.root.Rename(pair[0], pair[1]); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrPrivateLog
		}
	}
	file, err := logger.openFile(logName, os.O_CREATE|os.O_WRONLY|os.O_APPEND)
	logger.file = file
	logger.fileStartedAt = time.Now()
	return err
}

func (logger *Logger) saveFaults() error {
	const temporary = ".refresh-faults.tmp"
	file, err := logger.openFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer logger.root.Remove(temporary)
	err = json.NewEncoder(file).Encode(logger.faults)
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return ErrPrivateLog
	}
	if err := logger.root.Rename(temporary, faultName); err != nil {
		return ErrPrivateLog
	}
	return nil
}

// Faults 返回首次/最新异常的副本，调用方不能改写 logger 状态。
func (logger *Logger) Faults() map[string]Fault {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	result := make(map[string]Fault, len(logger.faults))
	for key, value := range logger.faults {
		value.First = cloneEvent(value.First)
		value.Latest = cloneEvent(value.Latest)
		result[key] = value
	}
	return result
}

func cloneEvent(event Event) Event {
	if event.RPCCode != nil {
		event.RPCCode = new(*event.RPCCode)
	}
	if event.ExitCode != nil {
		event.ExitCode = new(*event.ExitCode)
	}
	if event.NextDueAtMS != nil {
		event.NextDueAtMS = new(*event.NextDueAtMS)
	}
	return event
}

// Dropped 是本进程因日志不可写而丢弃的诊断数。
func (logger *Logger) Dropped() uint64 { return logger.dropped.Load() }

// Close 在 Helper 全部刷新工作退出后关闭 sink。
func (logger *Logger) Close() error {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return nil
	}
	logger.closed = true
	var err error
	if logger.file != nil {
		err = logger.file.Close()
		logger.file = nil
	}
	return errors.Join(err, logger.root.Close())
}
