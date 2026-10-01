package diagnostics

import (
	"errors"
	"testing"
)

func TestRefreshFailureKeepsOriginalRPCBoundary(t *testing.T) {
	cause := errors.New("secret-upstream-body")
	err := Wrap(&Failure{Stage: "rpc_read", Reason: "rpc_error", RPCCode: new(int64(-32603)), Cause: cause}, "initialize", "internal_error")
	event := FromError(err, "runner", "internal_error")
	if event.Stage != "rpc_read" || event.Reason != "rpc_error" || event.RPCCode == nil || *event.RPCCode != -32603 || !errors.Is(err, cause) {
		t.Fatalf("origin lost: %#v", event)
	}
	if err.Error() != "App Server RPC error -32603" {
		t.Fatal("raw error exposed or public RPC text changed")
	}
}
