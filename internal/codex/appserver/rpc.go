package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

const maxRPCLineBytes = 32 << 20

type jsonLineRPC struct {
	writer io.WriteCloser
	reader *bufio.Scanner
	mu     sync.Mutex
	nextID int64
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
	return &jsonLineRPC{writer: writer, reader: scanner}
}

func (rpc *jsonLineRPC) Call(ctx context.Context, method string, params any, result any) error {
	if rpc == nil || rpc.writer == nil || rpc.reader == nil || method == "" || result == nil {
		return errors.New("invalid App Server RPC call")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rpc.mu.Lock()
	defer rpc.mu.Unlock()

	rpc.nextID++
	requestID := rpc.nextID
	request := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int64  `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{JSONRPC: "2.0", ID: requestID, Method: method, Params: params}
	if err := json.NewEncoder(rpc.writer).Encode(request); err != nil {
		return errors.New("write App Server RPC request")
	}

	for rpc.reader.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var response rpcResponse
		if err := json.Unmarshal(rpc.reader.Bytes(), &response); err != nil {
			return errors.New("decode App Server RPC response")
		}
		if response.ID == nil {
			continue
		}
		if *response.ID != requestID {
			return errors.New("App Server RPC response id mismatch")
		}
		if response.Error != nil {
			return fmt.Errorf("App Server RPC error %d", response.Error.Code)
		}
		if len(response.Result) == 0 {
			return errors.New("App Server RPC response missing result")
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return errors.New("decode App Server RPC result")
		}
		return nil
	}
	if err := rpc.reader.Err(); err != nil {
		return errors.New("read App Server RPC response")
	}
	return io.ErrUnexpectedEOF
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
	return rpc.writer.Close()
}
