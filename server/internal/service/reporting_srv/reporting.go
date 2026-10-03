package reporting_srv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"math"
	"slices"
	"strings"
	"time"
	"uuid"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	reporting_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/reporting_vo"
	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

var ErrReportingVersion = errors.New("reporting version unsupported")
var ErrReportingBudget = errors.New("reporting merge budget exceeded")

// Reporting 接收真实身份的原子批次，仲裁多个采集副本并保留来源证据。
type Reporting struct {
	repository *reporting_repo.Reporting
	now        func() time.Time
}

func NewReporting(repository *reporting_repo.Reporting) *Reporting {
	return &Reporting{repository: repository, now: time.Now}
}
func requireCollector(p access_dto.Principal) error {
	if p.ID == "" || p.Purpose != access_dto.PurposeCollector {
		return utils.ErrForbidden
	}
	return nil
}
func jsonDigest(value any) (string, string, error) {
	bytes, err := json.Marshal(value)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(bytes)
	return string(bytes), hex.EncodeToString(sum[:]), nil
}
func sourceDigest(s reportingv1.SessionSnapshot) (string, error) {
	s.Revision = 0
	s.CollectedAtMS = 0
	_, hash, err := jsonDigest(s)
	return hash, err
}
func validateStableIDs(batch reportingv1.Batch) error {
	for _, s := range batch.Sessions {
		if s.Chunk != nil {
			continue
		}
		facts := make(map[string]int64)
		calls := make(map[string]int64)
		for _, c := range s.Contributions {
			key := reportingv1.ContributionID(s.Provider, s.SessionID, c, 0)
			if c.ID != reportingv1.ContributionID(s.Provider, s.SessionID, c, facts[key]) {
				return utils.ErrBadParamInput
			}
			facts[key]++
		}
		for _, i := range s.Invocations {
			key := reportingv1.InvocationID(s.Provider, s.SessionID, i, 0)
			if i.ID != reportingv1.InvocationID(s.Provider, s.SessionID, i, calls[key]) {
				return utils.ErrBadParamInput
			}
			calls[key]++
		}
	}
	return nil
}
func (s *Reporting) Accept(ctx context.Context, p access_dto.Principal, batch reportingv1.Batch) (receipt reporting_vo.ReportingReceiptView, err error) {
	if err = requireCollector(p); err != nil {
		return
	}
	if err = batch.Validate(); err != nil {
		if errors.Is(err, reportingv1.ErrVersion) {
			err = ErrReportingVersion
		} else {
			err = utils.ErrBadParamInput
		}
		return
	}
	if _, err = uuid.Parse(batch.ID); err != nil {
		return receipt, utils.ErrBadParamInput
	}
	if err = validateStableIDs(batch); err != nil {
		return
	}
	_, hash, err := jsonDigest(batch)
	if err != nil {
		return receipt, err
	}
	now := s.now().UnixMilli()
	receipt = reporting_vo.ReportingReceiptView{Version: reportingv1.Version, BatchID: batch.ID, ReceivedAtMS: now}
	err = s.repository.Transaction(ctx, func(ctx context.Context) error {
		client, err := s.repository.LockClient(ctx, p.ID)
		if err != nil {
			return err
		}
		if client.Purpose != access_dto.PurposeCollector || client.RevokedAtMS != nil || (client.ExpiresAtMS != nil && *client.ExpiresAtMS <= now) {
			return utils.ErrUnauthorized
		}
		previous, err := s.repository.Batch(ctx, p.ID, batch.ID)
		if err == nil {
			if previous.Digest != hash {
				return utils.ErrConflict
			}
			receipt.ReceivedAtMS = previous.ReceivedAtMS
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		snapshots := slices.Clone(batch.Sessions)
		slices.SortFunc(snapshots, func(a, b reportingv1.SessionSnapshot) int {
			return strings.Compare(reportingv1.Key(a.Provider, a.SessionID), reportingv1.Key(b.Provider, b.SessionID))
		})
		for _, snapshot := range snapshots {
			if snapshot.Chunk != nil {
				if err := s.acceptChunk(ctx, p, snapshot, now); err != nil {
					return err
				}
				continue
			}
			if err := s.acceptSession(ctx, p, snapshot); err != nil {
				return err
			}
		}
		if err := s.acceptAccountFacts(ctx, p, batch, now); err != nil {
			return err
		}
		for _, status := range batch.Status {
			if status.SyncCheckedAtMS != nil {
				if err := s.repository.SaveSync(ctx, reporting_do.DeviceSync{ClientID: p.ID, Provider: status.Provider, SyncState: status.SyncState, SyncCheckedAtMS: *status.SyncCheckedAtMS, FullSyncState: status.FullSyncState}); err != nil {
					return err
				}
			}
			previous, err := s.repository.DeviceStatus(ctx, p.ID, status.Provider)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && previous.CollectedAtMS != nil && status.CollectedAtMS != nil && *status.CollectedAtMS < *previous.CollectedAtMS {
				continue
			}
			row := reporting_do.DeviceStatus{ClientID: p.ID, Provider: status.Provider, Version: status.Version, CollectedAtMS: status.CollectedAtMS, CoverageStartMS: status.CoverageStartMS, CoverageEndMS: status.CoverageEndMS, PendingBatches: status.PendingBatches, Status: status.Status, ReceivedAtMS: now}
			if err := s.repository.SaveStatus(ctx, row); err != nil {
				return err
			}
		}
		return s.repository.CommitBatch(ctx, reporting_do.Batch{ClientID: p.ID, BatchID: batch.ID, Digest: hash, ReceivedAtMS: now})
	})
	return
}
func (s *Reporting) Sync(ctx context.Context, p access_dto.Principal) (reporting_vo.SyncView, error) {
	if err := requireCollector(p); err != nil {
		return reporting_vo.SyncView{}, err
	}
	client, batches, err := s.repository.Sync(ctx, p.ID)
	if err != nil {
		return reporting_vo.SyncView{}, err
	}
	if client.RevokedAtMS != nil {
		return reporting_vo.SyncView{}, utils.ErrUnauthorized
	}
	return reporting_vo.SyncView{ProtocolVersion: reportingv1.Version, LastReceivedAtMS: client.LastReceivedAtMS, Batches: batches}, nil
}
func (s *Reporting) acceptSession(ctx context.Context, p access_dto.Principal, snapshot reportingv1.SessionSnapshot) error {
	sessionKey := reportingv1.Key(snapshot.Provider, snapshot.SessionID)
	current, err := s.repository.LockSession(ctx, reporting_do.Session{ID: sessionKey, Provider: snapshot.Provider, SessionID: snapshot.SessionID, Deleted: true})
	if err != nil {
		return err
	}
	id := reportingv1.Key(p.ID, snapshot.Provider, snapshot.HomeID, snapshot.SessionID)
	previous, err := s.repository.Source(ctx, id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	hash, err := sourceDigest(snapshot)
	if err != nil {
		return err
	}
	if previous.ID != "" {
		if snapshot.Revision < previous.Revision {
			return nil
		}
		if snapshot.Revision == previous.Revision {
			if previous.Digest != hash {
				return utils.ErrConflict
			}
			return nil
		}
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if err := s.repository.SaveSource(ctx, reporting_do.SessionSource{ID: id, SessionKey: sessionKey, ClientID: p.ID, HomeID: snapshot.HomeID, SourceKind: snapshot.SourceKind, Revision: snapshot.Revision, CollectedAtMS: snapshot.CollectedAtMS, Digest: hash, Payload: string(payload)}); err != nil {
		return err
	}
	capsule, err := sessionCapsule("source", id, sessionKey, p.ID, snapshot)
	if err != nil {
		return err
	}
	if err = s.repository.SaveCapsule(ctx, capsule); err != nil {
		return err
	}
	projectID := reportingv1.Key(p.ID, snapshot.Provider, snapshot.ProjectID)
	project, err := s.repository.Project(ctx, projectID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if project.ID == "" {
		project = reporting_do.Project{ID: projectID, GroupID: projectID, ClientID: p.ID, Provider: snapshot.Provider, LocalID: snapshot.ProjectID}
	}
	if snapshot.CollectedAtMS >= project.UpdatedAtMS {
		project.Name = snapshot.ProjectName
		project.UpdatedAtMS = snapshot.CollectedAtMS
		if err := s.repository.SaveProject(ctx, project); err != nil {
			return err
		}
	}
	stored, err := s.repository.Sources(ctx, sessionKey)
	if err != nil {
		return err
	}
	if len(stored) > 128 {
		return ErrReportingBudget
	}
	var sources []reporting_dto.SourceSnapshot
	size := 0
	for _, row := range stored {
		size += len(row.Payload)
		if size > 64<<20 {
			return ErrReportingBudget
		}
		var snap reportingv1.SessionSnapshot
		if err := json.Unmarshal([]byte(row.Payload), &snap, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		// 旧不可变队列保留原始摘要用于重试；调用事实不再进入业务仲裁。
		snap.Invocations = nil
		sources = append(sources, reporting_dto.SourceSnapshot{ID: row.ID, ClientID: row.ClientID, Snapshot: snap})
	}
	var accepted *reporting_dto.SourceSnapshot
	old, err := s.repository.Canonical(ctx, sessionKey)
	if err == nil {
		var snap reportingv1.SessionSnapshot
		if err := json.Unmarshal([]byte(old.Payload), &snap, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		snap.Invocations = nil
		for _, row := range stored {
			if row.ID == current.CanonicalSourceID {
				accepted = &reporting_dto.SourceSnapshot{ID: row.ID, ClientID: row.ClientID, Snapshot: snap, CorrectionFence: current.CorrectionFence}
				break
			}
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	decision := DecideSnapshot(sources, accepted)
	if decision.Deleted {
		current.Deleted = true
		current.Conflict = false
		return s.repository.SaveSessionFlags(ctx, current)
	}
	chosen := decision.Source.Snapshot
	encoded, err := json.Marshal(chosen)
	if err != nil {
		return err
	}
	if len(encoded) > 64<<20 || len(chosen.Contributions)+len(chosen.Invocations) > 200000 {
		return ErrReportingBudget
	}
	selectedProject := reportingv1.Key(decision.Source.ClientID, chosen.Provider, chosen.ProjectID)
	meta := reporting_do.Session{ID: sessionKey, Provider: chosen.Provider, SessionID: chosen.SessionID, Title: chosen.Title, ProjectID: selectedProject, SourceKind: chosen.SourceKind, SessionKind: chosen.SessionKind, HistoryStartAtMS: chosen.HistoryStartAtMS, CanonicalSourceID: decision.Source.ID, CanonicalRevision: chosen.Revision, CreatedAtMS: chosen.CreatedAtMS, LastActiveAtMS: chosen.LastActiveAtMS, CollectedAtMS: chosen.CollectedAtMS, Complete: chosen.Complete, Conflict: decision.Conflict, CorrectionFence: decision.CorrectionFence}
	usage := make([]reporting_do.Usage, 0, len(chosen.Contributions))
	for position, c := range chosen.Contributions {
		row := reporting_do.Usage{SessionKey: sessionKey, ContributionID: c.ID, Position: int64(position), ObservedAtMS: c.ObservedAtMS, Model: c.Model, InputTokens: c.InputTokens, CachedTokens: c.CachedTokens, CacheWriteTokens: c.CacheWriteTokens, OutputTokens: c.OutputTokens, ReasoningTokens: c.ReasoningTokens, TotalTokens: c.TotalTokens, CostMicroUSD: c.CostMicroUSD, ReportedChargeMicroUSD: c.ReportedChargeMicroUSD, PricingVersion: c.PricingVersion, PricingMode: c.PricingMode, CostStatus: c.CostStatus}
		if c.Rates != nil {
			row.InputPrice = c.Rates.InputMicroUSD
			row.CachedPrice = c.Rates.CachedMicroUSD
			row.CacheWritePrice = c.Rates.CacheWriteMicroUSD
			row.OutputPrice = c.Rates.OutputMicroUSD
		}
		usage = append(usage, row)
	}
	capsule, err = sessionCapsule("canonical", sessionKey, sessionKey, "", chosen)
	if err != nil {
		return err
	}
	if err = s.repository.SaveCapsule(ctx, capsule); err != nil {
		return err
	}
	return s.repository.SaveCanonical(ctx, meta, reporting_do.CanonicalSnapshot{SessionKey: sessionKey, Payload: string(encoded)}, usage)
}
func normalizedPercent(value *float64) *float64 {
	if value == nil {
		return nil
	}
	return new(math.Round(*value*1e6) / 1e6)
}

// AssociateProjects 仅允许已授权管理浏览器执行显式关联，名称相同不触发合并。
func (s *Reporting) AssociateProjects(ctx context.Context, p access_dto.Principal, request reporting_vo.ProjectAssociationRequest) (vo.MutationView, error) {
	if err := access_srv.RequireAdmin(p); err != nil {
		return vo.MutationView{}, err
	}
	if len(request.ProjectIDs) == 0 || len(request.ProjectIDs) > 100 {
		return vo.MutationView{}, utils.ErrBadParamInput
	}
	ids := slices.Clone(request.ProjectIDs)
	if request.TargetID != "" {
		ids = append(ids, request.TargetID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	for _, id := range ids {
		if len(id) != 64 {
			return vo.MutationView{}, utils.ErrBadParamInput
		}
		if _, err := hex.DecodeString(id); err != nil {
			return vo.MutationView{}, utils.ErrBadParamInput
		}
	}
	err := s.repository.Transaction(ctx, func(ctx context.Context) error {
		projects, err := s.repository.LockProjects(ctx, ids)
		if err != nil {
			return err
		}
		if len(projects) != len(ids) {
			return utils.ErrNotFound
		}
		group := request.TargetID
		if group != "" {
			for _, row := range projects {
				if row.ID == group {
					group = row.GroupID
					break
				}
			}
		}
		for _, id := range request.ProjectIDs {
			selected := group
			if selected == "" {
				selected = id
			}
			if err := s.repository.AssociateProject(ctx, id, selected); err != nil {
				return err
			}
		}
		return nil
	})
	return vo.MutationView{Applied: err == nil}, err
}
