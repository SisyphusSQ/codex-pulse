package sqlite

import "context"

// WALCheckpointReport records SQLite's PASSIVE checkpoint result. Busy means
// another connection prevented a complete checkpoint; it is an observed state,
// not an execution error.
type WALCheckpointReport struct {
	Busy               bool
	LogFrames          int
	CheckpointedFrames int
}

// CheckpointWAL admits one fixed PASSIVE checkpoint through the low-priority
// writer queue. The fixed PRAGMA is an isolated SQLite lifecycle operation:
// checkpoints cannot execute inside the GORM-owned write transaction, and no
// caller-controlled SQL or checkpoint mode is accepted.
func (store *Store) CheckpointWAL(ctx context.Context) (WALCheckpointReport, error) {
	var report WALCheckpointReport
	err := store.enqueueOperation(ctx, func(ctx context.Context) error {
		var busy int
		err := store.writerSQL.QueryRowContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(
			&busy,
			&report.LogFrames,
			&report.CheckpointedFrames,
		)
		report.Busy = busy != 0
		return err
	}, store.maintenanceQueue, "enqueue WAL checkpoint")
	return report, err
}
