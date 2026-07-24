package indexer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestProjectorMapsQuotaObservationWithStableProvenance(t *testing.T) {
	t.Parallel()

	projector, err := newProjector("source-quota", 4, store.GenerationModeRebuild, nil, store.ProjectorCheckpoint{})
	if err != nil {
		t.Fatalf("newProjector() error = %v", err)
	}
	limitID, limitName, planType := "codex_bengalfox", "GPT-5.3-Codex-Spark", "pro"
	events := []logs.ParsedEvent{
		{
			Kind: logs.EventSessionMeta, Position: logs.SourcePosition{StartOffset: 0, EndOffset: 100},
			SessionMeta: &logs.SessionMetaFact{
				SessionID: "session-quota", RootSessionID: "session-quota", SourceKind: logs.SourceKindSession,
				CreatedAtMS: 1_783_990_800_000, ObservedAtMS: 1_783_990_800_000,
				InitialCWD: "/synthetic", Originator: "codex_cli_rs", CLIVersion: "0.142.3",
				Source: "cli", ModelProvider: "openai",
			},
		},
		{
			Kind: logs.EventQuotaObservation, Position: logs.SourcePosition{StartOffset: 100, EndOffset: 200},
			QuotaObservation: &logs.QuotaObservationFact{
				SessionID: "session-quota", AccountScope: logs.QuotaAccountScopeDefault,
				Source: logs.QuotaSourceLocalJSONL, LimitID: &limitID, LimitName: &limitName,
				WindowKind:  logs.QuotaWindowPrimary,
				UsedPercent: 38, WindowMinutes: 300, ResetsAtMS: 1_784_008_800_000,
				PlanType: &planType, ObservedAtMS: 1_783_990_801_000, Validity: logs.QuotaValidityAccepted,
			},
		},
	}

	facts, _, err := projector.Project(events)
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	if len(facts) != 2 || facts[1].QuotaObservation == nil || facts[1].Session == nil {
		t.Fatalf("quota facts = %#v", facts)
	}
	got := facts[1].QuotaObservation
	if got.ObservationID == "" || !strings.HasPrefix(got.ObservationID, "quota-local-jsonl-") ||
		got.AccountScope != store.QuotaAccountScopeDefault || got.Source != store.QuotaSourceLocalJSONL ||
		got.LimitName == nil || *got.LimitName != limitName ||
		got.WindowKind != store.QuotaWindowPrimary || got.UsedPercent != 38 || got.WindowMinutes != 300 ||
		got.ResetsAtMS != 1_784_008_800_000 || got.Validity != store.QuotaValidityAccepted ||
		got.SessionID == nil || *got.SessionID != "session-quota" || got.SourceGeneration != 4 ||
		got.SourceFileID == nil || *got.SourceFileID != "source-quota" ||
		got.SourceOffset != 100 || got.RequestID != nil {
		t.Fatalf("quota observation = %#v", got)
	}
	if facts[1].Session.LastSeenAtMS != got.ObservedAtMS {
		t.Fatalf("quota session = %#v, want last seen %d", facts[1].Session, got.ObservedAtMS)
	}

	replay, err := newProjector("source-quota", 4, store.GenerationModeRebuild, nil, store.ProjectorCheckpoint{})
	if err != nil {
		t.Fatalf("newProjector(replay) error = %v", err)
	}
	replayedFacts, _, err := replay.Project(events)
	if err != nil || replayedFacts[1].QuotaObservation == nil ||
		replayedFacts[1].QuotaObservation.ObservationID != got.ObservationID {
		t.Fatalf("stable observation ID replay = %#v, %v", replayedFacts, err)
	}
}

func TestIngesterCommitsLocalQuotaObservationsWithCheckpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, err := New(repository)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	home, path := newSyntheticCodexHome(t)
	meta := rolloutSessionMetaLine("session-quota")
	quota := `{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"codex_bengalfox","limit_name":"GPT-5.3-Codex-Spark","primary":{"used_percent":38,"window_minutes":300,"resets_at":1784008800},"secondary":{"used_percent":12,"window_minutes":10080,"resets_at":1784595600},"plan_type":"pro"}}}`
	content := []byte(strings.Join([]string{meta, quota}, "\n") + "\n")
	writeSyntheticRollout(t, path, content, time.Unix(10, 0))
	discoverer, err := logs.NewDiscoverer(home)
	if err != nil {
		t.Fatalf("logs.NewDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	plan, err := logs.PlanReconcile(home, nil, discovery)
	if err != nil || len(plan.Actions) != 1 {
		t.Fatalf("PlanReconcile() = %#v, %v", plan, err)
	}
	stream, err := ingester.Open(ctx, OpenRequest{Action: plan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	result, err := stream.Feed(ctx, content, true, 20)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if !result.Committed || result.Cursor.State != store.GenerationActive ||
		result.Cursor.Checkpoint.CommittedOffset != int64(len(content)) {
		t.Fatalf("Feed() = %#v, want active checkpoint at EOF", result)
	}

	sessionID := "session-quota"
	observations, err := repository.ListQuotaObservations(ctx, store.QuotaObservationFilter{
		SessionID: &sessionID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListQuotaObservations() error = %v", err)
	}
	quotaOffset := int64(len(meta) + 1)
	if len(observations) != 2 || observations[0].ObservationID == observations[1].ObservationID {
		t.Fatalf("quota observations = %#v", observations)
	}
	byWindow := make(map[store.QuotaWindowKind]store.QuotaObservation, len(observations))
	for _, observation := range observations {
		byWindow[observation.WindowKind] = observation
	}
	if len(byWindow) != 2 || byWindow[store.QuotaWindowPrimary].UsedPercent != 38 ||
		byWindow[store.QuotaWindowSecondary].UsedPercent != 12 {
		t.Fatalf("quota observations by window = %#v", byWindow)
	}
	for _, observation := range observations {
		if observation.Source != store.QuotaSourceLocalJSONL || observation.SessionID == nil ||
			observation.LimitName == nil || *observation.LimitName != "GPT-5.3-Codex-Spark" ||
			*observation.SessionID != sessionID || observation.SourceGeneration != result.Cursor.Generation ||
			observation.SourceFileID == nil || *observation.SourceFileID != result.Cursor.SourceFileID ||
			observation.SourceOffset != quotaOffset || observation.FirstSourceOffset != quotaOffset ||
			observation.SampleCount != 1 {
			t.Fatalf("quota observation provenance = %#v", observation)
		}
	}
}

func TestIngesterDeepSessionModeSkipsQuotaFacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, err := New(repository)
	if err != nil {
		t.Fatal(err)
	}
	home, path := newSyntheticCodexHome(t)
	meta := rolloutSessionMetaLine("session-deep")
	quota := `{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"codex","primary":{"used_percent":38,"window_minutes":300,"resets_at":1784008800},"plan_type":"pro"}}}`
	content := []byte(meta + "\n" + quota + "\n")
	writeSyntheticRollout(t, path, content, time.Unix(10, 0))
	discovery, err := logs.NewDiscoverer(home)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := discovery.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := logs.PlanReconcile(home, nil, observed)
	if err != nil || len(plan.Actions) != 1 {
		t.Fatalf("PlanReconcile() = %#v, %v", plan, err)
	}
	stream, err := ingester.Open(ctx, OpenRequest{Action: plan.Actions[0], AtMS: 10, SkipQuotaFacts: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Feed(ctx, content, true, 20); err != nil {
		t.Fatal(err)
	}
	sessionID := "session-deep"
	observations, err := repository.ListQuotaObservations(ctx, store.QuotaObservationFilter{SessionID: &sessionID, Limit: 10})
	if err != nil || len(observations) != 0 {
		t.Fatalf("quota observations = %#v, %v", observations, err)
	}
}

func TestIngesterCommitsZeroResetAsSuspiciousQuota(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, err := New(repository)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	home, path := newSyntheticCodexHome(t)
	meta := rolloutSessionMetaLine("session-zero-reset")
	quota := `{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"codex","primary":{"used_percent":38,"window_minutes":300,"resets_at":0},"plan_type":"pro"}}}`
	content := []byte(strings.Join([]string{meta, quota}, "\n") + "\n")
	writeSyntheticRollout(t, path, content, time.Unix(10, 0))
	discoverer, err := logs.NewDiscoverer(home)
	if err != nil {
		t.Fatalf("logs.NewDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	plan, err := logs.PlanReconcile(home, nil, discovery)
	if err != nil || len(plan.Actions) != 1 {
		t.Fatalf("PlanReconcile() = %#v, %v", plan, err)
	}
	stream, err := ingester.Open(ctx, OpenRequest{Action: plan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	result, err := stream.Feed(ctx, content, true, 20)
	if err != nil || !result.Committed || result.Cursor.State != store.GenerationActive {
		t.Fatalf("Feed() = %#v, %v, want committed zero-reset observation", result, err)
	}
	sourceFileID := result.Cursor.SourceFileID
	observations, err := repository.ListQuotaObservations(ctx, store.QuotaObservationFilter{
		SourceFileID: &sourceFileID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListQuotaObservations() error = %v", err)
	}
	if len(observations) != 1 || observations[0].ResetsAtMS != 0 ||
		observations[0].Validity != store.QuotaValiditySuspicious ||
		observations[0].RejectionReason == nil ||
		*observations[0].RejectionReason != store.QuotaReasonResetNotFuture {
		t.Fatalf("zero-reset observation = %#v", observations)
	}
}

func TestIngesterKeepsQuotaLineageDistinctAcrossPhysicalReplacement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, _ := openIndexerRepository(t)
	ingester, err := New(repository)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	home, path := newSyntheticCodexHome(t)
	discoverer, err := logs.NewDiscoverer(home)
	if err != nil {
		t.Fatalf("logs.NewDiscoverer() error = %v", err)
	}
	meta := rolloutSessionMetaLine("session-quota-replacement")
	quotaLine := func(usedPercent int) string {
		return `{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"limit_id":"codex","primary":{"used_percent":` +
			fmt.Sprint(usedPercent) + `,"window_minutes":300,"resets_at":1784008800},"plan_type":"pro"}}}`
	}
	oldContent := []byte(strings.Join([]string{meta, quotaLine(38)}, "\n") + "\n")
	writeSyntheticRollout(t, path, oldContent, time.Unix(10, 0))
	oldDiscovery, err := discoverer.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover(old) error = %v", err)
	}
	oldPlan, err := logs.PlanReconcile(home, nil, oldDiscovery)
	if err != nil || len(oldPlan.Actions) != 1 {
		t.Fatalf("PlanReconcile(old) = %#v, %v", oldPlan, err)
	}
	oldStream, err := ingester.Open(ctx, OpenRequest{Action: oldPlan.Actions[0], AtMS: 10})
	if err != nil {
		t.Fatalf("Open(old) error = %v", err)
	}
	oldResult, err := oldStream.Feed(ctx, oldContent, true, 20)
	if err != nil || oldResult.Cursor.State != store.GenerationActive {
		t.Fatalf("Feed(old) = %#v, %v", oldResult, err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove old physical source: %v", err)
	}
	newContent := []byte(strings.Join([]string{meta, quotaLine(39)}, "\n") + "\n")
	writeSyntheticRollout(t, path, newContent, time.Unix(30, 0))
	newDiscovery, err := discoverer.DiscoverAgainst(ctx, oldDiscovery.Snapshots)
	if err != nil {
		t.Fatalf("DiscoverAgainst(new) error = %v", err)
	}
	newPlan, err := logs.PlanReconcile(home, oldDiscovery.Snapshots, newDiscovery)
	if err != nil || len(newPlan.Actions) != 1 || newPlan.Actions[0].Kind != logs.ChangeReplaced ||
		newPlan.Actions[0].Previous == nil || newPlan.Actions[0].Current == nil ||
		newPlan.Actions[0].Previous.SourceFileID == newPlan.Actions[0].Current.SourceFileID {
		t.Fatalf("PlanReconcile(new) = %#v, %v, want physical replacement", newPlan, err)
	}
	newStream, err := ingester.Open(ctx, OpenRequest{Action: newPlan.Actions[0], AtMS: 40})
	if err != nil {
		t.Fatalf("Open(new) error = %v", err)
	}
	newResult, err := newStream.Feed(ctx, newContent, true, 50)
	if err != nil || newResult.Cursor.State != store.GenerationActive {
		t.Fatalf("Feed(new) = %#v, %v, want active replacement", newResult, err)
	}

	source := store.QuotaSourceLocalJSONL
	observations, err := repository.ListQuotaObservations(ctx, store.QuotaObservationFilter{
		Source: &source, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListQuotaObservations() error = %v", err)
	}
	if len(observations) != 2 || observations[0].ObservationID == observations[1].ObservationID ||
		observations[0].SourceFileID == nil || observations[1].SourceFileID == nil ||
		*observations[0].SourceFileID == *observations[1].SourceFileID ||
		observations[0].UsedPercent == observations[1].UsedPercent {
		t.Fatalf("replacement quota observations = %#v", observations)
	}
}
