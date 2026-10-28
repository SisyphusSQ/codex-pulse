//go:build windows

package helper

import (
	"context"
	"flag"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/core"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
)

// The compiled test peer exercises production gRPC/auth/listener with bounded fake query
// results. It has no production entry point and never loads user providers or credentials.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--pipe" {
		if err := runWirePeer(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runWirePeer() error {
	flags := flag.NewFlagSet("wire-peer", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pipe := flags.String("pipe", "", "")
	authFD := flags.Uint64("auth-fd", 0, "")
	runtime := flags.String("runtime-directory", "", "")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	reader := os.NewFile(uintptr(*authFD), "wire-auth")
	defer reader.Close()
	auth, eof, err := readAuthPipe(reader)
	if err != nil {
		return err
	}
	listener, cleanup, err := listenEndpoint(RuntimeConfig{SocketPath: *pipe, RuntimeDirectory: *runtime})
	if err != nil {
		return err
	}
	defer cleanup()
	broker, err := core.NewInvalidationBroker(2)
	if err != nil {
		return err
	}
	defer broker.Close()
	business, err := core.NewService(core.ServiceConfig{UsageCost: &wireUsage{}, InvocationUsage: &helperInvocationQueryStub{}, PricingCatalog: helperPricingCatalogQueryStub{}, RuntimeInfo: helperRuntimeQueryStub{}})
	if err != nil {
		return err
	}
	lifecycle := &wireLifecycle{broker: broker, stop: make(chan struct{})}
	server, err := NewGRPCServer(ServerConfig{Authenticator: auth, HelperVersion: "dev", Service: business, Broker: broker, Lifecycle: lifecycle, Shutdown: lifecycle})
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-eof:
	case <-lifecycle.stop:
	case err := <-done:
		return err
	}
	server.Stop()
	return nil
}

type wireUsage struct{ helperUsageQueryStub }

func (*wireUsage) ListSessions(_ context.Context, request basequery.Request) (usagecost.SessionListResponse, error) {
	size := 8 * 1024 * 1024
	if request.Page.Limit == 17 {
		size = 17 * 1024 * 1024
	}
	meta, _ := basequery.NewResponseMeta(basequery.ResponsePartial, nil, []basequery.ErrorCode{basequery.ErrorPartial})
	unknownCount, _ := basequery.UnknownNumeric(basequery.NumericCount, basequery.UnknownNotComputed)
	unknownTokens, _ := basequery.UnknownNumeric(basequery.NumericTokens, basequery.UnknownNotComputed)
	unknownCost, _ := basequery.UnknownNumeric(basequery.NumericMicroUSD, basequery.UnknownNotComputed)
	unknownTime, _ := basequery.UnknownNumeric(basequery.NumericMilliseconds, basequery.UnknownNotComputed)
	zero, _ := basequery.KnownNumeric(0, basequery.NumericCount)
	total := helperUsageTotals(unknownCount, unknownTokens, unknownCost, unknownTime)
	return usagecost.SessionListResponse{Meta: meta, MatchedCount: zero, MatchedTotals: total, PageTotals: total, Items: []usagecost.SessionItem{{SessionID: strings.Repeat("s", size), DisplayTitle: "synthetic", Totals: total, LastActivityAt: unknownTime}}}, nil
}

type wireLifecycle struct {
	broker *core.InvalidationBroker
	calls  atomic.Int32
	once   sync.Once
	stop   chan struct{}
}

func (wire *wireLifecycle) NotifyLifecycle(ctx context.Context, _ string) error {
	count := 1
	if wire.calls.Add(1) > 1 {
		count = 100
	}
	for range count {
		if err := wire.broker.Notify(ctx, core.InvalidationSettings); err != nil {
			return err
		}
	}
	return nil
}
func (wire *wireLifecycle) RequestShutdown(string) bool {
	accepted := false
	wire.once.Do(func() { accepted = true; close(wire.stop) })
	return accepted
}
