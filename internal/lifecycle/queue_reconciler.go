package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"strconv"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/liveindex"
	"github.com/SisyphusSQ/codex-pulse/internal/scheduler"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const defaultLiveLaneCapacity = 1024

type snapshotRepository interface {
	CodexSnapshots(context.Context) ([]store.SourceFingerprint, error)
}

type liveActionStarter interface {
	Start(context.Context, liveindex.LiveRequest) (store.JobRun, error)
}

type liveTaskEnqueuer interface {
	Enqueue(context.Context, scheduler.EnqueueRequest) (store.SchedulerTask, error)
}

type reconcileDiscover func(context.Context, ConfirmedHome, []logs.Snapshot) (logs.ReconcilePlan, error)

type QueueReconcileRunnerConfig struct {
	Repository   snapshotRepository
	Live         liveActionStarter
	Queue        liveTaskEnqueuer
	Discover     reconcileDiscover
	LaneCapacity int
}

// QueueReconcileRunner 把轻量目录reconcile产生的typed增量action接回既有
// live runtime与scheduler durable queue；它不解析文件，也不复制checkpoint。
type QueueReconcileRunner struct {
	repository   snapshotRepository
	live         liveActionStarter
	queue        liveTaskEnqueuer
	discover     reconcileDiscover
	laneCapacity int
}

func NewQueueReconcileRunner(config QueueReconcileRunnerConfig) (*QueueReconcileRunner, error) {
	if config.Repository == nil || config.Live == nil || config.Queue == nil ||
		config.LaneCapacity < 0 || config.LaneCapacity > 10000 {
		return nil, ErrInvalidCoordinator
	}
	if config.Discover == nil {
		config.Discover = discoverConfirmedHome
	}
	if config.LaneCapacity == 0 {
		config.LaneCapacity = defaultLiveLaneCapacity
	}
	return &QueueReconcileRunner{
		repository: config.Repository, live: config.Live, queue: config.Queue,
		discover: config.Discover, laneCapacity: config.LaneCapacity,
	}, nil
}

func (runner *QueueReconcileRunner) RunReconcile(
	ctx context.Context,
	home ConfirmedHome,
	reason ReconcileReason,
) error {
	if runner == nil || runner.repository == nil || runner.live == nil || runner.queue == nil ||
		runner.discover == nil || ctx == nil || !validConfirmedHome(home) || !validReconcileReason(reason) {
		return ErrInvalidCoordinator
	}
	stored, err := runner.repository.CodexSnapshots(ctx)
	if err != nil {
		return sanitizedReconcileDependencyError(ctx, err)
	}
	previous := make([]logs.Snapshot, len(stored))
	for index, value := range stored {
		previous[index] = snapshotFromStoreFingerprint(value)
	}
	plan, err := runner.discover(ctx, home, previous)
	if err != nil {
		return sanitizedReconcileDependencyError(ctx, err)
	}
	for _, action := range plan.Actions {
		if !liveReconcileAction(action) {
			continue
		}
		requestID := stableReconcileRequestID(home.Generation, action)
		requestedAtMS := action.Current.Fingerprint.MTimeNS / 1_000_000
		if requestedAtMS < 1 {
			requestedAtMS = 1
		}
		job, err := runner.live.Start(ctx, liveindex.LiveRequest{
			RequestID: requestID, HomeGeneration: home.Generation,
			HomePath: home.Path, HomeDeviceID: home.DeviceID, HomeInode: home.Inode,
			Action: action, RequestedAtMS: requestedAtMS,
		})
		if err != nil {
			return sanitizedReconcileDependencyError(ctx, err)
		}
		if _, err := runner.queue.Enqueue(ctx, scheduler.EnqueueRequest{
			TaskID: "task-live-" + requestID, DedupeKey: "live:" + requestID,
			TargetKind: store.SchedulerTargetLiveScan, TargetID: job.JobID,
			HomeGeneration: home.Generation, Lane: store.SchedulerLaneLive,
			ServiceClass: store.SchedulerServiceBackground, RequestedAtMS: requestedAtMS,
			LaneCapacity: runner.laneCapacity,
		}); err != nil {
			return sanitizedReconcileDependencyError(ctx, err)
		}
	}
	return nil
}

func discoverConfirmedHome(
	ctx context.Context,
	home ConfirmedHome,
	previous []logs.Snapshot,
) (logs.ReconcilePlan, error) {
	discoverer, err := logs.NewConfirmedDiscoverer(home.Path, home.DeviceID, home.Inode)
	if err != nil {
		return logs.ReconcilePlan{}, err
	}
	discovery, err := discoverer.DiscoverAgainst(ctx, previous)
	if err != nil {
		return logs.ReconcilePlan{}, err
	}
	return logs.PlanReconcile(home.Path, previous, discovery)
}

func snapshotFromStoreFingerprint(value store.SourceFingerprint) logs.Snapshot {
	return logs.Snapshot{
		SourceFileID: value.SourceFileID, Provider: value.Provider,
		Kind: logs.SourceKind(value.SourceKind), Path: value.CurrentPath,
		Fingerprint: logs.Fingerprint{
			DeviceID: value.DeviceID, Inode: value.Inode, SizeBytes: value.SizeBytes,
			MTimeNS: value.MTimeNS, PrefixBytes: value.PrefixBytes,
			PrefixSHA256: value.PrefixSHA256, Digest: value.FingerprintSHA256,
		},
	}
}

func liveReconcileAction(action logs.ReconcileAction) bool {
	if action.Current == nil || action.Issue != nil {
		return false
	}
	switch action.Kind {
	case logs.ChangeAdded, logs.ChangeGrown, logs.ChangeTruncated,
		logs.ChangeMoved, logs.ChangeReplaced:
		return true
	default:
		return false
	}
}

func stableReconcileRequestID(generation int64, action logs.ReconcileAction) string {
	hasher := sha256.New()
	writeReconcileIdentity(hasher, strconv.FormatInt(generation, 10))
	writeReconcileIdentity(hasher, string(action.Kind))
	writeReconcileSnapshotIdentity(hasher, action.Previous)
	writeReconcileSnapshotIdentity(hasher, action.Current)
	return "reconcile-" + hex.EncodeToString(hasher.Sum(nil))
}

func writeReconcileSnapshotIdentity(hasher hash.Hash, snapshot *logs.Snapshot) {
	if snapshot == nil {
		writeReconcileIdentity(hasher, "")
		return
	}
	writeReconcileIdentity(hasher, snapshot.SourceFileID)
	writeReconcileIdentity(hasher, snapshot.Path)
	writeReconcileIdentity(hasher, snapshot.Fingerprint.Digest)
}

func writeReconcileIdentity(hasher hash.Hash, value string) {
	_, _ = hasher.Write([]byte(strconv.Itoa(len(value))))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(value))
}

func validReconcileReason(reason ReconcileReason) bool {
	switch reason {
	case ReconcileUserResume, ReconcileSystemWake, ReconcileSourceChange, ReconcileStartup:
		return true
	default:
		return false
	}
}

var _ ReconcileRunner = (*QueueReconcileRunner)(nil)
