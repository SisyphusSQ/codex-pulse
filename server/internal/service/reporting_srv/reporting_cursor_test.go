package reporting_srv

import (
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestReportingCursorCyclesRemainHistoryWithoutAddingLocalCopies(t *testing.T) {
	s, db, clients := reportingFixture(t)
	base := centerfixture.Snapshot()
	base.Provider = "cursor"
	base.SourceKind = "cursor_local"
	c := centerfixture.Contribution(999, 1000)
	c.ID = reportingv1.ContributionID("cursor", "session", c, 0)
	base.Contributions = []reportingv1.Contribution{c}
	centerfixture.SendSnapshot(t, s, clients[0], base)
	dashboard := base
	dashboard.HomeID = "billing-cycle-1"
	dashboard.SourceKind = "cursor_dashboard"
	c = centerfixture.Contribution(10, 1000)
	c.ID = reportingv1.ContributionID("cursor", "session", c, 0)
	dashboard.Contributions = []reportingv1.Contribution{c}
	centerfixture.SendSnapshot(t, s, clients[0], dashboard)
	_, total := readSession(t, db)
	if total != 10 {
		t.Fatal("dashboard and local copy summed")
	}
	next := dashboard
	next.HomeID = "billing-cycle-2"
	next.CollectedAtMS = 5000
	c = centerfixture.Contribution(20, 4000)
	c.ID = reportingv1.ContributionID("cursor", "session", c, 0)
	next.Contributions = []reportingv1.Contribution{c}
	centerfixture.SendSnapshot(t, s, clients[0], next)
	_, total = readSession(t, db)
	if total != 30 {
		t.Fatal("new billing cycle erased old observed history")
	}
	centerfixture.SendSnapshot(t, s, clients[1], dashboard)
	centerfixture.SendSnapshot(t, s, clients[1], next)
	meta, total := readSession(t, db)
	if total != 30 || meta.Conflict {
		t.Fatal("copied billing histories duplicated or conflicted")
	}
	var rows int64
	_ = db.Model(&reporting_do.Usage{}).Count(&rows).Error
	if rows != 2 {
		t.Fatal("billing events were not deduplicated")
	}
}

func cursorDashboardSnapshot(tokens int64) reportingv1.SessionSnapshot {
	snapshot := centerfixture.Snapshot()
	snapshot.Provider = "cursor"
	snapshot.SourceKind = "cursor_dashboard"
	snapshot.HomeID = "billing-cycle-1"
	snapshot.Complete = false // 旧 Helper 没有本地会话 metadata。
	c := centerfixture.Contribution(tokens, 1000)
	c.ID = reportingv1.ContributionID("cursor", snapshot.SessionID, c, 0)
	snapshot.Contributions = []reportingv1.Contribution{c}
	return snapshot
}

func TestReportingCursorLegacyDashboardCorrectionsReplaceFacts(t *testing.T) {
	s, db, clients := reportingFixture(t)
	old := cursorDashboardSnapshot(10)
	for _, client := range clients {
		centerfixture.SendSnapshot(t, s, client, old)
	}
	updated := cursorDashboardSnapshot(100)
	updated.Revision = 2
	updated.CollectedAtMS++
	centerfixture.SendSnapshot(t, s, clients[0], updated)
	meta, total := readSession(t, db)
	if total != 100 || meta.CanonicalRevision != 2 || !meta.CorrectionFence || !meta.Conflict {
		t.Fatalf("latest complete Dashboard facts not accepted: %+v total=%d", meta, total)
	}
	for _, client := range clients[1:] {
		centerfixture.SendSnapshot(t, s, client, updated)
	}
	meta, total = readSession(t, db)
	if total != 100 || meta.Conflict {
		t.Fatalf("identical copies duplicated or conflicted: total=%d conflict=%v", total, meta.Conflict)
	}
	// 正式修订允许减少，不能以取数值最大值替代仲裁。
	updated = cursorDashboardSnapshot(5)
	updated.Revision = 3
	updated.CollectedAtMS += 2
	centerfixture.SendSnapshot(t, s, clients[0], updated)
	meta, total = readSession(t, db)
	if total != 5 || !meta.Conflict {
		t.Fatalf("stale copies resurrected corrected facts: total=%d conflict=%v", total, meta.Conflict)
	}
}
