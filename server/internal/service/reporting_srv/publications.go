package reporting_srv

import (
	"context"
	"encoding/json/v2"
	"errors"
	"reflect"
	"slices"
	"time"

	"go.uber.org/fx"
	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (s *Reporting) enqueuePublication(ctx context.Context, p access_dto.Principal, batch reportingv1.Batch, hash string, now int64) (bool, error) {
	id := reportingv1.Key(p.ID, batch.ID)
	job, err := s.repository.Job(ctx, id)
	if err == nil {
		if job.Digest != hash {
			return false, utils.ErrConflict
		}
		if job.State == "failed" {
			return false, ErrPublicationFailed
		}
		return true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	part := batch.Sessions[0]
	if part.Chunk != nil {
		err = s.acceptChunk(ctx, p, part, now)
		if !errors.Is(err, ErrPublicationPending) {
			return false, err
		}
	} else {
		previous, e := s.repository.SourceMetadata(ctx, reportingv1.Key(p.ID, part.Provider, part.HomeID, part.SessionID))
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return false, e
		}
		if previous.ID != "" && previous.Revision >= part.Revision {
			if previous.Revision == part.Revision {
				digest, e := sourceDigest(part)
				if e != nil {
					return false, e
				}
				if digest != previous.Digest {
					return false, utils.ErrConflict
				}
			}
			return false, nil
		}
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return false, err
	}
	job = reporting_do.PublicationJob{ID: id, ClientID: p.ID, BatchID: batch.ID, Digest: hash, SourceKey: reportingv1.Key(p.ID, part.Provider, part.HomeID, part.SessionID), Revision: part.Revision, Body: string(body), State: "pending", CreatedAtMS: now}
	return true, s.repository.SaveJob(ctx, job)
}

// StartPublications 是唯一后台 owner；持久租约同时约束重启及多个 Server 实例。
func StartPublications(lifecycle fx.Lifecycle, s *Reporting) {
	var cancel context.CancelFunc
	done := make(chan struct{})
	lifecycle.Append(fx.Hook{OnStart: func(context.Context) error {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go func() {
			defer close(done)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				c, stop := context.WithTimeout(ctx, 4*time.Minute)
				_, err := s.ProcessPublication(c)
				stop()
				if err != nil && ctx.Err() == nil && log.Logger != nil {
					log.Logger.Errorf("snapshot publication failed: %T", err)
				}
				c, stop = context.WithTimeout(ctx, 5*time.Second)
				err = s.repository.CleanupPublication(c)
				stop()
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && ctx.Err() == nil && log.Logger != nil {
					log.Logger.Errorf("snapshot staging cleanup failed: %T", err)
				}
				c, stop = context.WithTimeout(ctx, 5*time.Second)
				err = s.repository.CollectVersions(c)
				stop()
				if err != nil && ctx.Err() == nil && log.Logger != nil {
					log.Logger.Errorf("snapshot version cleanup failed: %T", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
		return nil
	}, OnStop: func(ctx context.Context) error {
		if cancel != nil {
			cancel()
		}
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
}

// ProcessPublication 完整校验和仲裁复用正式逻辑；分批构建后最终只提交小记录与指针。
func (s *Reporting) ProcessPublication(ctx context.Context) (processed bool, err error) {
	job, err := s.repository.ClaimPublication(ctx, time.Now().UnixMilli())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() {
		if err == nil {
			return
		}
		state, reason := "pending", "storage_unavailable"
		if errors.Is(err, utils.ErrBadParamInput) || errors.Is(err, utils.ErrConflict) || errors.Is(err, ErrReportingBudget) {
			state, reason = "failed", "invalid_snapshot"
		}
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if finishErr := s.repository.FinishJob(c, job, state, reason, time.Now().Add(30*time.Second).UnixMilli()); finishErr != nil {
			err = errors.Join(err, finishErr)
		}
	}()
	var batch reportingv1.Batch
	if err = json.Unmarshal([]byte(job.Body), &batch, json.RejectUnknownMembers(true)); err != nil {
		return true, utils.ErrBadParamInput
	}
	if batch.Validate() != nil || batch.ID != job.BatchID || len(batch.Sessions) != 1 || len(batch.Accounts)+len(batch.Bindings)+len(batch.Quotas)+len(batch.Credits)+len(batch.Status)+len(batch.AccountUsage) != 0 {
		return true, utils.ErrBadParamInput
	}
	_, bodyDigest, e := jsonDigest(batch)
	if e != nil {
		return true, e
	}
	if bodyDigest != job.Digest {
		return true, utils.ErrConflict
	}
	snapshot := batch.Sessions[0]
	if snapshot.Chunk != nil {
		rows, e := s.repository.Chunks(ctx, job.ClientID, job.SourceKey, job.Revision)
		if e != nil {
			return true, e
		}
		parts := make([]reportingv1.SessionSnapshot, 0, len(rows))
		for _, row := range rows {
			var part reportingv1.SessionSnapshot
			if json.Unmarshal([]byte(row.Payload), &part, json.RejectUnknownMembers(true)) != nil {
				return true, utils.ErrBadParamInput
			}
			parts = append(parts, part)
		}
		var e2 error
		snapshot, e2 = reportingv1.AssembleSnapshot(parts)
		if e2 != nil {
			return true, utils.ErrBadParamInput
		}
	}
	if snapshot.Chunk != nil || reportingv1.ValidateSnapshot(snapshot) != nil {
		return true, utils.ErrBadParamInput
	}
	if err = validateStableIDs(reportingv1.Batch{Sessions: []reportingv1.SessionSnapshot{snapshot}}); err != nil {
		return true, err
	}
	key := reportingv1.Key(snapshot.Provider, snapshot.SessionID)
	var current reporting_do.Session
	var stored []reporting_do.SessionSource
	var canonical reporting_do.CanonicalSnapshot
	var canonicalFound bool
	var stamp string
	err = s.repository.ReadSnapshot(ctx, func(ctx context.Context) error {
		var e error
		current, e = s.repository.Session(ctx, key)
		if errors.Is(e, gorm.ErrRecordNotFound) {
			current = reporting_do.Session{ID: key, Provider: snapshot.Provider, SessionID: snapshot.SessionID, Deleted: true}
		} else if e != nil {
			return e
		}
		stored, e = s.repository.ReadSources(ctx, key)
		if e != nil {
			return e
		}
		stamp, e = s.repository.SourcesStamp(ctx, key)
		if e != nil {
			return e
		}
		canonical, e = s.repository.Canonical(ctx, key)
		canonicalFound = e == nil
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		return nil
	})
	if err != nil {
		return true, err
	}
	previousIndex := slices.IndexFunc(stored, func(row reporting_do.SessionSource) bool { return row.ID == job.SourceKey })
	hash, err := sourceDigest(snapshot)
	if err != nil {
		return true, err
	}
	stale := false
	if previousIndex >= 0 {
		previous := stored[previousIndex]
		if previous.Revision > snapshot.Revision {
			stale = true
		}
		if previous.Revision == snapshot.Revision {
			if previous.Digest != hash {
				return true, utils.ErrConflict
			}
			stale = true
		}
	}
	var source reporting_do.SessionSource
	var sourceCapsule reporting_do.SessionCapsule
	var manifest, generation string
	// 旧修订只确认；不会回退来源、投影或 correction fence。
	if !stale {
		payload, e := json.Marshal(snapshot)
		if e != nil {
			return true, e
		}
		source = reporting_do.SessionSource{ID: job.SourceKey, SessionKey: key, ClientID: job.ClientID, HomeID: snapshot.HomeID, SourceKind: snapshot.SourceKind, Revision: snapshot.Revision, CollectedAtMS: snapshot.CollectedAtMS, Digest: hash, Payload: string(payload)}
		if previousIndex >= 0 {
			stored[previousIndex] = source
		} else {
			stored = append(stored, source)
		}
		sourceCapsule, err = sessionCapsule("source", source.ID, key, job.ClientID, snapshot)
		if err != nil {
			return true, err
		}
	}
	plan, err := planSession(current, stored, canonical, canonicalFound)
	if err != nil {
		return true, err
	}
	if !stale {
		source.Payload, err = s.repository.StagePayload(ctx, source.Payload, snapshot.Complete, snapshot.Deleted)
		if err != nil {
			return true, err
		}
		if plan.Changed && !plan.FlagsOnly {
			body, e := json.Marshal(plan.Snapshot)
			if e != nil {
				return true, e
			}
			generation = reportingv1.Key("usage-publication", key, string(body))
			manifest, err = s.repository.StagePayload(ctx, string(body), plan.Snapshot.Complete, plan.Snapshot.Deleted)
			if err != nil {
				return true, err
			}
			if err = s.repository.StageUsage(ctx, generation, plan.Usage); err != nil {
				return true, err
			}
		}
	}
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = s.repository.Transaction(c, func(ctx context.Context) error {
		// 与 HTTP 接收统一 client → session 顺序，避免后台发布和新上传互相等锁。
		client, e := s.repository.LockClient(ctx, job.ClientID)
		if e != nil {
			return e
		}
		if client.RevokedAtMS != nil || client.Purpose != access_dto.PurposeCollector || client.ExpiresAtMS != nil && *client.ExpiresAtMS <= time.Now().UnixMilli() {
			return utils.ErrUnauthorized
		}
		locked, e := s.repository.LockSession(ctx, current)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(locked, current) {
			return ErrPublicationPending
		}
		actual, e := s.repository.SourcesStamp(ctx, key)
		if e != nil {
			return e
		}
		if actual != stamp {
			return ErrPublicationPending
		}
		claimed, e := s.repository.Job(ctx, job.ID)
		if e != nil {
			return e
		}
		if claimed.LeaseToken != job.LeaseToken || claimed.LeaseUntilMS < time.Now().UnixMilli() {
			return ErrPublicationPending
		}
		if !stale {
			if e = s.repository.SaveSource(ctx, source); e != nil {
				return e
			}
			if e = s.repository.SaveCapsule(ctx, sourceCapsule); e != nil {
				return e
			}
			projectID := reportingv1.Key(job.ClientID, snapshot.Provider, snapshot.ProjectID)
			project, e := s.repository.Project(ctx, projectID)
			if errors.Is(e, gorm.ErrRecordNotFound) {
				project = reporting_do.Project{ID: projectID, GroupID: projectID, ClientID: job.ClientID, Provider: snapshot.Provider, LocalID: snapshot.ProjectID}
			} else if e != nil {
				return e
			}
			if snapshot.CollectedAtMS >= project.UpdatedAtMS {
				project.Name = snapshot.ProjectName
				project.UpdatedAtMS = snapshot.CollectedAtMS
				if e = s.repository.SaveProject(ctx, project); e != nil {
					return e
				}
			}
			if plan.Changed {
				if plan.FlagsOnly {
					e = s.repository.SaveSessionFlags(ctx, plan.Session)
				} else {
					if e = s.repository.SaveCapsule(ctx, plan.Capsule); e == nil {
						e = s.repository.Publish(ctx, plan.Session, generation, manifest)
					}
				}
				if e != nil {
					return e
				}
			}
		}
		received := time.Now().UnixMilli()
		if e = s.repository.CommitBatch(ctx, reporting_do.Batch{ClientID: job.ClientID, BatchID: job.BatchID, Digest: job.Digest, ReceivedAtMS: received}); e != nil {
			return e
		}
		return s.repository.FinishJob(ctx, job, "published", "", 0)
	})
	return true, err
}
