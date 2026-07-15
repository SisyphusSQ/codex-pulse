package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var ErrLiveScanConflict = errors.New("live scan job conflicts with durable identity")

// CreateLiveScanJob 原子创建公共job与typed live action facts。
func (repository *Repository) CreateLiveScanJob(
	ctx context.Context,
	job JobRun,
	facts LiveScanJob,
) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateLiveScanCreate(job, facts); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		existingJob, jobFound, err := jobRunByID(ctx, transaction, job.JobID)
		if err != nil {
			return err
		}
		existingFacts, factsFound, err := liveScanJobByID(ctx, transaction, facts.JobID)
		if err != nil {
			return err
		}
		if jobFound || factsFound {
			if jobFound && factsFound && jobRunsEqual(existingJob, job) && liveScanJobsEqual(existingFacts, facts) {
				return nil
			}
			return ErrLiveScanConflict
		}
		var requestCount int64
		if err := transaction.WithContext(ctx).Model(&liveScanJobModel{}).
			Where("request_id = ?", facts.RequestID).Count(&requestCount).Error; err != nil {
			return err
		}
		if requestCount > 0 {
			return ErrLiveScanConflict
		}
		if err := createJobRun(ctx, transaction, job); err != nil {
			return err
		}
		model := liveScanJobModelFromDomain(facts)
		return transaction.WithContext(ctx).Create(&model).Error
	})
}

func (repository *Repository) LiveScanRun(
	ctx context.Context,
	jobID string,
) (JobRun, LiveScanJob, error) {
	if repository == nil || repository.database == nil || jobID == "" {
		return JobRun{}, LiveScanJob{}, ErrInvalidRepository
	}
	var job JobRun
	var facts LiveScanJob
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		value, found, err := jobRunByID(ctx, connection, jobID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		job = value
		factsValue, found, err := liveScanJobByID(ctx, connection, jobID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		facts = factsValue
		return nil
	})
	return job, facts, err
}

// ResumeLiveScanJob 原子克隆interrupted live action到新的公共job attempt。
func (repository *Repository) ResumeLiveScanJob(
	ctx context.Context,
	oldJobID string,
	resumed JobRun,
	facts LiveScanJob,
) error {
	if repository == nil || repository.database == nil || oldJobID == "" {
		return ErrInvalidRepository
	}
	if err := validateLiveScanCreate(resumed, facts); err != nil {
		return err
	}
	if resumed.ResumeOfJobID == nil || *resumed.ResumeOfJobID != oldJobID {
		return ErrLiveScanConflict
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if existingJob, found, err := jobRunByID(ctx, transaction, resumed.JobID); err != nil {
			return err
		} else if found {
			existingFacts, factsFound, readErr := liveScanJobByID(ctx, transaction, resumed.JobID)
			if readErr != nil {
				return readErr
			}
			if factsFound && jobRunsEqual(existingJob, resumed) && liveScanJobsEqual(existingFacts, facts) {
				return nil
			}
			return ErrLiveScanConflict
		}
		oldJob, found, err := jobRunByID(ctx, transaction, oldJobID)
		if err != nil {
			return err
		}
		oldFacts, factsFound, err := liveScanJobByID(ctx, transaction, oldJobID)
		if err != nil {
			return err
		}
		if !found || !factsFound || oldJob.State != JobInterrupted ||
			resumed.JobType != oldJob.JobType || resumed.RequestedBy != oldJob.RequestedBy ||
			resumed.Priority != oldJob.Priority || resumed.Phase != oldJob.Phase ||
			resumed.CreatedAtMS < oldJob.UpdatedAtMS ||
			facts.HomeGeneration != oldFacts.HomeGeneration || facts.HomePath != oldFacts.HomePath ||
			facts.HomeDeviceID != oldFacts.HomeDeviceID || facts.HomeInode != oldFacts.HomeInode ||
			facts.ActionKind != oldFacts.ActionKind ||
			!equalSourceFingerprintPointer(facts.Previous, oldFacts.Previous) || facts.Current != oldFacts.Current {
			return ErrLiveScanConflict
		}
		if err := createJobRun(ctx, transaction, resumed); err != nil {
			return err
		}
		model := liveScanJobModelFromDomain(facts)
		return transaction.WithContext(ctx).Create(&model).Error
	})
}

func validateLiveScanCreate(job JobRun, facts LiveScanJob) error {
	if err := validateNewJobRun(job); err != nil {
		return err
	}
	if job.JobID != facts.JobID || job.Phase != JobPhaseLive || facts.RequestID == "" ||
		facts.HomeGeneration < 0 || facts.HomeInode <= 0 || facts.UpdatedAtMS != job.UpdatedAtMS ||
		!filepath.IsAbs(facts.HomePath) || filepath.Clean(facts.HomePath) != facts.HomePath ||
		facts.HomeDeviceID == "" || !validLiveScanActionKind(facts.ActionKind) ||
		!validSourceFingerprint(facts.Current) {
		return ErrLiveScanConflict
	}
	if facts.ActionKind == LiveScanActionAdded && facts.Previous != nil ||
		facts.ActionKind != LiveScanActionAdded && (facts.Previous == nil || !validSourceFingerprint(*facts.Previous)) {
		return ErrLiveScanConflict
	}
	for _, path := range []string{facts.Current.CurrentPath} {
		relative, err := filepath.Rel(facts.HomePath, path)
		if err != nil || relative == "." || filepath.IsAbs(relative) ||
			relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return ErrLiveScanConflict
		}
	}
	if facts.Previous != nil {
		relative, err := filepath.Rel(facts.HomePath, facts.Previous.CurrentPath)
		if err != nil || relative == "." || filepath.IsAbs(relative) ||
			relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return ErrLiveScanConflict
		}
	}
	return nil
}

func liveScanJobByID(ctx context.Context, database *gorm.DB, jobID string) (LiveScanJob, bool, error) {
	var model liveScanJobModel
	result := database.WithContext(ctx).Where("job_id = ?", jobID).Take(&model)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return LiveScanJob{}, false, nil
	}
	if result.Error != nil {
		return LiveScanJob{}, false, result.Error
	}
	facts, err := liveScanJobFromModel(model)
	return facts, err == nil, err
}

func liveScanJobModelFromDomain(value LiveScanJob) liveScanJobModel {
	model := liveScanJobModel{
		JobID: value.JobID, RequestID: value.RequestID, HomeGeneration: value.HomeGeneration,
		HomePath: value.HomePath, HomeDeviceID: value.HomeDeviceID, HomeInode: value.HomeInode,
		ActionKind: string(value.ActionKind), CurrentSourceID: value.Current.SourceFileID,
		CurrentKind: value.Current.SourceKind, CurrentPath: value.Current.CurrentPath,
		CurrentDeviceID: value.Current.DeviceID, CurrentInode: value.Current.Inode,
		CurrentSize: value.Current.SizeBytes, CurrentMTimeNS: value.Current.MTimeNS,
		CurrentPrefixN: value.Current.PrefixBytes, CurrentPrefix: value.Current.PrefixSHA256,
		CurrentDigest: value.Current.FingerprintSHA256, UpdatedAtMS: value.UpdatedAtMS,
	}
	if value.Previous != nil {
		model.PreviousSourceID = pointerToValue(value.Previous.SourceFileID)
		model.PreviousKind = pointerToValue(value.Previous.SourceKind)
		model.PreviousPath = pointerToValue(value.Previous.CurrentPath)
		model.PreviousDeviceID = pointerToValue(value.Previous.DeviceID)
		model.PreviousInode = pointerToValue(value.Previous.Inode)
		model.PreviousSize = pointerToValue(value.Previous.SizeBytes)
		model.PreviousMTimeNS = pointerToValue(value.Previous.MTimeNS)
		model.PreviousPrefixN = pointerToValue(value.Previous.PrefixBytes)
		model.PreviousPrefix = pointerToValue(value.Previous.PrefixSHA256)
		model.PreviousDigest = pointerToValue(value.Previous.FingerprintSHA256)
	}
	return model
}

func liveScanJobFromModel(model liveScanJobModel) (LiveScanJob, error) {
	previous, err := sourceFingerprintFromBootstrapColumns(
		model.PreviousSourceID, model.PreviousKind, model.PreviousPath, model.PreviousDeviceID,
		model.PreviousInode, model.PreviousSize, model.PreviousMTimeNS, model.PreviousPrefixN,
		model.PreviousPrefix, model.PreviousDigest,
	)
	if err != nil {
		return LiveScanJob{}, err
	}
	current := SourceFingerprint{
		SourceFileID: model.CurrentSourceID, Provider: "codex", SourceKind: model.CurrentKind,
		CurrentPath: model.CurrentPath, DeviceID: model.CurrentDeviceID, Inode: model.CurrentInode,
		SizeBytes: model.CurrentSize, MTimeNS: model.CurrentMTimeNS, PrefixBytes: model.CurrentPrefixN,
		PrefixSHA256: model.CurrentPrefix, FingerprintSHA256: model.CurrentDigest,
	}
	value := LiveScanJob{
		JobID: model.JobID, RequestID: model.RequestID, HomeGeneration: model.HomeGeneration,
		HomePath: model.HomePath, HomeDeviceID: model.HomeDeviceID, HomeInode: model.HomeInode,
		ActionKind: LiveScanActionKind(model.ActionKind), Previous: previous, Current: current,
		UpdatedAtMS: model.UpdatedAtMS,
	}
	if !validSourceFingerprint(current) || !validLiveScanActionKind(value.ActionKind) {
		return LiveScanJob{}, ErrLiveScanConflict
	}
	return value, nil
}

func liveScanJobsEqual(left, right LiveScanJob) bool {
	return left.JobID == right.JobID && left.RequestID == right.RequestID &&
		left.HomeGeneration == right.HomeGeneration && left.HomePath == right.HomePath &&
		left.HomeDeviceID == right.HomeDeviceID && left.HomeInode == right.HomeInode &&
		left.ActionKind == right.ActionKind && equalSourceFingerprintPointer(left.Previous, right.Previous) &&
		left.Current == right.Current && left.UpdatedAtMS == right.UpdatedAtMS
}
