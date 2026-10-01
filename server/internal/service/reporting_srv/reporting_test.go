package reporting_srv

import (
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	reporting_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/reporting_vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/access_repo"
	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func reportingFixture(t *testing.T) (*Reporting, *gorm.DB, []access_dto.Principal) {
	t.Helper()
	engine := centerfixture.Engine(t)
	access := access_srv.NewAccess(access_repo.NewAccess(engine))
	admin := centerfixture.Admin(t, access)
	clients := []access_dto.Principal{}
	for _, name := range []string{"机器一", "机器二", "机器三"} {
		clients = append(clients, centerfixture.Collector(t, access, admin.Principal, name))
	}
	return NewReporting(reporting_repo.NewReporting(engine)), engine.DB(t.Context()), clients
}

func readSession(t *testing.T, db *gorm.DB) (meta reporting_do.Session, total int64) {
	t.Helper()
	if err := db.First(&meta).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&reporting_do.Usage{}).Select("COALESCE(SUM(total_tokens),0)").Scan(&total).Error; err != nil {
		t.Fatal(err)
	}
	return
}
func TestReportingDuplicateReceiptCopiesGrowthAndRevision(t *testing.T) {
	s, db, clients := reportingFixture(t)
	snap := centerfixture.Snapshot()
	batch := reportingv1.Batch{Version: 1, ID: uuid.New().String(), Sessions: []reportingv1.SessionSnapshot{snap}}
	s.now = func() time.Time { return time.UnixMilli(4000) }
	first, err := s.Accept(t.Context(), clients[0], batch)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.UnixMilli(9000) }
	second, err := s.Accept(t.Context(), clients[0], batch)
	if err != nil || first != second {
		t.Fatal("duplicate changed receipt")
	}
	bad := batch
	bad.Sessions = append([]reportingv1.SessionSnapshot(nil), batch.Sessions...)
	bad.Sessions[0].Title = "different"
	if _, err := s.Accept(t.Context(), clients[0], bad); !errors.Is(err, utils.ErrConflict) {
		t.Fatal("batch ID reused with different facts")
	}
	for _, p := range clients[1:] {
		centerfixture.SendSnapshot(t, s, p, snap)
	}
	meta, total := readSession(t, db)
	if total != 100 || meta.Conflict {
		t.Fatal("copies summed or conflicted")
	}
	var sources, projects int64
	_ = db.Model(&reporting_do.SessionSource{}).Count(&sources).Error
	_ = db.Model(&reporting_do.Project{}).Count(&projects).Error
	if sources != 3 || projects != 3 {
		t.Fatal("provenance or same-name projects collapsed")
	}
	snap.Revision = 2
	snap.Contributions = append(snap.Contributions, centerfixture.Contribution(25, 2000))
	snap.CollectedAtMS = 5000
	centerfixture.SendSnapshot(t, s, clients[1], snap)
	meta, total = readSession(t, db)
	if total != 125 || meta.Conflict {
		t.Fatal("growth did not replace canonical")
	}
	c := &snap.Contributions[0]
	c.CostMicroUSD = new(int64(12))
	c.PricingVersion = new("historical-v1")
	c.PricingMode = "event_cost"
	c.CostStatus = "known"
	snap.Revision = 3
	centerfixture.SendSnapshot(t, s, clients[1], snap)
	meta, total = readSession(t, db)
	var cost int64
	_ = db.Model(&reporting_do.Usage{}).Select("COALESCE(SUM(cost_micro_usd),0)").Scan(&cost).Error
	if total != 125 || cost != 12 {
		t.Fatal("price revision duplicated facts or lost evidence")
	}
}
func TestReportingConflictsKeepAcceptedAndCompleteCorrectionsDoNotResurrect(t *testing.T) {
	s, db, clients := reportingFixture(t)
	snap := centerfixture.Snapshot()
	snap.Contributions = append(snap.Contributions, centerfixture.Contribution(25, 2000))
	centerfixture.SendSnapshot(t, s, clients[0], snap)
	centerfixture.SendSnapshot(t, s, clients[1], snap)
	incompatible := centerfixture.Snapshot()
	incompatible.Contributions = []reportingv1.Contribution{centerfixture.Contribution(999, 1000)}
	centerfixture.SendSnapshot(t, s, clients[2], incompatible)
	meta, total := readSession(t, db)
	if !meta.Conflict || total != 125 {
		t.Fatal("incomparable copy took maximum or added")
	}
	// Partial source loss preserves the previously accepted snapshot.
	snap.Revision = 2
	snap.Complete = false
	snap.Contributions = snap.Contributions[:1]
	centerfixture.SendSnapshot(t, s, clients[0], snap)
	meta, total = readSession(t, db)
	if total != 125 || meta.Complete || !meta.Conflict {
		t.Fatal("partial revision erased accepted facts")
	}
	// A complete, newer revision of the canonical source authoritatively removes a fact.
	snap.Revision = 3
	snap.Complete = true
	centerfixture.SendSnapshot(t, s, clients[0], snap)
	meta, total = readSession(t, db)
	if total != 100 || !meta.CorrectionFence || !meta.Conflict {
		t.Fatal("complete correction resurrected old copy")
	}
	stale := centerfixture.Snapshot()
	stale.Revision = 2
	stale.Contributions = append(stale.Contributions, centerfixture.Contribution(25, 2000))
	centerfixture.SendSnapshot(t, s, clients[1], stale)
	_, total = readSession(t, db)
	if total != 100 {
		t.Fatal("later stale copy restored retired contribution")
	}
	stale.Revision = 3
	stale.Contributions = stale.Contributions[:1]
	centerfixture.SendSnapshot(t, s, clients[1], stale)
	incompatible.Revision = 2
	incompatible.Contributions = snap.Contributions
	centerfixture.SendSnapshot(t, s, clients[2], incompatible)
	meta, total = readSession(t, db)
	if total != 100 || meta.Conflict {
		t.Fatal("consistent corrections did not resolve conflict")
	}
}
func TestReportingRollbackIdentityChecksDeletionAndConcurrentCopies(t *testing.T) {
	s, db, clients := reportingFixture(t)
	snap := centerfixture.Snapshot()
	broken := reportingv1.Batch{Version: 1, ID: uuid.New().String(), Sessions: []reportingv1.SessionSnapshot{snap}, Quotas: []reportingv1.QuotaObservation{{Provider: "codex", ID: "q", LocalScope: "unconfirmed", AccountID: new("other-account"), LimitID: "codex", WindowKind: "primary", ObservedAtMS: 1000, Validity: "unknown", Source: "app_server", HistoryOrigin: "confirmed"}}}
	if _, err := s.Accept(t.Context(), clients[0], broken); !errors.Is(err, utils.ErrBadParamInput) {
		t.Fatal("unproven raw account accepted")
	}
	for _, model := range []any{&reporting_do.Batch{}, &reporting_do.Session{}, &reporting_do.SessionSource{}, &reporting_do.Usage{}} {
		var count int64
		_ = db.Model(model).Count(&count).Error
		if count != 0 {
			t.Fatal("failed batch partly committed")
		}
	}
	broken.Quotas = nil
	broken.Sessions[0].Contributions = append([]reportingv1.Contribution(nil), snap.Contributions...)
	broken.Sessions[0].Contributions[0].ID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := s.Accept(t.Context(), clients[0], broken); !errors.Is(err, utils.ErrBadParamInput) {
		t.Fatal("forged contribution ID")
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(clients))
	for _, p := range clients {
		wg.Go(func() {
			_, err := s.Accept(t.Context(), p, reportingv1.Batch{Version: 1, ID: uuid.New().String(), Sessions: []reportingv1.SessionSnapshot{snap}})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	_, total := readSession(t, db)
	if total != 100 {
		t.Fatal("concurrent copies summed")
	}
	for _, p := range clients {
		deleted := snap
		deleted.Revision = 2
		deleted.Deleted = true
		deleted.Contributions = nil
		centerfixture.SendSnapshot(t, s, p, deleted)
	}
	meta, total := readSession(t, db)
	if !meta.Deleted || total != 100 {
		t.Fatal("source tombstone physically removed historical evidence")
	}
}
func TestReportingAccountScopeIsolationAndPendingHistory(t *testing.T) {
	s, db, clients := reportingFixture(t)
	q := reportingv1.QuotaObservation{Provider: "codex", ID: "q", LocalScope: "scope-a", LimitID: "codex", WindowKind: "primary", WindowMinutes: new(int64(300)), ResetsAtMS: new(int64(999999)), ObservedAtMS: 1000, UsedPercent: new(0.0), Validity: "accepted", Source: "app_server", HistoryOrigin: "pending_association"}
	_, err := s.Accept(t.Context(), clients[0], reportingv1.Batch{Version: 1, ID: uuid.New().String(), Quotas: []reportingv1.QuotaObservation{q}})
	if err != nil {
		t.Fatal(err)
	}
	var observation reporting_do.QuotaObservation
	_ = db.First(&observation).Error
	if observation.AccountKey != nil || observation.UsedPercent == nil || *observation.UsedPercent != 0 {
		t.Fatal("unknown account or real zero lost")
	}
	binding := reportingv1.AccountBinding{Provider: "codex", LocalScope: "scope-a", AccountID: "account-a", ConfirmedAtMS: 2000}
	_, err = s.Accept(t.Context(), clients[1], reportingv1.Batch{Version: 1, ID: uuid.New().String(), Bindings: []reportingv1.AccountBinding{binding}})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.First(&observation).Error
	if observation.AccountKey != nil {
		t.Fatal("other device scope associated history")
	}
	_, err = s.Accept(t.Context(), clients[0], reportingv1.Batch{Version: 1, ID: uuid.New().String(), Bindings: []reportingv1.AccountBinding{binding}})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.First(&observation).Error
	if observation.AccountKey == nil || *observation.AccountKey != reportingv1.Key("codex", "account-a") {
		t.Fatal("verified local scope did not resolve history")
	}
	bad := binding
	bad.AccountID = "account-b"
	_, err = s.Accept(t.Context(), clients[0], reportingv1.Batch{Version: 1, ID: uuid.New().String(), Bindings: []reportingv1.AccountBinding{bad}})
	if !errors.Is(err, utils.ErrConflict) {
		t.Fatal("scope rebound to another account")
	}
	q.ID = "legacy"
	q.HistoryOrigin = "legacy_unassigned"
	_, err = s.Accept(t.Context(), clients[0], reportingv1.Batch{Version: 1, ID: uuid.New().String(), Quotas: []reportingv1.QuotaObservation{q}})
	if err != nil {
		t.Fatal(err)
	}
	var legacy reporting_do.QuotaObservation
	_ = db.Where("observation_id = ?", "legacy").First(&legacy).Error
	if legacy.AccountKey != nil {
		t.Fatal("legacy history auto-promoted")
	}
	q.ID = "claimed-b"
	q.AccountID = new("account-b")
	q.HistoryOrigin = "confirmed"
	_, err = s.Accept(t.Context(), clients[0], reportingv1.Batch{Version: 1, ID: uuid.New().String(), Quotas: []reportingv1.QuotaObservation{q}})
	if !errors.Is(err, utils.ErrConflict) {
		t.Fatal("old queue mixed into new account")
	}
}

func TestReportingCreditsPendingBindingAndSixDigitPercent(t *testing.T) {
	s, db, clients := reportingFixture(t)
	credit := reportingv1.ResetCredits{Provider: "codex", ID: "credit-observation", LocalScope: "scope", ObservedAtMS: 1000, Inventory: new(int64(0)), Status: "accepted"}
	q := reportingv1.QuotaObservation{Provider: "codex", ID: "fraction", LocalScope: "scope", LimitID: "codex", WindowKind: "primary", ObservedAtMS: 1000, UsedPercent: new(23.12345678), Validity: "unknown", Source: "app_server", HistoryOrigin: "pending_association"}
	_, err := s.Accept(t.Context(), clients[0], reportingv1.Batch{Version: 1, ID: uuid.New().String(), Credits: []reportingv1.ResetCredits{credit}, Quotas: []reportingv1.QuotaObservation{q}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Accept(t.Context(), clients[0], reportingv1.Batch{Version: 1, ID: uuid.New().String(), Bindings: []reportingv1.AccountBinding{{Provider: "codex", LocalScope: "scope", AccountID: "account", ConfirmedAtMS: 2000}}})
	if err != nil {
		t.Fatal(err)
	}
	var row reporting_do.ResetCredits
	_ = db.First(&row).Error
	if row.AccountKey == nil || row.Inventory == nil || *row.Inventory != 0 {
		t.Fatal("credits binding or real zero lost")
	}
	var observation reporting_do.QuotaObservation
	_ = db.First(&observation).Error
	if observation.UsedPercent == nil || *observation.UsedPercent != 23.123457 {
		t.Fatal("SQLite/MySQL percent precision differs")
	}
}

func TestLegacyQuotaLinkUnlinkAndCreditsPreserveOriginalFacts(t *testing.T) {
	s, db, clients := reportingFixture(t)
	ctx := t.Context()
	binding := reportingv1.AccountBinding{Provider: "codex", LocalScope: "proof-a", AccountID: "raw-a", ConfirmedAtMS: 2000}
	send := func(client access_dto.Principal, b reportingv1.Batch) error {
		b.Version, b.ID = 1, uuid.New().String()
		_, err := s.Accept(ctx, client, b)
		return err
	}
	if err := send(clients[1], reportingv1.Batch{Bindings: []reportingv1.AccountBinding{binding}}); err != nil {
		t.Fatal(err)
	}
	q := reportingv1.QuotaObservation{Provider: "codex", ID: "legacy-link", LocalScope: "default", AssociationScope: new("proof-a"), AccountID: new("raw-a"), LimitID: "codex", WindowKind: "primary", WindowMinutes: new(int64(300)), ResetsAtMS: new(int64(18000000)), ObservedAtMS: 1000, UsedPercent: new(0.0), Validity: "accepted", Source: "legacy_wham", HistoryOrigin: "linked_history"}
	if err := send(clients[0], reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{q}}); !errors.Is(err, utils.ErrBadParamInput) {
		t.Fatal("other device proof accepted", err)
	}
	if err := send(clients[0], reportingv1.Batch{Bindings: []reportingv1.AccountBinding{binding}, Quotas: []reportingv1.QuotaObservation{q}}); err != nil {
		t.Fatal(err)
	}
	var row reporting_do.QuotaObservation
	if err := db.Where("observation_id = ?", q.ID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	firstReceipt := row.ReceivedAtMS
	if row.AccountKey == nil || row.ObservedAtMS != 1000 || row.LocalScope != "default" {
		t.Fatal("link changed original fact")
	}
	q.AccountID, q.AssociationScope, q.HistoryOrigin = nil, nil, "legacy_unassigned"
	if err := send(clients[0], reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{q}}); err != nil {
		t.Fatal(err)
	}
	row = reporting_do.QuotaObservation{}
	if err := db.Where("observation_id = ?", q.ID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.AccountKey != nil || row.ReceivedAtMS != firstReceipt {
		t.Fatal("unlink did not preserve receipt/history")
	}
	c := reportingv1.ResetCredits{Provider: "codex", ID: "expiry-fact", LocalScope: "proof-a", AccountID: new("raw-a"), ObservedAtMS: 1000, Inventory: new(int64(2)), Status: "accepted", DetailsStatus: "complete", NextExpiresAtMS: new(int64(5000)), ExpirySchedule: []reportingv1.CreditExpiry{{ExpiresAtMS: new(int64(5000)), Count: 2}}}
	if err := send(clients[0], reportingv1.Batch{Credits: []reportingv1.ResetCredits{c}}); err != nil {
		t.Fatal(err)
	}
	var credits reporting_do.ResetCredits
	if err := db.Where("id = ?", reportingv1.Key(clients[0].ID, "codex", c.ID)).Take(&credits).Error; err != nil {
		t.Fatal(err)
	}
	if credits.NextResetAtMS != nil || credits.NextExpiresAtMS == nil || *credits.NextExpiresAtMS != 5000 || credits.DetailsStatus != "complete" || credits.ObservedAtMS != 1000 {
		t.Fatal("expiry became reset/current time")
	}
}

func TestReportingProjectAssociationIsExplicitAuthorizedAndReversible(t *testing.T) {
	s, db, clients := reportingFixture(t)
	snap := centerfixture.Snapshot()
	for _, p := range clients {
		centerfixture.SendSnapshot(t, s, p, snap)
	}
	var projects []reporting_do.Project
	if err := db.Order("id").Find(&projects).Error; err != nil {
		t.Fatal(err)
	}
	request := reporting_vo.ProjectAssociationRequest{ProjectIDs: []string{projects[1].ID, projects[2].ID}, TargetID: projects[0].ID}
	if _, err := s.AssociateProjects(t.Context(), clients[0], request); !errors.Is(err, utils.ErrForbidden) {
		t.Fatal("collector associated projects")
	}
	admin := access_dto.Principal{ID: "trusted-admin", Purpose: access_dto.PurposeAdmin}
	if _, err := s.AssociateProjects(t.Context(), admin, request); err != nil {
		t.Fatal(err)
	}
	if err := db.Find(&projects).Error; err != nil {
		t.Fatal(err)
	}
	group := projects[0].GroupID
	for _, p := range projects {
		if p.GroupID != group {
			t.Fatal("explicit group not applied")
		}
	}
	snap.Revision = 2
	snap.ProjectName = "来源名称更新"
	for _, p := range clients {
		centerfixture.SendSnapshot(t, s, p, snap)
	}
	if err := db.Find(&projects).Error; err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		if p.GroupID != group {
			t.Fatal("source update erased explicit association")
		}
	}
	request.TargetID = ""
	if _, err := s.AssociateProjects(t.Context(), admin, request); err != nil {
		t.Fatal(err)
	}
	if err := db.Find(&projects).Error; err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		if p.GroupID != p.ID {
			t.Fatal("unlink did not restore project identity")
		}
	}
}
