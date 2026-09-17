package store

import (
	"context"
	"testing"

	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

func TestReconcileLightMetadataSkipsIdenticalSnapshotAndPublishesTitleChange(t *testing.T) {
	t.Parallel()

	repository := openLightRuntimeRepository(t)
	home := storelight.LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2}
	title := "初始标题"
	snapshot := storelight.LightMetadataSnapshot{
		Home: home, Generation: 1, ReadyAtMS: 1_000,
		Sessions: []storelight.LightSessionMetadata{{
			SessionID: "one", ThreadName: &title, CWD: "/workspace",
			CreatedAtMS: 100, UpdatedAtMS: 200,
		}},
	}
	changed, err := repository.ReconcileLightMetadata(context.Background(), snapshot)
	if err != nil || !changed {
		t.Fatalf("ReconcileLightMetadata(initial) = %t, %v", changed, err)
	}

	identical := snapshot
	identical.Generation = 2
	identical.ReadyAtMS = 2_000
	changed, err = repository.ReconcileLightMetadata(context.Background(), identical)
	if err != nil || changed {
		t.Fatalf("ReconcileLightMetadata(identical) = %t, %v", changed, err)
	}
	state, err := repository.LightIndexState(context.Background())
	if err != nil || state.MetadataGeneration != 1 || state.MetadataReadyAtMS == nil || *state.MetadataReadyAtMS != 1_000 {
		t.Fatalf("state after no-op = %#v, %v", state, err)
	}

	updatedTitle := "动态标题"
	updated := identical
	updated.Sessions = []storelight.LightSessionMetadata{{
		SessionID: "one", ThreadName: &updatedTitle, CWD: "/workspace",
		CreatedAtMS: 100, UpdatedAtMS: 300,
	}}
	changed, err = repository.ReconcileLightMetadata(context.Background(), updated)
	if err != nil || !changed {
		t.Fatalf("ReconcileLightMetadata(title change) = %t, %v", changed, err)
	}
	state, err = repository.LightIndexState(context.Background())
	sessions, listErr := repository.ListLightSessions(context.Background())
	if err != nil || listErr != nil || state.MetadataGeneration != 2 ||
		len(sessions) != 1 || sessions[0].ThreadName == nil || *sessions[0].ThreadName != updatedTitle {
		t.Fatalf("published state=%#v sessions=%#v errors=%v/%v", state, sessions, err, listErr)
	}
}

func TestReconcileLightMetadataKeepsUnchangedRowsWhileDeletingRemovedSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openLightRuntimeRepository(t)
	home := storelight.LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2}
	initial := storelight.LightMetadataSnapshot{
		Home: home, Generation: 1, ReadyAtMS: 1_000,
		Sessions: []storelight.LightSessionMetadata{
			{SessionID: "changed", CWD: "/before", CreatedAtMS: 100, UpdatedAtMS: 200},
			{SessionID: "unchanged", CWD: "/same", CreatedAtMS: 100, UpdatedAtMS: 200},
			{SessionID: "removed", CWD: "/gone", CreatedAtMS: 100, UpdatedAtMS: 200},
		},
	}
	if changed, err := repository.ReconcileLightMetadata(ctx, initial); err != nil || !changed {
		t.Fatalf("initial reconcile = %t, %v", changed, err)
	}
	next := storelight.LightMetadataSnapshot{
		Home: home, Generation: 2, ReadyAtMS: 2_000,
		Sessions: []storelight.LightSessionMetadata{
			{SessionID: "changed", CWD: "/after", CreatedAtMS: 100, UpdatedAtMS: 300},
			{SessionID: "unchanged", CWD: "/same", CreatedAtMS: 100, UpdatedAtMS: 200},
			{SessionID: "added", CWD: "/new", CreatedAtMS: 100, UpdatedAtMS: 300},
		},
	}
	if changed, err := repository.ReconcileLightMetadata(ctx, next); err != nil || !changed {
		t.Fatalf("changed reconcile = %t, %v", changed, err)
	}
	state, err := repository.LightIndexState(ctx)
	if err != nil || state.MetadataGeneration != 2 {
		t.Fatalf("state = %#v, %v", state, err)
	}
	sessions, err := repository.ListLightSessions(ctx)
	if err != nil || len(sessions) != 3 {
		t.Fatalf("sessions = %#v, %v", sessions, err)
	}
	byID := make(map[string]storelight.LightSessionMetadata, len(sessions))
	for _, session := range sessions {
		byID[session.SessionID] = session
	}
	if byID["unchanged"].MetadataGeneration != 1 || byID["changed"].MetadataGeneration != 2 ||
		byID["added"].MetadataGeneration != 2 || byID["changed"].CWD != "/after" {
		t.Fatalf("metadata was not published as a row diff: %#v", byID)
	}
	if _, found := byID["removed"]; found {
		t.Fatalf("removed session remained: %#v", byID["removed"])
	}
	noChange := next
	noChange.Generation = 3
	noChange.ReadyAtMS = 3_000
	if changed, err := repository.ReconcileLightMetadata(ctx, noChange); err != nil || changed {
		t.Fatalf("no-change reconcile = %t, %v", changed, err)
	}
}
