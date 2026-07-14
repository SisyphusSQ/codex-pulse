package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const (
	repairProgressConfirmed int64 = 0
	repairProgressDatabase  int64 = 1
	repairProgressIndex     int64 = 2
	repairProgressAppended  int64 = 3
	repairProgressTotal     int64 = 4
	repairJobTypePrefix           = "session_index_repair:"
	repairRequestedBy             = "explicit_confirmation"
)

type repairRepository interface {
	ListSessionIndexExpectations(context.Context) ([]factstore.SessionIndexExpectation, error)
	CreateJobRun(context.Context, factstore.JobRun) error
	TransitionJobRun(context.Context, factstore.JobTransition) error
	CompleteSessionIndexRepairJob(
		context.Context,
		[]factstore.SessionIndexExpectation,
		factstore.JobTransition,
	) error
	JobRun(context.Context, string) (factstore.JobRun, error)
}

type databaseBackuper interface {
	Config() storesqlite.Config
	Backup(context.Context, storesqlite.BackupOptions) (storesqlite.BackupReport, error)
}

type serviceHooks struct {
	beforeAppend func()
	afterAppend  func() error
}

// Service 串联 dry-run、精确确认、双备份、append-only correction 与持久 audit job。
type Service struct {
	repository    repairRepository
	database      databaseBackuper
	indexFile     *IndexFile
	now           func() time.Time
	hooks         serviceHooks
	syncDirectory func(string) error
}

// NewService 使用已经 bootstrap 的 Codex Pulse Store 和用户确认的单一 Codex Home。
func NewService(
	repository *factstore.Repository,
	database *storesqlite.Store,
	confirmedHome string,
) (*Service, error) {
	if repository == nil || database == nil {
		return nil, ErrInvalidPlan
	}
	indexFile, err := OpenIndexFile(confirmedHome)
	if err != nil {
		return nil, err
	}
	return newService(repository, database, indexFile, time.Now, serviceHooks{}), nil
}

func newService(
	repository repairRepository,
	database databaseBackuper,
	indexFile *IndexFile,
	now func() time.Time,
	hooks serviceHooks,
) *Service {
	return &Service{
		repository: repository, database: database, indexFile: indexFile, now: now, hooks: hooks,
		syncDirectory: syncDirectoryPath,
	}
}

// Analyze 只读 Store 和根级 index，返回可校验 plan；不会创建 job 或备份目录。
func (service *Service) Analyze(ctx context.Context) (RepairPlan, error) {
	if err := service.validate(); err != nil {
		return RepairPlan{}, err
	}
	expectations, err := service.loadExpectations(ctx)
	if err != nil {
		return RepairPlan{}, err
	}
	read, err := service.indexFile.Read(ctx)
	if err != nil {
		return RepairPlan{}, err
	}
	parsed, err := Parse(read.Content)
	clear(read.Content)
	if err != nil {
		return RepairPlan{}, err
	}
	return Analyze(parsed, expectations, read.Version, service.now().UnixMilli())
}

// Execute 只接受精确 plan confirmation；所有 repair 写入均发生在双备份成功之后。
func (service *Service) Execute(
	ctx context.Context,
	plan RepairPlan,
	confirmation Confirmation,
) (RepairReport, error) {
	report := RepairReport{PlanID: plan.ID, Actions: len(plan.Actions)}
	if err := service.validate(); err != nil {
		return report, err
	}
	if err := VerifyPlan(plan); err != nil {
		return report, err
	}
	if confirmation.PlanID == "" || confirmation.PlanID != plan.ID {
		return report, ErrConfirmationRequired
	}
	if len(plan.Conflicts) > 0 {
		return report, ErrPlanConflict
	}
	if len(plan.Actions) == 0 {
		if err := service.verifyExpectationSnapshot(ctx, plan.ExpectationsSHA256); err != nil {
			return report, err
		}
		current, err := service.indexFile.Read(ctx)
		if err != nil {
			return report, err
		}
		clear(current.Content)
		if current.Version != plan.Source {
			return report, ErrPlanDrift
		}
		report.Noop = true
		report.FinalVersion = current.Version
		return report, nil
	}

	jobID := repairJobID(plan.ID)
	report.JobID = jobID
	jobType := repairJobTypePrefix + plan.ID
	existing, err := service.repository.JobRun(ctx, jobID)
	switch {
	case err == nil:
		if existing.JobType != jobType || existing.State != factstore.JobSucceeded {
			return report, ErrRepairAlreadyRecorded
		}
		if err := service.verifyExpectationSnapshot(ctx, plan.ExpectationsSHA256); err != nil {
			return report, err
		}
		finalVersion, reconcileErr := service.reconcile(ctx, plan)
		if reconcileErr != nil {
			return report, reconcileErr
		}
		report.Replayed = true
		report.FinalVersion = finalVersion
		report.DatabaseBackupPath, report.IndexBackupPath = service.backupPaths(jobID, plan.Source.Exists)
		return report, nil
	case !errors.Is(err, factstore.ErrNotFound):
		return report, err
	}
	if err := service.verifyExpectationSnapshot(ctx, plan.ExpectationsSHA256); err != nil {
		return report, err
	}
	current, err := service.indexFile.Read(ctx)
	if err != nil {
		return report, err
	}
	clear(current.Content)
	if current.Version != plan.Source {
		return report, ErrPlanDrift
	}

	createdAtMS := plan.AnalyzedAtMS
	progress := repairProgressConfirmed
	total := repairProgressTotal
	job := factstore.JobRun{
		JobID: jobID, JobType: jobType, RequestedBy: repairRequestedBy,
		State: factstore.JobQueued, Phase: factstore.JobPhaseMaintenance,
		CreatedAtMS: createdAtMS, UpdatedAtMS: createdAtMS,
		ProgressCurrent: &progress, ProgressTotal: &total,
	}
	if err := service.repository.CreateJobRun(ctx, job); err != nil {
		return report, err
	}
	lastAtMS := createdAtMS
	if err := service.transition(ctx, jobID, factstore.JobQueued, factstore.JobRunning, progress, &lastAtMS, nil); err != nil {
		return report, err
	}

	backupDirectory, err := service.prepareBackupDirectory(jobID)
	if err != nil {
		return service.fail(ctx, report, jobID, progress, &lastAtMS, err)
	}
	databaseBackupPath := filepath.Join(backupDirectory, "codex-pulse.before.db")
	databaseReport, err := service.database.Backup(ctx, storesqlite.BackupOptions{Destination: databaseBackupPath})
	if err != nil {
		return service.fail(ctx, report, jobID, progress, &lastAtMS, err)
	}
	report.DatabaseBackupPath = databaseReport.Path
	progress = repairProgressDatabase
	if err := service.transition(ctx, jobID, factstore.JobRunning, factstore.JobRunning, progress, &lastAtMS, nil); err != nil {
		return report, err
	}

	indexBackupPath := filepath.Join(backupDirectory, "session_index.before.jsonl")
	if !plan.Source.Exists {
		indexBackupPath = filepath.Join(backupDirectory, "session_index.absent")
	}
	indexReport, err := service.indexFile.Backup(ctx, plan.Source, indexBackupPath)
	if err != nil {
		return service.fail(ctx, report, jobID, progress, &lastAtMS, err)
	}
	report.IndexBackupPath = indexReport.Path
	progress = repairProgressIndex
	if err := service.transition(ctx, jobID, factstore.JobRunning, factstore.JobRunning, progress, &lastAtMS, nil); err != nil {
		return report, err
	}
	if service.hooks.beforeAppend != nil {
		service.hooks.beforeAppend()
	}
	if err := service.verifyExpectationSnapshot(ctx, plan.ExpectationsSHA256); err != nil {
		return service.fail(ctx, report, jobID, progress, &lastAtMS, err)
	}

	entries := make([]Entry, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		entries = append(entries, Entry{
			ID: action.SessionID, ThreadName: action.ThreadName, UpdatedAt: action.UpdatedAt,
		})
	}
	report.FinalVersion, err = service.indexFile.Append(ctx, plan.Source, entries)
	if err != nil {
		return service.fail(ctx, report, jobID, progress, &lastAtMS, err)
	}
	progress = repairProgressAppended
	if err := service.transition(ctx, jobID, factstore.JobRunning, factstore.JobRunning, progress, &lastAtMS, nil); err != nil {
		return report, err
	}
	if service.hooks.afterAppend != nil {
		if err := service.hooks.afterAppend(); err != nil {
			// 测试 hook 模拟进程直接消失：不能把不存在的 terminal audit 伪装成 failed。
			return report, err
		}
	}
	if err := service.verifyExpectationSnapshot(ctx, plan.ExpectationsSHA256); err != nil {
		return service.fail(ctx, report, jobID, progress, &lastAtMS, err)
	}

	report.FinalVersion, err = service.reconcile(ctx, plan)
	if err != nil {
		return service.fail(ctx, report, jobID, progress, &lastAtMS, err)
	}
	expectationSnapshot, err := service.expectationSnapshot(ctx, plan.ExpectationsSHA256)
	if err != nil {
		return service.fail(ctx, report, jobID, progress, &lastAtMS, err)
	}
	progress = repairProgressTotal
	if err := service.complete(
		ctx, jobID, progress, &lastAtMS, expectationSnapshot,
	); err != nil {
		return service.fail(ctx, report, jobID, progress, &lastAtMS, err)
	}
	return report, nil
}

func (service *Service) validate() error {
	if service == nil || service.repository == nil || service.database == nil ||
		service.indexFile == nil || service.now == nil || service.syncDirectory == nil {
		return ErrInvalidPlan
	}
	return nil
}

func (service *Service) loadExpectations(ctx context.Context) ([]Expectation, error) {
	expectations, err := service.repository.ListSessionIndexExpectations(ctx)
	if err != nil {
		return nil, fmt.Errorf("read session index expectations: %w", err)
	}
	converted := make([]Expectation, 0, len(expectations))
	for _, expectation := range expectations {
		converted = append(converted, Expectation{
			SessionID: expectation.SessionID, ThreadName: expectation.ThreadName,
			UpdatedAtMS: expectation.UpdatedAtMS,
		})
	}
	return converted, nil
}

func (service *Service) verifyExpectationSnapshot(ctx context.Context, expectedDigest string) error {
	_, err := service.expectationSnapshot(ctx, expectedDigest)
	return err
}

func (service *Service) expectationSnapshot(
	ctx context.Context,
	expectedDigest string,
) ([]factstore.SessionIndexExpectation, error) {
	snapshot, err := service.repository.ListSessionIndexExpectations(ctx)
	if err != nil {
		return nil, fmt.Errorf("read session index expectations: %w", err)
	}
	snapshot = append([]factstore.SessionIndexExpectation(nil), snapshot...)
	sort.Slice(snapshot, func(left, right int) bool {
		return snapshot[left].SessionID < snapshot[right].SessionID
	})
	expectations := make([]Expectation, 0, len(snapshot))
	for _, expectation := range snapshot {
		expectations = append(expectations, Expectation{
			SessionID: expectation.SessionID, ThreadName: expectation.ThreadName,
			UpdatedAtMS: expectation.UpdatedAtMS,
		})
	}
	sort.Slice(expectations, func(left, right int) bool {
		return expectations[left].SessionID < expectations[right].SessionID
	})
	for index, expectation := range expectations {
		if !validExpectation(expectation) ||
			(index > 0 && expectations[index-1].SessionID == expectation.SessionID) {
			return nil, ErrExpectationDrift
		}
	}
	if expectationDigest(expectations) != expectedDigest {
		return nil, ErrExpectationDrift
	}
	return snapshot, nil
}

func (service *Service) reconcile(ctx context.Context, plan RepairPlan) (FileVersion, error) {
	read, err := service.indexFile.Read(ctx)
	if err != nil {
		return FileVersion{}, err
	}
	parsed, err := Parse(read.Content)
	clear(read.Content)
	if err != nil {
		return FileVersion{}, err
	}
	for _, action := range plan.Actions {
		latest, found := parsed.Latest(action.SessionID)
		if !found || latest.ThreadName != action.ThreadName {
			return FileVersion{}, ErrReconcileFailed
		}
	}
	return read.Version, nil
}

func (service *Service) fail(
	ctx context.Context,
	report RepairReport,
	jobID string,
	progress int64,
	lastAtMS *int64,
	cause error,
) (RepairReport, error) {
	class := classifyRepairError(cause)
	transitionErr := service.transition(
		context.WithoutCancel(ctx), jobID, factstore.JobRunning, factstore.JobFailed,
		progress, lastAtMS, &class,
	)
	return report, errors.Join(cause, transitionErr)
}

func (service *Service) transition(
	ctx context.Context,
	jobID string,
	expected factstore.JobState,
	state factstore.JobState,
	progress int64,
	lastAtMS *int64,
	errorClass *factstore.RuntimeErrorClass,
) error {
	atMS := service.now().UnixMilli()
	if atMS <= *lastAtMS {
		atMS = *lastAtMS + 1
	}
	total := repairProgressTotal
	err := service.repository.TransitionJobRun(ctx, factstore.JobTransition{
		JobID: jobID, ExpectedState: expected, State: state, Phase: factstore.JobPhaseMaintenance,
		ProgressCurrent: &progress, ProgressTotal: &total, ErrorClass: errorClass, AtMS: atMS,
	})
	if err == nil {
		*lastAtMS = atMS
	}
	return err
}

func (service *Service) complete(
	ctx context.Context,
	jobID string,
	progress int64,
	lastAtMS *int64,
	expectations []factstore.SessionIndexExpectation,
) error {
	atMS := service.now().UnixMilli()
	if atMS <= *lastAtMS {
		atMS = *lastAtMS + 1
	}
	total := repairProgressTotal
	err := service.repository.CompleteSessionIndexRepairJob(
		ctx,
		expectations,
		factstore.JobTransition{
			JobID: jobID, ExpectedState: factstore.JobRunning, State: factstore.JobSucceeded,
			Phase: factstore.JobPhaseMaintenance, ProgressCurrent: &progress,
			ProgressTotal: &total, AtMS: atMS,
		},
	)
	if errors.Is(err, factstore.ErrSessionIndexExpectationDrift) {
		return ErrExpectationDrift
	}
	if err == nil {
		*lastAtMS = atMS
	}
	return err
}

func (service *Service) prepareBackupDirectory(jobID string) (string, error) {
	config := service.database.Config()
	if !filepath.IsAbs(config.Path) || filepath.Clean(config.Path) != config.Path {
		return "", ErrInvalidBackup
	}
	base := filepath.Dir(config.Path)
	baseInfo, err := os.Lstat(base)
	if err != nil || !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 || baseInfo.Mode().Perm() != 0o700 {
		return "", ErrInvalidBackup
	}
	current := base
	for _, component := range []string{"backups", "session-index", jobID} {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return "", fmt.Errorf("create repair backup directory: %w", err)
			}
			parent := filepath.Dir(current)
			if err := service.syncDirectory(parent); err != nil {
				removeErr := os.Remove(current)
				cleanupSyncErr := service.syncDirectory(parent)
				return "", errors.Join(
					fmt.Errorf("sync repair backup directory: %w", err), removeErr, cleanupSyncErr,
				)
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
			return "", ErrInvalidBackup
		}
	}
	return current, nil
}

func (service *Service) backupPaths(jobID string, sourceExisted bool) (string, string) {
	directory := filepath.Join(
		filepath.Dir(service.database.Config().Path), "backups", "session-index", jobID,
	)
	indexName := "session_index.before.jsonl"
	if !sourceExisted {
		indexName = "session_index.absent"
	}
	return filepath.Join(directory, "codex-pulse.before.db"), filepath.Join(directory, indexName)
}

func repairJobID(planID string) string {
	return "session-index-repair-" + planID
}

func classifyRepairError(err error) factstore.RuntimeErrorClass {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, storesqlite.ErrCanceled):
		return factstore.RuntimeErrorCanceled
	case errors.Is(err, storesqlite.ErrBusy):
		return factstore.RuntimeErrorBusy
	case errors.Is(err, storesqlite.ErrDiskFull):
		return factstore.RuntimeErrorDiskFull
	case errors.Is(err, storesqlite.ErrReadOnly):
		return factstore.RuntimeErrorReadOnly
	case errors.Is(err, os.ErrPermission), errors.Is(err, storesqlite.ErrPermission):
		return factstore.RuntimeErrorPermission
	case errors.Is(err, storesqlite.ErrIO):
		return factstore.RuntimeErrorIO
	case errors.Is(err, storesqlite.ErrCorrupt):
		return factstore.RuntimeErrorCorrupt
	case errors.Is(err, ErrInvalidPlan), errors.Is(err, ErrPlanDrift),
		errors.Is(err, ErrExpectationDrift), errors.Is(err, ErrPlanConflict),
		errors.Is(err, ErrIndexLineTooLarge), errors.Is(err, ErrUnsupportedIndexEntry):
		return factstore.RuntimeErrorInvalid
	default:
		return factstore.RuntimeErrorUnknown
	}
}
