package store

import (
	"context"
	"testing"
)

func TestReconcileLightMetadataSkipsIdenticalSnapshotAndPublishesTitleChange(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	home := LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2}
	title := "初始标题"
	snapshot := LightMetadataSnapshot{
		Home: home, Generation: 1, ReadyAtMS: 1_000,
		Sessions: []LightSessionMetadata{{
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
	updated.Sessions = []LightSessionMetadata{{
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
