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
