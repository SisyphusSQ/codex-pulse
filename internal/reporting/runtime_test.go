package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type fakeSource struct {
	reads    atomic.Int32
	mismatch bool
}

func (f *fakeSource) Status(_ context.Context, provider string, _ int64) (reportingv1.DeviceStatus, error) {
	return reportingv1.DeviceStatus{Provider: provider, Status: "ready"}, nil
}
func (f *fakeSource) Partition(context.Context, string) (string, error) {
	return "private-home-key", nil
}
func (f *fakeSource) Page(_ context.Context, provider, after string, _ int64) (ExportPage, error) {
	f.reads.Add(1)
	if provider != "codex" {
		return ExportPage{}, store.ErrReportingSource
	}
	if after != "" {
		return ExportPage{Authority: true}, nil
	}
	snap := testSnapshot()
	if f.mismatch {
		snap.HomeID = "switched-home"
	}
	return ExportPage{Sessions: []reportingv1.SessionSnapshot{snap}, Next: snap.SessionID, Authority: true}, nil
}
func testPairResponse(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": reportingv1.CollectorPairResponse{ClientID: "00000000-0000-0000-0000-000000000001", Credential: strings.Repeat("c", 43), ProtocolVersion: 1}})
}
func waitStatus(t *testing.T, r *Runtime, want func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		out, err := r.Status(t.Context())
		if err == nil && want(out) {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
	out, _ := r.Status(t.Context())
	t.Fatalf("reporting status: %+v", out)
	return Status{}
}
func TestOptionalSyncRetriesSameCommittedBatchAndStopsWithOwner(t *testing.T) {
	state, _ := testState(t)
	source := &fakeSource{}
	var mu sync.Mutex
	var bodies []string
	ids := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/v1/pair" {
			testPairResponse(w)
			return
		}
		var batch reportingv1.Batch
		if err := json.NewDecoder(req.Body).Decode(&batch); err != nil {
			t.Error(err)
			return
		}
		bytes, _ := json.Marshal(batch)
		mu.Lock()
		bodies = append(bodies, string(bytes))
		ids[batch.ID] = true
		first := len(bodies) == 1
		mu.Unlock()
		if first {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": reportingv1.Receipt{Version: 1, BatchID: batch.ID, ReceivedAtMS: 3000}})
	}))
	defer server.Close()
	runtime := Start(state, source)
	defer runtime.Close(context.Background())
	if _, err := runtime.Pair(t.Context(), PairRequest{Endpoint: server.URL, AllowHTTP: true, Code: "AAAA-BBBB-CCCC-DDDD"}); err != nil {
		t.Fatal(err)
	}
	if source.reads.Load() != 0 {
		t.Fatal("disabled reporter read sources")
	}
	if _, err := runtime.Configure(t.Context(), ConfigureRequest{Enabled: true, IntervalSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, runtime, func(s Status) bool { return s.LastSuccessAtMS != nil && s.PendingBatches == 0 })
	mu.Lock()
	if len(ids) != 2 || len(bodies) < 2 || bodies[0] != bodies[1] {
		t.Error("lost receipt duplicated facts or changed retry body")
	}
	mu.Unlock()
	if err := runtime.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.done:
	default:
		t.Fatal("worker survives owner")
	}
	if _, err := runtime.Configure(t.Context(), ConfigureRequest{Enabled: true, IntervalSeconds: 60}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed owner accepted work")
	}
}
func TestDisableCancelsInflightUploadWithoutDroppingQueue(t *testing.T) {
	state, _ := testState(t)
	source := &fakeSource{}
	started := make(chan struct{})
	released := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/v1/pair" {
			testPairResponse(w)
			return
		}
		_, _ = io.Copy(io.Discard, req.Body)
		select {
		case <-started:
		default:
			close(started)
		}
		<-req.Context().Done()
		close(released)
	}))
	defer server.Close()
	runtime := Start(state, source)
	defer runtime.Close(context.Background())
	if _, err := runtime.Pair(t.Context(), PairRequest{Endpoint: server.URL, AllowHTTP: true, Code: "AAAA-BBBB-CCCC-DDDD"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Configure(t.Context(), ConfigureRequest{Enabled: true, IntervalSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not start")
	}
	out, err := runtime.Configure(t.Context(), ConfigureRequest{Enabled: false, IntervalSeconds: 60})
	if err != nil || out.Enabled || out.PendingBatches != 2 {
		t.Fatalf("disable: %+v %v", out, err)
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("inflight request continued")
	}
}
func TestRevocationPausesAndHomeMismatchNeverUploads(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoked", true: "Home fence"}[mismatch], func(t *testing.T) {
			state, _ := testState(t)
			source := &fakeSource{mismatch: mismatch}
			var uploads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/api/v1/pair" {
					testPairResponse(w)
					return
				}
				uploads.Add(1)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer server.Close()
			runtime := Start(state, source)
			defer runtime.Close(context.Background())
			_, err := runtime.Pair(t.Context(), PairRequest{Endpoint: server.URL, AllowHTTP: true, Code: "AAAA-BBBB-CCCC-DDDD"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = runtime.Configure(t.Context(), ConfigureRequest{Enabled: true, IntervalSeconds: 60})
			if err != nil {
				t.Fatal(err)
			}
			want := "reconnect_required"
			if mismatch {
				want = "source_unavailable"
			}
			waitStatus(t, runtime, func(s Status) bool { return s.State == want })
			for range 3 {
				_, _ = runtime.SyncNow(t.Context())
			}
			if mismatch && uploads.Load() != 0 {
				t.Fatal("mixed Home uploaded")
			}
			if !mismatch && uploads.Load() != 1 {
				t.Fatal("unauthorized credential retried")
			}
		})
	}
}

func TestHistoryRangeCannotImplicitlyRemoveCenterHistory(t *testing.T) {
	state, _ := testState(t)
	source := &fakeSource{}
	runtime := Start(state, source)
	defer runtime.Close(context.Background())
	cfg := credentialSettings{ID: 1, Endpoint: "https://center.example", ClientID: "collector", IntervalSeconds: 60, State: "disabled"}
	if err := state.saveSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := state.Enqueue(t.Context(), cfg.partition(), "first", testSnapshot()); err != nil {
		t.Fatal(err)
	}
	_, err := runtime.Configure(t.Context(), ConfigureRequest{IntervalSeconds: 60, HistoryStartAtMS: 1000, ClearPending: true})
	if !errors.Is(err, ErrSettings) {
		t.Fatal("history shrink accepted after first export")
	}
	if _, err := state.next(t.Context(), cfg.partition()); err != nil {
		t.Fatal("rejected range change deleted queue")
	}
}
