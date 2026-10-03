package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		select {
		case <-released:
		default:
			close(released)
		}
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
				if mismatch {
					var batch reportingv1.Batch
					if err := json.NewDecoder(req.Body).Decode(&batch); err != nil {
						w.WriteHeader(400)
						return
					}
					if len(batch.Sessions) > 0 {
						uploads.Add(1)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": reportingv1.Receipt{Version: 1, BatchID: batch.ID, ReceivedAtMS: 3000}})
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

func (f *fakeSource) FactsPartition(context.Context, string) (string, error) {
	return "quota-partition", nil
}
func (f *fakeSource) Facts(context.Context, string, string, int64) (ExportFactsPage, error) {
	return ExportFactsPage{Partition: "quota-partition", Done: true}, nil
}

type quotaSyncSource struct {
	fakeSource
	account atomic.Value
}

func (s *quotaSyncSource) Page(context.Context, string, string, int64) (ExportPage, error) {
	return ExportPage{}, store.ErrReportingSource
}
func (s *quotaSyncSource) FactsPartition(context.Context, string) (string, error) {
	return s.account.Load().(string), nil
}
func (s *quotaSyncSource) Facts(_ context.Context, provider, _ string, _ int64) (ExportFactsPage, error) {
	raw := s.account.Load().(string)
	page := ExportFactsPage{Partition: raw, Done: true}
	if provider != "codex" {
		return page, nil
	}
	q := reportingv1.QuotaObservation{Provider: provider, ID: "quota-" + raw, LocalScope: "scope-" + raw, AccountID: new(raw), LimitID: "codex", WindowKind: "primary", ObservedAtMS: 1000, UsedPercent: new(0.0), WindowMinutes: new(int64(300)), ResetsAtMS: new(int64(18000000)), Source: "app_server", Validity: "accepted", HistoryOrigin: "confirmed"}
	page.Groups = []FactsGroup{{Key: reportingv1.Key(raw), Batch: reportingv1.Batch{Accounts: []reportingv1.Account{{Provider: provider, ID: raw, CollectedAtMS: 1000}}, Bindings: []reportingv1.AccountBinding{{Provider: provider, LocalScope: q.LocalScope, AccountID: raw, ConfirmedAtMS: 1000}}, Quotas: []reportingv1.QuotaObservation{q}}}}
	return page, nil
}
func TestQuotaSyncLostReceiptKeepsAAfterAccountSwitchToB(t *testing.T) {
	state, _ := testState(t)
	source := &quotaSyncSource{}
	source.account.Store("raw-a")
	var mu sync.Mutex
	bodies := map[string][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/v1/pair" {
			testPairResponse(w)
			return
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var batch reportingv1.Batch
		if err := json.Unmarshal(body, &batch); err != nil {
			t.Error(err)
			return
		}
		first := false
		if len(batch.Quotas) > 0 {
			raw := *batch.Quotas[0].AccountID
			mu.Lock()
			bodies[raw] = append(bodies[raw], string(body))
			first = raw == "raw-a" && len(bodies[raw]) == 1
			mu.Unlock()
		}
		if first {
			source.account.Store("raw-b")
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
	if _, err := runtime.Configure(t.Context(), ConfigureRequest{Enabled: true, IntervalSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, runtime, func(s Status) bool {
		mu.Lock()
		defer mu.Unlock()
		return len(bodies["raw-a"]) >= 2 && len(bodies["raw-b"]) >= 1 && s.PendingBatches == 0
	})
	mu.Lock()
	defer mu.Unlock()
	if bodies["raw-a"][0] != bodies["raw-a"][1] {
		t.Fatal("old A queue was rewritten after switch")
	}
}

type blockedSessionSource struct{ quotaSyncSource }

func (s *blockedSessionSource) Page(_ context.Context, provider, after string, _ int64) (ExportPage, error) {
	if provider == "codex" {
		return ExportPage{}, store.ErrReportingBudget
	}
	if provider == "cursor" && after == "" {
		snap := testSnapshot()
		snap.Provider = "cursor"
		snap.SessionID = "cursor-session"
		snap.Contributions[0].ID = reportingv1.ContributionID(snap.Provider, snap.SessionID, snap.Contributions[0], 0)
		return ExportPage{Sessions: []reportingv1.SessionSnapshot{snap}, Next: snap.SessionID}, nil
	}
	return ExportPage{}, store.ErrReportingSource
}
func (s *blockedSessionSource) CurrentFacts(ctx context.Context, provider string, start int64) (ExportFactsPage, error) {
	return s.Facts(ctx, provider, "", start)
}
func TestSessionFailureDoesNotBlockCurrentQuotaAndStatus(t *testing.T) {
	state, _ := testState(t)
	source := &blockedSessionSource{}
	source.account.Store("confirmed-current")
	var mu sync.Mutex
	var received []reportingv1.Batch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var b reportingv1.Batch
		if err := json.NewDecoder(req.Body).Decode(&b); err != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		received = append(received, b)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": reportingv1.Receipt{Version: 1, BatchID: b.ID, ReceivedAtMS: 3000}})
	}))
	defer server.Close()
	runtime := &Runtime{state: state, source: source, ctx: t.Context(), version: "test"}
	cfg := credentialSettings{ID: 1, Endpoint: server.URL, ClientID: "client", Credential: strings.Repeat("c", 43), AllowHTTP: true, Enabled: true, IntervalSeconds: 60}
	if _, err := runtime.cycle(t.Context(), cfg); !errors.Is(err, store.ErrReportingBudget) {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	quota, status, otherProvider := false, false, false
	for _, b := range received {
		for _, snap := range b.Sessions {
			if snap.Provider == "cursor" {
				otherProvider = true
			}
		}
		if len(b.Quotas) > 0 && b.Quotas[0].AccountID != nil && *b.Quotas[0].AccountID == "confirmed-current" {
			quota = true
		}
		for _, st := range b.Status {
			if st.Provider == "codex" && st.SyncState == "source_budget_exceeded" && st.SyncCheckedAtMS != nil {
				status = true
			}
		}
	}
	if !quota || !status || !otherProvider || len(received[0].Quotas) == 0 {
		t.Fatal("latest quota/status starved behind session", quota, status)
	}
}

type fullTaskSource struct {
	fakeSource
	unavailable bool
}

func (s *fullTaskSource) Page(ctx context.Context, provider, after string, start int64) (ExportPage, error) {
	if s.unavailable {
		return ExportPage{}, store.ErrReportingSource
	}
	return s.fakeSource.Page(ctx, provider, after, start)
}
func (s *fullTaskSource) Status(_ context.Context, provider string, _ int64) (reportingv1.DeviceStatus, error) {
	state := "ready"
	if provider != "codex" {
		state = "disabled"
	}
	return reportingv1.DeviceStatus{Provider: provider, Status: state}, nil
}
func TestFullTaskWaitsForAvailableSourceAndReportsCompletion(t *testing.T) {
	for _, unavailable := range []bool{true, false} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			state, _ := testState(t)
			source := &fullTaskSource{unavailable: unavailable}
			var completed bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				var b reportingv1.Batch
				if err := json.NewDecoder(req.Body).Decode(&b); err != nil {
					w.WriteHeader(400)
					return
				}
				for _, st := range b.Status {
					if st.FullSyncState == "completed" {
						completed = true
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": reportingv1.Receipt{Version: 1, BatchID: b.ID, ReceivedAtMS: 3000}})
			}))
			defer server.Close()
			cfg := credentialSettings{ID: 1, Endpoint: server.URL, ClientID: "client", Credential: strings.Repeat("c", 43), AllowHTTP: true, Enabled: true, IntervalSeconds: 60}
			if err := state.saveSettings(t.Context(), cfg); err != nil {
				t.Fatal(err)
			}
			if err := state.startFullSync(t.Context(), cfg.partition()); err != nil {
				t.Fatal(err)
			}
			runtime := &Runtime{state: state, source: source, ctx: t.Context(), version: "test"}
			for range 4 {
				again, err := runtime.cycle(t.Context(), cfg)
				if unavailable {
					if !errors.Is(err, store.ErrReportingSource) {
						t.Fatal("partial source did not retain task", err)
					}
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if !again {
					break
				}
			}
			status, err := state.Status(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if unavailable && (status.FullSyncState != "running" || completed) {
				t.Fatal("incomplete task marked complete")
			}
			if !unavailable && (status.FullSyncState != "completed" || !completed) {
				t.Fatal("completion not delivered to center")
			}
		})
	}
}
