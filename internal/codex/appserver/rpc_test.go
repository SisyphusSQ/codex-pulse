package appserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestJSONLineRPCCallSkipsNotificationsAndMatchesResponseID(t *testing.T) {
	t.Parallel()

	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","method":"thread/status/changed","params":{"private":"ignored"}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"data":[],"nextCursor":null}}`,
	}, "\n") + "\n"
	writes := &bytes.Buffer{}
	rpc := newJSONLineRPC(nopWriteCloser{writes}, strings.NewReader(responses))
	var result threadListResult
	err := rpc.Call(context.Background(), "thread/list", threadListParams{
		Limit: 100, SortKey: "recency_at", UseStateDBOnly: true,
	}, &result)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	written := writes.String()
	for _, expected := range []string{`"jsonrpc":"2.0"`, `"id":1`, `"method":"thread/list"`, `"useStateDbOnly":true`} {
		if !strings.Contains(written, expected) {
			t.Fatalf("request %q does not contain %q", written, expected)
		}
	}
}

func TestJSONLineRPCDoesNotExposeServerErrorMessage(t *testing.T) {
	t.Parallel()

	privateMarker := "private prompt from stderr-like RPC message"
	response := `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"` + privateMarker + `"}}` + "\n"
	rpc := newJSONLineRPC(nopWriteCloser{io.Discard}, strings.NewReader(response))
	var result threadListResult
	err := rpc.Call(context.Background(), "thread/list", threadListParams{}, &result)
	if err == nil || !strings.Contains(err.Error(), "-32603") {
		t.Fatalf("Call() error = %v", err)
	}
	if strings.Contains(err.Error(), privateMarker) {
		t.Fatalf("RPC error leaked server message: %v", err)
	}
}

func TestJSONLineRPCCancellationBeforeWrite(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writes := &bytes.Buffer{}
	rpc := newJSONLineRPC(nopWriteCloser{writes}, strings.NewReader(""))
	var result threadListResult
	err := rpc.Call(ctx, "thread/list", threadListParams{}, &result)
	if !errors.Is(err, context.Canceled) || writes.Len() != 0 {
		t.Fatalf("Call() error=%v writes=%q", err, writes.String())
	}
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }
