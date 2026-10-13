package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

func TestPendingSessionPreservesBodyAndDoesNotBlockQuotaOrAnotherSession(t *testing.T) {
	state, _ := testState(t)
	source := &blockedSessionSource{}
	source.account.Store("account-a")
	var received []reportingv1.Batch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var batch reportingv1.Batch
		if err := json.NewDecoder(req.Body).Decode(&batch); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		received = append(received, batch)
		if len(batch.Sessions) > 0 && batch.Sessions[0].SessionID == "session" {
			w.Header().Set("X-Pulse-Publication", "pending")
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": reportingv1.Receipt{Version: 1, BatchID: batch.ID, ReceivedAtMS: 3000}})
	}))
	defer server.Close()
	cfg := credentialSettings{ID: 1, Endpoint: server.URL, ClientID: "client", Credential: strings.Repeat("c", 43), AllowHTTP: true, Enabled: true, IntervalSeconds: 60}
	old := testSnapshot()
	old.SessionID = "session"
	old.Contributions[0].ID = reportingv1.ContributionID(old.Provider, old.SessionID, old.Contributions[0], 0)
	if err := state.Enqueue(t.Context(), cfg.partition(), "sweep", old); err != nil {
		t.Fatal(err)
	}
	queuedBefore, err := state.next(t.Context(), cfg.partition())
	if err != nil {
		t.Fatal(err)
	}
	r := &Runtime{state: state, source: source, ctx: t.Context(), version: "test"}
	if _, err := r.cycle(t.Context(), cfg); !errors.Is(err, ErrProcessing) {
		t.Fatal("pending session state", err)
	}
	if len(received) == 0 || len(received[0].Quotas) == 0 {
		t.Fatal("quota did not precede stuck session")
	}
	other := false
	for _, b := range received {
		for _, s := range b.Sessions {
			if s.Provider == "cursor" {
				other = true
			}
		}
	}
	if !other {
		t.Fatal("another session was blocked")
	}
	source.account.Store("account-b")
	if _, err := r.cycle(t.Context(), cfg); !errors.Is(err, ErrProcessing) {
		t.Fatal("persisted pending status lost", err)
	}
	foundB := false
	for _, b := range received {
		for _, q := range b.Quotas {
			if q.AccountID != nil && *q.AccountID == "account-b" {
				foundB = true
			}
		}
	}
	if !foundB {
		t.Fatal("next quota was blocked by retry deadline")
	}
	var after queued
	if err := state.db.View(t.Context(), func(_ context.Context, db *gorm.DB) error {
		return db.Where("batch_id = ?", queuedBefore.BatchID).Take(&after).Error
	}); err != nil {
		t.Fatal(err)
	}
	if string(after.Body) != string(queuedBefore.Body) || after.Revision != queuedBefore.Revision {
		t.Fatal("immutable backlog rewritten")
	}
}

func TestCurrentFactsHaveReservedCapacityWithLegacyFullQueue(t *testing.T) {
	state, _ := testState(t)
	if err := state.db.Write(t.Context(), func(_ context.Context, db *gorm.DB) error {
		return db.Exec("WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<2048) INSERT INTO reporting_outbox(partition,batch_id,source_key,revision,body,priority) SELECT 'center',printf('old-%d',x),'retained',1,X'7B7D',2 FROM n").Error
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot()
	if err := state.Enqueue(t.Context(), "center", "sweep", snapshot); !errors.Is(err, ErrQueueFull) {
		t.Fatal("history consumed reserve", err)
	}
	group := FactsGroup{Key: reportingv1.Key("current-status"), Batch: reportingv1.Batch{Status: []reportingv1.DeviceStatus{{Provider: "codex", Status: "ready"}}}}
	if _, err := state.EnqueueFactGroups(t.Context(), "center", []FactsGroup{group}, -2); err != nil {
		t.Fatal("legacy backlog blocked current lane", err)
	}
	status, err := state.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status.RetainedBatches != 2049 {
		t.Fatal("old batches discarded", status)
	}
}
