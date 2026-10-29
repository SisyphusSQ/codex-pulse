package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

func TestPublishedEmptyHomeNeverFallsBackToRetainedDeepFacts(t *testing.T) {
	t.Parallel()
	repository := openLightRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository.coreTestRepository.Repository, false)
	ctx := context.Background()
	rangeValue := AnalyticsRange{ReportingTimezone: "UTC", StartAtMS: 0, EndAtMS: 86_400_000}
	sessionFilter := SessionAnalyticsFilter{Limit: 20, SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending}
	projectFilter := ProjectAnalyticsFilter{Range: rangeValue, Limit: 20, SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending}
	before, err := repository.ListSessionAnalytics(ctx, sessionFilter)
	if err != nil || before.MatchedCount == 0 {
		t.Fatalf("legacy fixture did not expose retained facts: %v", err)
	}
	oldProjects, err := repository.ListProjectAnalytics(ctx, projectFilter)
	if err != nil || len(oldProjects.Records) == 0 {
		t.Fatalf("legacy fixture did not expose project facts: %v", err)
	}
	if err := repository.ReplaceLightMetadata(ctx, storelight.LightMetadataSnapshot{
		Home:       storelight.LightHomeIdentity{Path: filepath.Join(t.TempDir(), "new-empty-home"), DeviceID: "2", Inode: 3},
		Generation: 1, ReadyAtMS: 1_000, Sessions: []storelight.LightSessionMetadata{},
	}); err != nil {
		t.Fatal(err)
	}
	usage, err := repository.UsageCostRange(ctx, rangeValue)
	if err != nil || usage.Mode != AnalyticsReadLightIndex || len(usage.Daily) != 0 || len(usage.Models) != 0 {
		t.Fatalf("empty Home read old usage: mode=%q days=%d error=%v", usage.Mode, len(usage.Daily), err)
	}
	sessions, err := repository.ListSessionAnalytics(ctx, sessionFilter)
	if err != nil || sessions.Mode != AnalyticsReadLightIndex || sessions.MatchedCount != 0 || len(sessions.Records) != 0 {
		t.Fatalf("empty Home read old sessions: %#v, %v", sessions, err)
	}
	if _, err := repository.SessionAnalytics(ctx, SessionAnalyticsDetailFilter{SessionID: before.Records[0].SessionID, TurnLimit: 20}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old Home session detail remained readable: %v", err)
	}
	projects, err := repository.ListProjectAnalytics(ctx, projectFilter)
	if err != nil || projects.Mode != AnalyticsReadLightIndex || len(projects.Records) != 0 || projects.MatchedCount != 0 {
		t.Fatalf("empty Home read old projects: %#v, %v", projects, err)
	}
	if _, err := repository.ProjectAnalytics(ctx, ProjectAnalyticsDetailFilter{Range: rangeValue, DimensionKey: oldProjects.Records[0].DimensionKey, SessionLimit: 20, ModelLimit: 20}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old Home project detail remained readable: %v", err)
	}
}
