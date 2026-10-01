package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/diagnostics"
)

const (
	maxRPCLineBytes           = 32 << 20
	jsonRPCMethodNotFoundCode = -32601
)

var ErrCapabilityUnavailable = errors.New("App Server capability unavailable")
var ErrProtocolIncompatible = errors.New("App Server protocol incompatible")

type RPCError struct {
	Code int64
}

func (err RPCError) Error() string {
	return fmt.Sprintf("App Server RPC error %d", err.Code)
}

func (err RPCError) Unwrap() error {
	if err.Code == jsonRPCMethodNotFoundCode {
		return ErrCapabilityUnavailable
	}
	return nil
}

type jsonLineRPC struct {
	writer       io.WriteCloser
	reader       *bufio.Scanner
	readerCloser io.Closer
	mu           sync.Mutex
	nextID       int64
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code int64 `json:"code"`
	} `json:"error"`
}

func newJSONLineRPC(writer io.WriteCloser, reader io.Reader) *jsonLineRPC {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxRPCLineBytes)
	closer, _ := reader.(io.Closer)
	return &jsonLineRPC{writer: writer, reader: scanner, readerCloser: closer}
}

func (rpc *jsonLineRPC) Call(ctx context.Context, method string, params any, result any) (returnErr error) {
	if rpc == nil || rpc.writer == nil || rpc.reader == nil || method == "" || result == nil {
		return errors.New("invalid App Server RPC call")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rpc.mu.Lock()
	defer rpc.mu.Unlock()
	started := time.Now()
	defer func() {
		if returnErr != nil {
			event := diagnostics.FromError(returnErr, "rpc_read", "protocol_incompatible")
			if errors.Is(returnErr, context.DeadlineExceeded) {
				event.Reason = "timeout"
			}
			if errors.Is(returnErr, context.Canceled) {
				event.Reason = "cancelled"
				event.Outcome = "cancelled"
			}
			event.Method = method
			event.DurationMS = time.Since(started).Milliseconds()
			diagnostics.Emit(ctx, event)
		}
	}()
	// deadline 必须中断 Scan/Encode 的阻塞 IO，不能只等下一行后才检查 context。
	stopCancel := context.AfterFunc(ctx, func() { _ = rpc.Close() })
	defer stopCancel()

	rpc.nextID++
	requestID := rpc.nextID
	request := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int64  `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{JSONRPC: "2.0", ID: requestID, Method: method, Params: params}
	if err := json.NewEncoder(rpc.writer).Encode(request); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return diagnostics.Wrap(errors.New("write App Server RPC request"), "rpc_write", "write_failed")
	}

	for rpc.reader.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var response rpcResponse
		if err := json.Unmarshal(rpc.reader.Bytes(), &response); err != nil {
			return ErrProtocolIncompatible
		}
		if response.ID == nil {
			continue
		}
		if *response.ID != requestID {
			return ErrProtocolIncompatible
		}
		if response.Error != nil {
			return &diagnostics.Failure{Stage: "rpc_read", Reason: "rpc_error", RPCCode: new(response.Error.Code), Cause: RPCError{Code: response.Error.Code}}
		}
		if len(response.Result) == 0 {
			return ErrProtocolIncompatible
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return ErrProtocolIncompatible
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := rpc.reader.Err(); err != nil {
		return diagnostics.Wrap(errors.New("read App Server RPC response"), "rpc_read", "read_failed")
	}
	return diagnostics.Wrap(io.ErrUnexpectedEOF, "rpc_read", "unexpected_eof")
}

func (rpc *jsonLineRPC) Notify(ctx context.Context, method string, params any) error {
	if rpc == nil || rpc.writer == nil || method == "" {
		return errors.New("invalid App Server RPC notification")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rpc.mu.Lock()
	defer rpc.mu.Unlock()
	stopCancel := context.AfterFunc(ctx, func() { _ = rpc.Close() })
	defer stopCancel()
	request := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{JSONRPC: "2.0", Method: method, Params: params}
	if err := json.NewEncoder(rpc.writer).Encode(request); err != nil {
		return errors.New("write App Server RPC notification")
	}
	return nil
}

func (rpc *jsonLineRPC) Close() error {
	if rpc == nil || rpc.writer == nil {
		return nil
	}
	var readErr error
	if rpc.readerCloser != nil {
		readErr = rpc.readerCloser.Close()
	}
	return errors.Join(readErr, rpc.writer.Close())
}
