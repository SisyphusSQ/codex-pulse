package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestIngesterRestartsFromCommittedOffsetAndReplaysHalfLineOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, err := New(repository)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	home, path := newSyntheticCodexHome(t)
	meta := rolloutSessionMetaLine("session-a")
	start := rolloutTurnStartLine("turn-a")
	split := len(start) / 2
	initialContent := []byte(meta + "\n" + start[:split])
	writeSyntheticRollout(t, path, initialContent, time.Unix(10, 0))
	discoverer, err := logs.NewDiscoverer(home)
	if err != nil {
		t.Fatalf("logs.NewDiscoverer() error = %v", err)
	}
	initialDiscovery, err := discoverer.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	initialPlan, err := logs.PlanReconcile(home, nil, initialDiscovery)
	if err != nil || len(initialPlan.Actions) != 1 || initialPlan.Actions[0].Kind != logs.ChangeAdded {
		t.Fatalf("PlanReconcile(initial) = %#v, %v", initialPlan, err)
	}
	stream, err := ingester.Open(ctx, OpenRequest{Action: initialPlan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open(initial) error = %v", err)
	}
	openedCursor, err := stream.Cursor()
	if err != nil || openedCursor.SourceFileID != initialPlan.Actions[0].Current.SourceFileID ||
		openedCursor.CommittedOffset != 0 {
		t.Fatalf("Cursor(opened) = %#v, %v", openedCursor, err)
	}
	first, err := stream.Feed(ctx, initialContent, true, 20)
	if err != nil {
		t.Fatalf("Feed(initial) error = %v", err)
	}
	wantOffset := int64(len(meta) + 1)
	if !first.Committed || first.CommittableOffset != wantOffset || first.BufferedBytes != split {
		t.Fatalf("Feed(initial) = %#v, want committed offset %d with %d buffered", first, wantOffset, split)
	}
	committedCursor, err := stream.Cursor()
	if err != nil || committedCursor.CommittedOffset != wantOffset ||
		committedCursor.Generation != first.Cursor.Generation {
		t.Fatalf("Cursor(committed) = %#v, %v", committedCursor, err)
	}
	file, err := repository.SourceFile(ctx, initialPlan.Actions[0].Current.SourceFileID)
	if err != nil || file.ParsedOffset != wantOffset {
		t.Fatalf("SourceFile(initial) = %#v, %v, want offset %d", file, err, wantOffset)
	}

	fullContent := []byte(meta + "\n" + start + "\n")
	writeSyntheticRollout(t, path, fullContent, time.Unix(20, 0))
	nextDiscovery, err := discoverer.DiscoverAgainst(ctx, initialDiscovery.Snapshots)
	if err != nil {
		t.Fatalf("DiscoverAgainst() error = %v", err)
	}
	nextPlan, err := logs.PlanReconcile(home, initialDiscovery.Snapshots, nextDiscovery)
	if err != nil || len(nextPlan.Actions) != 1 || nextPlan.Actions[0].Kind != logs.ChangeGrown {
		t.Fatalf("PlanReconcile(grown) = %#v, %v", nextPlan, err)
	}
	stream, err = ingester.Open(ctx, OpenRequest{Action: nextPlan.Actions[0], AtMS: 30})
	if err != nil {
		t.Fatalf("Open(grown) error = %v", err)
	}
	remaining := fullContent[wantOffset:]
	second, err := stream.Feed(ctx, remaining, true, 40)
	if err != nil {
		t.Fatalf("Feed(grown) error = %v", err)
	}
	if !second.Committed || second.CommittableOffset != int64(len(fullContent)) || second.BufferedBytes != 0 {
		t.Fatalf("Feed(grown) = %#v, want full committed file", second)
	}
	turns, err := repository.ListTurns(ctx, store.TurnFilter{SessionID: stringPointer("session-a")})
	if err != nil || len(turns) != 1 || turns[0].TurnID != CanonicalTurnID("session-a", "turn-a") ||
		turns[0].StartOffset != wantOffset {
		t.Fatalf("ListTurns() = %#v, %v, want one replayed half-line turn", turns, err)
	}
}

func TestIngesterSupersedesGrowingBuildingAndRejectsStaleStream(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, _ := New(repository)
	home, path := newSyntheticCodexHome(t)
	initialContent := []byte(rolloutSessionMetaLine("session-a") + "\n")
	writeSyntheticRollout(t, path, initialContent, time.Unix(10, 0))
	discoverer, _ := logs.NewDiscoverer(home)
	initialDiscovery, err := discoverer.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover(initial building) error = %v", err)
	}
	initialPlan, _ := logs.PlanReconcile(home, nil, initialDiscovery)
	staleStream, err := ingester.Open(ctx, OpenRequest{Action: initialPlan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open(initial building) error = %v", err)
	}
	staged, err := staleStream.Feed(ctx, initialContent, false, 20)
	if err != nil || !staged.Committed || staged.Cursor.State != store.GenerationBuilding {
		t.Fatalf("Feed(initial building) = %#v, %v", staged, err)
	}

	fullContent := completeRollout("session-a", "turn-a")
	writeSyntheticRollout(t, path, fullContent, time.Unix(30, 0))
	durablePrevious, err := repository.CodexSnapshots(ctx)
	if err != nil || len(durablePrevious) != 0 {
		t.Fatalf("CodexSnapshots(initial building) = %#v, %v, want no active snapshots", durablePrevious, err)
	}
	nextDiscovery, err := discoverer.DiscoverAgainst(ctx, snapshotsFromStore(durablePrevious))
	if err != nil {
		t.Fatalf("DiscoverAgainst(grown building) error = %v", err)
	}
	nextPlan, err := logs.PlanReconcile(home, snapshotsFromStore(durablePrevious), nextDiscovery)
	if err != nil || len(nextPlan.Actions) != 1 || nextPlan.Actions[0].Kind != logs.ChangeAdded {
		t.Fatalf("PlanReconcile(durable grown building) = %#v, %v", nextPlan, err)
	}
	restarted, err := ingester.Open(ctx, OpenRequest{Action: nextPlan.Actions[0], AtMS: 40})
	if err != nil {
		t.Fatalf("Open(grown building) error = %v", err)
	}
	if restarted.cursor.Generation != 1 || restarted.cursor.State != store.GenerationBuilding ||
		restarted.cursor.Checkpoint.CommittedOffset != 0 {
		t.Fatalf("Open(grown building) cursor = %#v, want restarted generation 1", restarted.cursor)
	}
	if _, err := staleStream.Feed(ctx, nil, true, 41); !errors.Is(err, store.ErrInvalidRecord) {
		t.Fatalf("Feed(stale building stream) error = %v, want ErrInvalidRecord", err)
	}
	result, err := restarted.Feed(ctx, fullContent, true, 50)
	if err != nil || !result.Committed || result.Cursor.State != store.GenerationActive {
		t.Fatalf("Feed(restarted building) = %#v, %v, want active", result, err)
	}
	if turn, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-a")); err != nil || turn.SourceGeneration != 1 {
		t.Fatalf("Turn(restarted building) = %#v, %v, want generation 1", turn, err)
	}
}

func TestIngesterResumesExactDurableSiblingAndSupersedesCompetitor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, _ := New(repository)
	home, path := newSyntheticCodexHome(t)
	content := completeRollout("session-a", "turn-a")
	writeSyntheticRollout(t, path, content, time.Unix(10, 0))
	discoverer, _ := logs.NewDiscoverer(home)
	discoveryB, err := discoverer.Discover(ctx)
	if err != nil || len(discoveryB.Snapshots) != 1 {
		t.Fatalf("Discover(B) = %#v, %v", discoveryB, err)
	}
	snapshotB := discoveryB.Snapshots[0]
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove B: %v", err)
	}
	writeSyntheticRollout(t, path, content, time.Unix(20, 0))
	discoveryC, err := discoverer.Discover(ctx)
	if err != nil || len(discoveryC.Snapshots) != 1 {
		t.Fatalf("Discover(C) = %#v, %v", discoveryC, err)
	}
	snapshotC := discoveryC.Snapshots[0]
	if snapshotB.SourceFileID == snapshotC.SourceFileID {
		t.Fatal("replacement fixtures share a source ID")
	}
	fingerprintB := sourceFingerprintFromSnapshot(snapshotB)
	fingerprintC := sourceFingerprintFromSnapshot(snapshotC)
	buildingB, err := repository.PrepareGeneration(ctx, store.PrepareGenerationRequest{
		Mode: store.GenerationModeRebuild, Current: fingerprintB,
		ParserVersion: logs.ParserVersion, AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(B) error = %v", err)
	}
	buildingC, err := repository.PrepareGeneration(ctx, store.PrepareGenerationRequest{
		Mode: store.GenerationModeRebuild, Current: fingerprintC,
		ParserVersion: logs.ParserVersion, AtMS: 31,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(C) error = %v", err)
	}

	stream, err := ingester.Open(ctx, OpenRequest{
		Action: logs.ReconcileAction{Kind: logs.ChangeAdded, Current: &snapshotB}, AtMS: 40,
	})
	if err != nil {
		t.Fatalf("Open(exact durable B) error = %v", err)
	}
	if stream.cursor.SourceFileID != buildingB.SourceFileID || stream.cursor.Generation != buildingB.Generation {
		t.Fatalf("Open(exact durable B) cursor = %#v", stream.cursor)
	}
	result, err := stream.Feed(ctx, content, true, 50)
	if err != nil || result.Cursor.State != store.GenerationActive {
		t.Fatalf("Feed(exact durable B) = %#v, %v", result, err)
	}
	competitor, err := repository.GenerationCursor(ctx, buildingC.SourceFileID, buildingC.Generation)
	if err != nil || competitor.State != store.GenerationSuperseded {
		t.Fatalf("GenerationCursor(C) = %#v, %v, want superseded", competitor, err)
	}
	fileC, err := repository.SourceFile(ctx, fingerprintC.SourceFileID)
	if err != nil || fileC.State != store.SourceFileUnavailable {
		t.Fatalf("SourceFile(C) = %#v, %v, want unavailable", fileC, err)
	}
}

func TestIngesterSupersedesPhysicalReplacementChainFromDurableBase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, _ := New(repository)
	home, path := newSyntheticCodexHome(t)
	discoverer, _ := logs.NewDiscoverer(home)
	activeContent := completeRollout("session-a", "turn-a")
	writeSyntheticRollout(t, path, activeContent, time.Unix(10, 0))
	activeDiscovery, _ := discoverer.Discover(ctx)
	activePlan, _ := logs.PlanReconcile(home, nil, activeDiscovery)
	activeStream, err := ingester.Open(ctx, OpenRequest{Action: activePlan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open(active A) error = %v", err)
	}
	if _, err := activeStream.Feed(ctx, activeContent, true, 20); err != nil {
		t.Fatalf("Feed(active A) error = %v", err)
	}
	activeSourceID := activePlan.Actions[0].Current.SourceFileID

	buildingContent := []byte(rolloutSessionMetaLine("session-a") + "\n")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove active A: %v", err)
	}
	writeSyntheticRollout(t, path, buildingContent, time.Unix(30, 0))
	durableBase, err := repository.CodexSnapshots(ctx)
	if err != nil || len(durableBase) != 1 {
		t.Fatalf("CodexSnapshots(active A) = %#v, %v", durableBase, err)
	}
	buildingDiscovery, _ := discoverer.DiscoverAgainst(ctx, snapshotsFromStore(durableBase))
	buildingPlan, err := logs.PlanReconcile(home, snapshotsFromStore(durableBase), buildingDiscovery)
	if err != nil || len(buildingPlan.Actions) != 1 || buildingPlan.Actions[0].Kind != logs.ChangeReplaced {
		t.Fatalf("PlanReconcile(building B) = %#v, %v", buildingPlan, err)
	}
	buildingStream, err := ingester.Open(ctx, OpenRequest{Action: buildingPlan.Actions[0], AtMS: 40})
	if err != nil {
		t.Fatalf("Open(building B) error = %v", err)
	}
	staged, err := buildingStream.Feed(ctx, buildingContent, false, 50)
	if err != nil || !staged.Committed || staged.Cursor.State != store.GenerationBuilding {
		t.Fatalf("Feed(building B) = %#v, %v", staged, err)
	}
	buildingSourceID := buildingPlan.Actions[0].Current.SourceFileID

	replacementContent := completeRollout("session-a", "turn-c")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove building B: %v", err)
	}
	writeSyntheticRollout(t, path, replacementContent, time.Unix(60, 0))
	replacementDiscovery, _ := discoverer.DiscoverAgainst(ctx, snapshotsFromStore(durableBase))
	replacementPlan, err := logs.PlanReconcile(home, snapshotsFromStore(durableBase), replacementDiscovery)
	if err != nil || len(replacementPlan.Actions) != 1 || replacementPlan.Actions[0].Kind != logs.ChangeReplaced {
		t.Fatalf("PlanReconcile(replacement C) = %#v, %v", replacementPlan, err)
	}
	replacementStream, err := ingester.Open(ctx, OpenRequest{Action: replacementPlan.Actions[0], AtMS: 70})
	if err != nil {
		t.Fatalf("Open(replacement C) error = %v", err)
	}
	result, err := replacementStream.Feed(ctx, replacementContent, true, 80)
	if err != nil || !result.Committed || result.Cursor.State != store.GenerationActive {
		t.Fatalf("Feed(replacement C) = %#v, %v", result, err)
	}
	buildingCursor, err := repository.GenerationCursor(ctx, buildingSourceID, staged.Cursor.Generation)
	if err != nil || buildingCursor.State != store.GenerationSuperseded {
		t.Fatalf("GenerationCursor(building B) = %#v, %v, want superseded", buildingCursor, err)
	}
	for _, sourceFileID := range []string{activeSourceID, buildingSourceID} {
		file, err := repository.SourceFile(ctx, sourceFileID)
		if err != nil || file.State != store.SourceFileUnavailable {
			t.Fatalf("SourceFile(%s) = %#v, %v, want unavailable", sourceFileID, file, err)
		}
	}
}

func TestIngesterResumesInitialPhysicalReplacementWithoutActiveSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, _ := New(repository)
	home, path := newSyntheticCodexHome(t)
	discoverer, _ := logs.NewDiscoverer(home)
	content := []byte(rolloutSessionMetaLine("session-a") + "\n")
	writeSyntheticRollout(t, path, content, time.Unix(10, 0))
	discoveryA, _ := discoverer.Discover(ctx)
	planA, _ := logs.PlanReconcile(home, nil, discoveryA)
	streamA, err := ingester.Open(ctx, OpenRequest{Action: planA.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open(initial A) error = %v", err)
	}
	stagedA, err := streamA.Feed(ctx, content, false, 20)
	if err != nil || stagedA.Cursor.State != store.GenerationBuilding {
		t.Fatalf("Feed(initial A) = %#v, %v", stagedA, err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove initial A: %v", err)
	}
	writeSyntheticRollout(t, path, content, time.Unix(30, 0))
	durable, err := repository.CodexSnapshots(ctx)
	if err != nil || len(durable) != 0 {
		t.Fatalf("CodexSnapshots(initial A) = %#v, %v, want none", durable, err)
	}
	discoveryB, _ := discoverer.DiscoverAgainst(ctx, nil)
	planB, err := logs.PlanReconcile(home, nil, discoveryB)
	if err != nil || len(planB.Actions) != 1 || planB.Actions[0].Kind != logs.ChangeAdded {
		t.Fatalf("PlanReconcile(initial B) = %#v, %v", planB, err)
	}
	streamB, err := ingester.Open(ctx, OpenRequest{Action: planB.Actions[0], AtMS: 40})
	if err != nil {
		t.Fatalf("Open(initial B) error = %v", err)
	}
	stagedB, err := streamB.Feed(ctx, content, false, 50)
	if err != nil || stagedB.Cursor.State != store.GenerationBuilding {
		t.Fatalf("Feed(initial B) = %#v, %v", stagedB, err)
	}

	restartedB, err := ingester.Open(ctx, OpenRequest{Action: planB.Actions[0], AtMS: 60})
	if err != nil {
		t.Fatalf("Open(restarted B) error = %v", err)
	}
	if restartedB.cursor.SourceFileID != stagedB.Cursor.SourceFileID ||
		restartedB.cursor.Generation != stagedB.Cursor.Generation {
		t.Fatalf("Open(restarted B) cursor = %#v, want same building", restartedB.cursor)
	}
	activated, err := restartedB.Feed(ctx, nil, true, 70)
	if err != nil || activated.Cursor.State != store.GenerationActive {
		t.Fatalf("Feed(restarted B EOF) = %#v, %v", activated, err)
	}
	fileA, err := repository.SourceFile(ctx, stagedA.Cursor.SourceFileID)
	if err != nil || fileA.State != store.SourceFileUnavailable {
		t.Fatalf("SourceFile(initial A) = %#v, %v, want unavailable", fileA, err)
	}
}

func TestIngesterKeepsOldFactsVisibleUntilReplacementEOF(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, _ := New(repository)
	home, path := newSyntheticCodexHome(t)
	oldContent := completeRollout("session-a", "turn-old")
	writeSyntheticRollout(t, path, oldContent, time.Unix(10, 0))
	discoverer, err := logs.NewDiscoverer(home)
	if err != nil {
		t.Fatalf("logs.NewDiscoverer() error = %v", err)
	}
	oldDiscovery, err := discoverer.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	oldPlan, _ := logs.PlanReconcile(home, nil, oldDiscovery)
	oldStream, err := ingester.Open(ctx, OpenRequest{Action: oldPlan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open(old) error = %v", err)
	}
	if _, err := oldStream.Feed(ctx, oldContent, true, 20); err != nil {
		t.Fatalf("Feed(old) error = %v", err)
	}
	if _, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-old")); err != nil {
		t.Fatalf("Turn(old) error = %v", err)
	}

	newContent := completeRollout("session-a", "turn-new")
	writeSyntheticRollout(t, path, newContent, time.Unix(30, 0))
	newDiscovery, err := discoverer.DiscoverAgainst(ctx, oldDiscovery.Snapshots)
	if err != nil {
		t.Fatalf("DiscoverAgainst() error = %v", err)
	}
	newPlan, err := logs.PlanReconcile(home, oldDiscovery.Snapshots, newDiscovery)
	if err != nil || len(newPlan.Actions) != 1 ||
		(newPlan.Actions[0].Kind != logs.ChangeReplaced && newPlan.Actions[0].Kind != logs.ChangeTruncated) {
		t.Fatalf("PlanReconcile(replacement) = %#v, %v", newPlan, err)
	}
	newStream, err := ingester.Open(ctx, OpenRequest{Action: newPlan.Actions[0], AtMS: 30})
	if err != nil {
		t.Fatalf("Open(replacement) error = %v", err)
	}
	firstLineEnd := bytesIndexByte(newContent, '\n') + 1
	if firstLineEnd <= 0 {
		t.Fatal("replacement fixture has no complete metadata line")
	}
	first, err := newStream.Feed(ctx, newContent[:firstLineEnd], false, 40)
	if err != nil || !first.Committed || first.Cursor.State != store.GenerationBuilding {
		t.Fatalf("Feed(replacement staging) = %#v, %v, want committed building batch", first, err)
	}
	if _, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-old")); err != nil {
		t.Fatalf("old turn disappeared before EOF: %v", err)
	}
	if _, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-new")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("new turn visible before EOF: %v", err)
	}
	final, err := newStream.Feed(ctx, newContent[firstLineEnd:], true, 50)
	if err != nil || !final.Committed || final.Cursor.State != store.GenerationActive {
		t.Fatalf("Feed(replacement EOF) = %#v, %v, want active generation", final, err)
	}
	if _, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-old")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old turn after activation error = %v, want ErrNotFound", err)
	}
	if turn, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-new")); err != nil || turn.SourceGeneration != 1 {
		t.Fatalf("Turn(new) = %#v, %v, want generation 1", turn, err)
	}
}

func TestIngesterReplacesNewPhysicalIdentityEndToEnd(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, _ := New(repository)
	home, path := newSyntheticCodexHome(t)
	oldContent := completeRollout("session-a", "turn-old")
	writeSyntheticRollout(t, path, oldContent, time.Unix(10, 0))
	discoverer, _ := logs.NewDiscoverer(home)
	oldDiscovery, err := discoverer.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover(old) error = %v", err)
	}
	oldPlan, _ := logs.PlanReconcile(home, nil, oldDiscovery)
	oldStream, err := ingester.Open(ctx, OpenRequest{Action: oldPlan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open(old) error = %v", err)
	}
	if _, err := oldStream.Feed(ctx, oldContent, true, 20); err != nil {
		t.Fatalf("Feed(old) error = %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove old physical source: %v", err)
	}
	newContent := completeRollout("session-a", "turn-new")
	writeSyntheticRollout(t, path, newContent, time.Unix(30, 0))
	newDiscovery, err := discoverer.DiscoverAgainst(ctx, oldDiscovery.Snapshots)
	if err != nil {
		t.Fatalf("DiscoverAgainst(new identity) error = %v", err)
	}
	newPlan, err := logs.PlanReconcile(home, oldDiscovery.Snapshots, newDiscovery)
	if err != nil || len(newPlan.Actions) != 1 || newPlan.Actions[0].Kind != logs.ChangeReplaced ||
		newPlan.Actions[0].Previous == nil || newPlan.Actions[0].Current == nil ||
		newPlan.Actions[0].Previous.SourceFileID == newPlan.Actions[0].Current.SourceFileID {
		t.Fatalf("PlanReconcile(new identity) = %#v, %v, want one physical replacement", newPlan, err)
	}
	stream, err := ingester.Open(ctx, OpenRequest{Action: newPlan.Actions[0], AtMS: 40})
	if err != nil {
		t.Fatalf("Open(new identity) error = %v", err)
	}
	result, err := stream.Feed(ctx, newContent, true, 50)
	if err != nil || !result.Committed || result.Cursor.State != store.GenerationActive {
		t.Fatalf("Feed(new identity) = %#v, %v, want active replacement", result, err)
	}
	if _, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-old")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Turn(old) error = %v, want ErrNotFound", err)
	}
	if turn, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-new")); err != nil || turn.SourceGeneration != 0 {
		t.Fatalf("Turn(new) = %#v, %v, want replacement generation 0", turn, err)
	}
	oldFile, err := repository.SourceFile(ctx, newPlan.Actions[0].Previous.SourceFileID)
	if err != nil || oldFile.State != store.SourceFileUnavailable {
		t.Fatalf("SourceFile(old) = %#v, %v, want unavailable", oldFile, err)
	}
}

func TestIngesterPoisonsFailedStreamAndLeavesDurableCheckpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, database, path := openIndexerRepositoryAt(t)
	ingester, _ := New(repository)
	home, rolloutPath := newSyntheticCodexHome(t)
	content := []byte(rolloutSessionMetaLine("session-a") + "\n")
	writeSyntheticRollout(t, rolloutPath, content, time.Unix(10, 0))
	discoverer, _ := logs.NewDiscoverer(home)
	discovery, _ := discoverer.Discover(ctx)
	plan, _ := logs.PlanReconcile(home, nil, discovery)
	stream, err := ingester.Open(ctx, OpenRequest{Action: plan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := database.Close(ctx); err != nil {
		t.Fatalf("Store.Close() error = %v", err)
	}
	if _, err := stream.Feed(ctx, content, true, 20); err == nil {
		t.Fatal("Feed(closed store) succeeded, want commit error")
	}
	if _, err := stream.Feed(ctx, nil, true, 21); !errors.Is(err, ErrStreamInvalidated) {
		t.Fatalf("Feed(poisoned) error = %v, want ErrStreamInvalidated", err)
	}

	reopened, err := storesqlite.Open(ctx, storesqlite.Config{Path: path})
	if err != nil {
		t.Fatalf("sqlite.Open(reopen) error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })
	reopenedRepository := store.NewRepository(reopened)
	if err := reopenedRepository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema(reopen) error = %v", err)
	}
	cursor, err := reopenedRepository.GenerationCursor(ctx, plan.Actions[0].Current.SourceFileID, 0)
	if err != nil || cursor.Checkpoint.CommittedOffset != 0 || cursor.State != store.GenerationBuilding {
		t.Fatalf("GenerationCursor(reopen) = %#v, %v, want old building offset 0", cursor, err)
	}
}

func TestIngesterRejectsActionsWithoutRolloutStream(t *testing.T) {
	t.Parallel()

	repository, _ := openIndexerRepository(t)
	ingester, _ := New(repository)
	if _, err := ingester.Open(context.Background(), OpenRequest{
		Action: logs.ReconcileAction{Kind: logs.ChangeDeleted}, AtMS: 1,
	}); !errors.Is(err, ErrNoStream) {
		t.Fatalf("Open(deleted) error = %v, want ErrNoStream", err)
	}
}

func TestIngesterPersistsTerminalBeforeStartWithoutReorderingOffsets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, _ := New(repository)
	home, path := newSyntheticCodexHome(t)
	meta := rolloutSessionMetaLine("session-a")
	terminal := rolloutTurnEndLine("turn-late")
	start := rolloutTurnStartLine("turn-late")
	content := []byte(meta + "\n" + terminal + "\n" + start + "\n")
	writeSyntheticRollout(t, path, content, time.Unix(10, 0))
	discoverer, _ := logs.NewDiscoverer(home)
	discovery, err := discoverer.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	plan, err := logs.PlanReconcile(home, nil, discovery)
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	stream, err := ingester.Open(ctx, OpenRequest{Action: plan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := stream.Feed(ctx, content, true, 20); err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	turn, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-late"))
	if err != nil {
		t.Fatalf("Turn() error = %v", err)
	}
	wantCompleteOffset := int64(len(meta) + 1)
	wantStartOffset := wantCompleteOffset + int64(len(terminal)+1)
	if turn.StartOffset != wantStartOffset || turn.CompleteOffset == nil ||
		*turn.CompleteOffset != wantCompleteOffset || *turn.CompleteOffset >= turn.StartOffset {
		t.Fatalf("Turn() = %#v, want start=%d complete=%d", turn, wantStartOffset, wantCompleteOffset)
	}
	cursor, err := repository.GenerationCursor(ctx, plan.Actions[0].Current.SourceFileID, 0)
	if err != nil || cursor.Checkpoint.CommittedOffset != int64(len(content)) ||
		len(cursor.Checkpoint.Seed.ClosedTurns) != 1 {
		t.Fatalf("GenerationCursor() = %#v, %v, want durable closed turn", cursor, err)
	}
}

func TestIngesterRebuildsWhenParserVersionChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	home, path := newSyntheticCodexHome(t)
	content := completeRollout("session-a", "turn-a")
	writeSyntheticRollout(t, path, content, time.Unix(10, 0))
	discoverer, _ := logs.NewDiscoverer(home)
	discovery, _ := discoverer.Discover(ctx)
	snapshot := discovery.Snapshots[0]
	fingerprint := sourceFingerprintFromSnapshot(snapshot)
	initial, err := repository.PrepareGeneration(ctx, store.PrepareGenerationRequest{
		Mode: store.GenerationModeRebuild, Current: fingerprint, ParserVersion: "rollout-v0", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(old parser) error = %v", err)
	}
	session := store.Session{
		SessionID: "session-a", Provider: "codex", SourceKind: "session",
		CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: 1,
	}
	checkpoint := store.ParserCheckpoint{
		Version: store.ParserCheckpointVersion, ParserVersion: "rollout-v0",
		CommittedOffset: int64(len(content)),
		Seed: &store.ParserSeedCheckpoint{Session: &store.CheckpointSessionMeta{
			SessionID: "session-a", RootSessionID: "session-a", SourceKind: "session",
			CreatedAtMS: 1, ObservedAtMS: 1, InitialCWD: "/tmp/project",
			Originator: "cli", CLIVersion: "1", Source: "cli",
		}},
		Projector: store.ProjectorCheckpoint{
			SessionSourceKind: "session",
			Current: &store.SessionCurrent{
				SessionID: "session-a", UpdatedAtMS: 1,
			},
		},
	}
	if _, err := repository.CommitIngestBatch(ctx, store.IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: initial.Generation,
		Fingerprint: fingerprint, Facts: []store.FactBatch{{Session: &session}},
		Checkpoint: checkpoint, EOF: true, AtMS: 20,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(old parser) error = %v", err)
	}
	action := logs.ReconcileAction{Kind: logs.ChangeUnchanged, Previous: &snapshot, Current: &snapshot}
	ingester, _ := New(repository)
	stream, err := ingester.Open(ctx, OpenRequest{Action: action, AtMS: 30})
	if err != nil {
		t.Fatalf("Open(parser upgrade) error = %v", err)
	}
	if stream.cursor.Generation != 1 || stream.cursor.State != store.GenerationBuilding ||
		stream.cursor.ParserVersion != logs.ParserVersion {
		t.Fatalf("Open(parser upgrade) cursor = %#v, want building generation 1", stream.cursor)
	}
}

func TestIngesterRestoresOpenTurnProjectionAcrossRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, _ := New(repository)
	home, path := newSyntheticCodexHome(t)
	meta := rolloutSessionMetaLine("session-a")
	start := rolloutTurnStartLine("turn-a")
	contextLine := `{"timestamp":"2026-07-14T01:00:02Z","type":"turn_context","payload":{"turn_id":"turn-a","cwd":"/tmp/project","model":"gpt-5","effort":"high"}}`
	initialContent := []byte(meta + "\n" + start + "\n" + contextLine + "\n")
	writeSyntheticRollout(t, path, initialContent, time.Unix(10, 0))
	discoverer, _ := logs.NewDiscoverer(home)
	initialDiscovery, _ := discoverer.Discover(ctx)
	initialPlan, _ := logs.PlanReconcile(home, nil, initialDiscovery)
	stream, err := ingester.Open(ctx, OpenRequest{Action: initialPlan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open(initial) error = %v", err)
	}
	if _, err := stream.Feed(ctx, initialContent, true, 20); err != nil {
		t.Fatalf("Feed(initial) error = %v", err)
	}
	started, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-a"))
	if err != nil || started.CompletedAtMS != nil || started.Model == nil || *started.Model != "gpt-5" {
		t.Fatalf("Turn(open) = %#v, %v", started, err)
	}

	terminal := rolloutTurnEndLine("turn-a") + "\n"
	fullContent := append(append([]byte(nil), initialContent...), []byte(terminal)...)
	writeSyntheticRollout(t, path, fullContent, time.Unix(20, 0))
	nextDiscovery, _ := discoverer.DiscoverAgainst(ctx, initialDiscovery.Snapshots)
	nextPlan, _ := logs.PlanReconcile(home, initialDiscovery.Snapshots, nextDiscovery)
	restarted, err := ingester.Open(ctx, OpenRequest{Action: nextPlan.Actions[0], AtMS: 30})
	if err != nil {
		t.Fatalf("Open(restart) error = %v", err)
	}
	if _, err := restarted.Feed(ctx, []byte(terminal), true, 40); err != nil {
		t.Fatalf("Feed(restart terminal) error = %v", err)
	}
	completed, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-a"))
	wantStart := int64(len(meta) + 1)
	if err != nil || completed.CompletedAtMS == nil || completed.StartOffset != wantStart ||
		completed.Model == nil || *completed.Model != "gpt-5" ||
		completed.ReasoningEffort == nil || *completed.ReasoningEffort != "high" {
		t.Fatalf("Turn(completed after restart) = %#v, %v, want restored start/model/effort", completed, err)
	}
}

func TestIngesterRestoresCounterEpochAcrossRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, _ := New(repository)
	home, path := newSyntheticCodexHome(t)
	meta := rolloutSessionMetaLine("session-a")
	usage100 := rolloutSessionUsageLine(100, 50)
	initialContent := []byte(meta + "\n" + usage100 + "\n")
	writeSyntheticRollout(t, path, initialContent, time.Unix(10, 0))
	discoverer, _ := logs.NewDiscoverer(home)
	initialDiscovery, _ := discoverer.Discover(ctx)
	initialPlan, _ := logs.PlanReconcile(home, nil, initialDiscovery)
	stream, err := ingester.Open(ctx, OpenRequest{Action: initialPlan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open(initial) error = %v", err)
	}
	if _, err := stream.Feed(ctx, initialContent, true, 20); err != nil {
		t.Fatalf("Feed(initial) error = %v", err)
	}
	session, err := repository.Session(ctx, "session-a")
	if err != nil || session.Usage == nil || session.Usage.CounterEpoch != 0 ||
		session.Usage.CounterState != "rebuilt" {
		t.Fatalf("Session(initial usage) = %#v, %v", session, err)
	}

	usage50 := rolloutSessionUsageLine(50, 60) + "\n"
	fullContent := append(append([]byte(nil), initialContent...), []byte(usage50)...)
	writeSyntheticRollout(t, path, fullContent, time.Unix(20, 0))
	nextDiscovery, _ := discoverer.DiscoverAgainst(ctx, initialDiscovery.Snapshots)
	nextPlan, _ := logs.PlanReconcile(home, initialDiscovery.Snapshots, nextDiscovery)
	restarted, err := ingester.Open(ctx, OpenRequest{Action: nextPlan.Actions[0], AtMS: 30})
	if err != nil {
		t.Fatalf("Open(restart) error = %v", err)
	}
	if _, err := restarted.Feed(ctx, []byte(usage50), true, 40); err != nil {
		t.Fatalf("Feed(reset usage) error = %v", err)
	}
	session, err = repository.Session(ctx, "session-a")
	if err != nil || session.Usage == nil || session.Usage.CounterEpoch != 1 ||
		session.Usage.CounterState != "reset" || session.Usage.TotalInputTokens == nil ||
		*session.Usage.TotalInputTokens != 50 {
		t.Fatalf("Session(reset usage) = %#v, %v, want epoch 1 reset", session, err)
	}
}

func TestIngesterCommitsMetadataOnlyMoveWithoutReplayingFacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, _ := New(repository)
	home, path := newSyntheticCodexHome(t)
	content := []byte(rolloutSessionMetaLine("session-a") + "\n")
	writeSyntheticRollout(t, path, content, time.Unix(10, 0))
	discoverer, _ := logs.NewDiscoverer(home)
	initialDiscovery, _ := discoverer.Discover(ctx)
	initialPlan, _ := logs.PlanReconcile(home, nil, initialDiscovery)
	stream, err := ingester.Open(ctx, OpenRequest{Action: initialPlan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open(initial) error = %v", err)
	}
	if _, err := stream.Feed(ctx, content, true, 20); err != nil {
		t.Fatalf("Feed(initial) error = %v", err)
	}
	movedPath := filepath.Join(home, "archived_sessions", "2026", "07", "rollout.jsonl")
	if err := os.MkdirAll(filepath.Dir(movedPath), 0o700); err != nil {
		t.Fatalf("create archive directory: %v", err)
	}
	if err := os.Rename(path, movedPath); err != nil {
		t.Fatalf("move rollout: %v", err)
	}
	nextDiscovery, err := discoverer.DiscoverAgainst(ctx, initialDiscovery.Snapshots)
	if err != nil {
		t.Fatalf("DiscoverAgainst(move) error = %v", err)
	}
	nextPlan, err := logs.PlanReconcile(home, initialDiscovery.Snapshots, nextDiscovery)
	if err != nil || len(nextPlan.Actions) != 1 || nextPlan.Actions[0].Kind != logs.ChangeMoved {
		t.Fatalf("PlanReconcile(move) = %#v, %v", nextPlan, err)
	}
	moved, err := ingester.Open(ctx, OpenRequest{Action: nextPlan.Actions[0], AtMS: 30})
	if err != nil {
		t.Fatalf("Open(move) error = %v", err)
	}
	result, err := moved.Feed(ctx, nil, true, 40)
	if err != nil || !result.Committed || result.CommittableOffset != int64(len(content)) {
		t.Fatalf("Feed(move) = %#v, %v, want metadata-only commit", result, err)
	}
	file, err := repository.SourceFile(ctx, nextPlan.Actions[0].Current.SourceFileID)
	if err != nil || file.CurrentPath != movedPath || file.ParsedOffset != int64(len(content)) {
		t.Fatalf("SourceFile(move) = %#v, %v", file, err)
	}
	if session, err := repository.Session(ctx, "session-a"); err != nil || session.SourceKind != "session" {
		t.Fatalf("Session(move) = %#v, %v, want stable original session metadata", session, err)
	}

	durableMoved, err := repository.CodexSnapshots(ctx)
	if err != nil || len(durableMoved) != 1 {
		t.Fatalf("CodexSnapshots(move) = %#v, %v", durableMoved, err)
	}
	grownContent := append(append([]byte(nil), content...), []byte(rolloutTurnStartLine("turn-after-move")+"\n"+rolloutTurnEndLine("turn-after-move")+"\n")...)
	writeSyntheticRollout(t, movedPath, grownContent, time.Unix(50, 0))
	grownDiscovery, err := discoverer.DiscoverAgainst(ctx, snapshotsFromStore(durableMoved))
	if err != nil {
		t.Fatalf("DiscoverAgainst(grown archive) error = %v", err)
	}
	grownPlan, err := logs.PlanReconcile(home, snapshotsFromStore(durableMoved), grownDiscovery)
	if err != nil || len(grownPlan.Actions) != 1 || grownPlan.Actions[0].Kind != logs.ChangeGrown {
		t.Fatalf("PlanReconcile(grown archive) = %#v, %v", grownPlan, err)
	}
	grown, err := ingester.Open(ctx, OpenRequest{Action: grownPlan.Actions[0], AtMS: 60})
	if err != nil {
		t.Fatalf("Open(grown archive) error = %v", err)
	}
	if _, err := grown.Feed(ctx, grownContent[len(content):], true, 70); err != nil {
		t.Fatalf("Feed(grown archive) error = %v", err)
	}
	if session, err := repository.Session(ctx, "session-a"); err != nil || session.SourceKind != "session" {
		t.Fatalf("Session(grown archive) = %#v, %v, want canonical source kind session", session, err)
	}
	if _, err := repository.Turn(ctx, CanonicalTurnID("session-a", "turn-after-move")); err != nil {
		t.Fatalf("Turn(after archive move) error = %v", err)
	}

	file, err = repository.SourceFile(ctx, grownPlan.Actions[0].Current.SourceFileID)
	if err != nil {
		t.Fatalf("SourceFile(before archived rebuild) error = %v", err)
	}
	file.ParserVersion = "rollout-v0"
	file.UpdatedAtMS = 80
	if err := repository.UpsertSourceFile(ctx, file); err != nil {
		t.Fatalf("UpsertSourceFile(force archived rebuild) error = %v", err)
	}
	durableArchived, err := repository.CodexSnapshots(ctx)
	if err != nil || len(durableArchived) != 1 {
		t.Fatalf("CodexSnapshots(archived rebuild) = %#v, %v", durableArchived, err)
	}
	rebuildDiscovery, err := discoverer.DiscoverAgainst(ctx, snapshotsFromStore(durableArchived))
	if err != nil {
		t.Fatalf("DiscoverAgainst(archived rebuild) error = %v", err)
	}
	rebuildPlan, err := logs.PlanReconcile(home, snapshotsFromStore(durableArchived), rebuildDiscovery)
	if err != nil || len(rebuildPlan.Actions) != 1 || rebuildPlan.Actions[0].Kind != logs.ChangeUnchanged {
		t.Fatalf("PlanReconcile(archived rebuild) = %#v, %v", rebuildPlan, err)
	}
	rebuild, err := ingester.Open(ctx, OpenRequest{Action: rebuildPlan.Actions[0], AtMS: 90})
	if err != nil {
		t.Fatalf("Open(archived rebuild) error = %v", err)
	}
	if rebuild.cursor.State != store.GenerationBuilding {
		t.Fatalf("Open(archived rebuild) cursor = %#v, want building", rebuild.cursor)
	}
	if _, err := rebuild.Feed(ctx, grownContent, true, 100); err != nil {
		t.Fatalf("Feed(archived rebuild) error = %v", err)
	}
	if session, err := repository.Session(ctx, "session-a"); err != nil || session.SourceKind != "session" {
		t.Fatalf("Session(archived rebuild) = %#v, %v, want canonical source kind session", session, err)
	}
}

func TestIngesterCommitsJobCursorWithFactsAndCheckpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	home, path := newSyntheticCodexHome(t)
	content := []byte(rolloutSessionMetaLine("session-a") + "\n")
	writeSyntheticRollout(t, path, content, time.Unix(10, 0))
	discoverer, _ := logs.NewDiscoverer(home)
	discovery, _ := discoverer.Discover(ctx)
	plan, _ := logs.PlanReconcile(home, nil, discovery)
	fingerprint := sourceFingerprintFromSnapshot(*plan.Actions[0].Current)
	if _, err := repository.PrepareGeneration(ctx, store.PrepareGenerationRequest{
		Mode: store.GenerationModeRebuild, Current: fingerprint,
		ParserVersion: logs.ParserVersion, AtMS: 5,
	}); err != nil {
		t.Fatalf("PrepareGeneration() error = %v", err)
	}
	zero, total := int64(0), int64(len(content))
	sourceFileID := fingerprint.SourceFileID
	job := store.JobRun{
		JobID: "job-a", JobType: "codex_ingest", RequestedBy: "test",
		State: store.JobQueued, Phase: store.JobPhaseReconcile, SourceFileID: &sourceFileID,
		CreatedAtMS: 6, ProgressCurrent: &zero, ProgressTotal: &total,
		ResumeCursor: &store.JobCursor{Generation: 0, Offset: 0}, UpdatedAtMS: 6,
	}
	if err := repository.CreateJobRun(ctx, job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	ingester, _ := New(repository)
	stream, err := ingester.Open(ctx, OpenRequest{Action: plan.Actions[0], JobID: job.JobID, AtMS: 10})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := stream.Feed(ctx, content, true, 20); err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	stored, err := repository.JobRun(ctx, job.JobID)
	if err != nil || stored.State != store.JobRunning || stored.ProgressCurrent == nil ||
		*stored.ProgressCurrent != total || stored.ResumeCursor == nil || stored.ResumeCursor.Offset != total {
		t.Fatalf("JobRun() = %#v, %v, want running cursor %d", stored, err, total)
	}
}

func openIndexerRepository(t *testing.T) (*store.Repository, *storesqlite.Store) {
	t.Helper()
	repository, database, _ := openIndexerRepositoryAt(t)
	return repository, database
}

func openIndexerRepositoryAt(t *testing.T) (*store.Repository, *storesqlite.Store, string) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	path := filepath.Join(directory, "codex-pulse-test.db")
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: path})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = database.Close(context.Background())
		}
	})
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	return repository, database, path
}

func newSyntheticCodexHome(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "2026", "07", "rollout.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create synthetic sessions directory: %v", err)
	}
	return home, path
}

func writeSyntheticRollout(t *testing.T, path string, content []byte, modified time.Time) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write synthetic rollout: %v", err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatalf("set synthetic rollout time: %v", err)
	}
}

func snapshotsFromStore(values []store.SourceFingerprint) []logs.Snapshot {
	result := make([]logs.Snapshot, len(values))
	for index, value := range values {
		result[index] = logs.Snapshot{
			SourceFileID: value.SourceFileID, Provider: value.Provider,
			Kind: logs.SourceKind(value.SourceKind), Path: value.CurrentPath,
			Fingerprint: logs.Fingerprint{
				DeviceID: value.DeviceID, Inode: value.Inode, SizeBytes: value.SizeBytes,
				MTimeNS: value.MTimeNS, PrefixBytes: value.PrefixBytes,
				PrefixSHA256: value.PrefixSHA256, Digest: value.FingerprintSHA256,
			},
		}
	}
	return result
}

func rolloutSessionMetaLine(sessionID string) string {
	return `{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"` + sessionID +
		`","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}`
}

func rolloutTurnStartLine(turnID string) string {
	return `{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"task_started","turn_id":"` + turnID +
		`","started_at":1783990801,"model_context_window":258000}}`
}

func rolloutTurnEndLine(turnID string) string {
	return `{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"` + turnID +
		`","completed_at":1783990802}}`
}

func rolloutSessionUsageLine(input, output int64) string {
	return `{"timestamp":"2026-07-14T01:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":` +
		fmt.Sprintf("%d", input) + `,"output_tokens":` + fmt.Sprintf("%d", output) +
		`},"last_token_usage":{}}}}`
}

func completeRollout(sessionID, turnID string) []byte {
	return []byte(rolloutSessionMetaLine(sessionID) + "\n" + rolloutTurnStartLine(turnID) + "\n" +
		rolloutTurnEndLine(turnID) + "\n")
}

func bytesIndexByte(value []byte, target byte) int {
	for index, current := range value {
		if current == target {
			return index
		}
	}
	return -1
}
