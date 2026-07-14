package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// 测试 JobRun 状态机拒绝非法、陈旧、倒退和 terminal revival。
func TestJobRunStateMachineRejectsIllegalAndStaleTransitions(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	queued := JobRun{
		JobID:           "job-a",
		JobType:         "history_scan",
		RequestedBy:     "startup",
		Priority:        10,
		State:           JobQueued,
		Phase:           JobPhaseDiscover,
		CreatedAtMS:     10,
		ProgressCurrent: pointerTo(int64(0)),
		ProgressTotal:   pointerTo(int64(100)),
		ResumeCursor:    pointerTo(JobCursor{Generation: 0, Offset: 0}),
		UpdatedAtMS:     10,
	}
	if err := repository.CreateJobRun(context.Background(), queued); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	if err := repository.CreateJobRun(context.Background(), queued); err != nil {
		t.Fatalf("CreateJobRun(replay) error = %v", err)
	}
	rawCursor := queued
	rawCursor.JobID = "job-raw-cursor"
	rawCursor.ResumeCursor = pointerTo(JobCursor{Generation: -1, Offset: 1})
	if err := repository.CreateJobRun(context.Background(), rawCursor); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CreateJobRun(raw cursor) error = %v, want ErrInvalidRecord", err)
	}
	got, err := repository.JobRun(context.Background(), queued.JobID)
	if err != nil {
		t.Fatalf("JobRun() error = %v", err)
	}
	if !reflect.DeepEqual(got, queued) {
		t.Fatalf("JobRun() = %#v, want %#v", got, queued)
	}

	running := JobTransition{
		JobID:           queued.JobID,
		ExpectedState:   JobQueued,
		State:           JobRunning,
		Phase:           JobPhaseFastBootstrap,
		ProgressCurrent: pointerTo(int64(20)),
		ProgressTotal:   pointerTo(int64(100)),
		ResumeCursor:    pointerTo(JobCursor{Generation: 0, Offset: 20}),
		AtMS:            20,
	}
	if err := repository.TransitionJobRun(context.Background(), running); err != nil {
		t.Fatalf("TransitionJobRun(running) error = %v", err)
	}
	if err := repository.TransitionJobRun(context.Background(), running); err != nil {
		t.Fatalf("TransitionJobRun(running replay) error = %v", err)
	}

	stale := running
	stale.State = JobCancelled
	stale.AtMS = 21
	if err := repository.TransitionJobRun(context.Background(), stale); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("TransitionJobRun(stale expected state) error = %v, want ErrInvalidRecord", err)
	}
	regressed := JobTransition{
		JobID:           queued.JobID,
		ExpectedState:   JobRunning,
		State:           JobRunning,
		Phase:           JobPhaseDiscover,
		ProgressCurrent: pointerTo(int64(10)),
		ProgressTotal:   pointerTo(int64(100)),
		AtMS:            21,
	}
	if err := repository.TransitionJobRun(context.Background(), regressed); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("TransitionJobRun(regression) error = %v, want ErrInvalidRecord", err)
	}

	succeeded := JobTransition{
		JobID:           queued.JobID,
		ExpectedState:   JobRunning,
		State:           JobSucceeded,
		Phase:           JobPhaseReconcile,
		ProgressCurrent: pointerTo(int64(100)),
		ProgressTotal:   pointerTo(int64(100)),
		ResumeCursor:    pointerTo(JobCursor{Generation: 0, Offset: 100}),
		AtMS:            30,
	}
	if err := repository.TransitionJobRun(context.Background(), succeeded); err != nil {
		t.Fatalf("TransitionJobRun(succeeded) error = %v", err)
	}
	terminal, err := repository.JobRun(context.Background(), queued.JobID)
	if err != nil {
		t.Fatalf("JobRun(terminal) error = %v", err)
	}
	if terminal.State != JobSucceeded || terminal.StartedAtMS == nil || *terminal.StartedAtMS != 20 ||
		terminal.FinishedAtMS == nil || *terminal.FinishedAtMS != 30 {
		t.Fatalf("terminal lifecycle = %#v, want running at 20 and succeeded at 30", terminal)
	}
	if err := repository.TransitionJobRun(context.Background(), JobTransition{
		JobID: queued.JobID, ExpectedState: JobSucceeded, State: JobRunning,
		Phase: JobPhaseReconcile, AtMS: 40,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("TransitionJobRun(terminal revival) error = %v, want ErrInvalidRecord", err)
	}

	illegal := queued
	illegal.JobID = "job-illegal"
	illegal.CreatedAtMS = 40
	illegal.UpdatedAtMS = 40
	if err := repository.CreateJobRun(context.Background(), illegal); err != nil {
		t.Fatalf("CreateJobRun(illegal fixture) error = %v", err)
	}
	if err := repository.TransitionJobRun(context.Background(), JobTransition{
		JobID: illegal.JobID, ExpectedState: JobQueued, State: JobSucceeded,
		Phase: JobPhaseDiscover, AtMS: 41,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("TransitionJobRun(queued to succeeded) error = %v, want ErrInvalidRecord", err)
	}
}

// 测试 startup interruption 与新 Job resume 保留旧历史、进度和 cursor 血缘。
func TestInterruptAndResumeJobRunsPreserveHistoryAndCursor(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	queued := JobRun{
		JobID: "job-queued", JobType: "scan", RequestedBy: "startup", Priority: 5,
		State: JobQueued, Phase: JobPhaseDiscover, CreatedAtMS: 10,
		ProgressCurrent: pointerTo(int64(0)), ResumeCursor: pointerTo(JobCursor{Generation: 0, Offset: 0}), UpdatedAtMS: 10,
	}
	running := JobRun{
		JobID: "job-running", JobType: "scan", RequestedBy: "startup", Priority: 6,
		State: JobQueued, Phase: JobPhaseHistoryBackfill, CreatedAtMS: 11,
		ProgressCurrent: pointerTo(int64(40)), ProgressTotal: pointerTo(int64(100)),
		ResumeCursor: pointerTo(JobCursor{Generation: 0, Offset: 40}), UpdatedAtMS: 11,
	}
	for _, job := range []JobRun{queued, running} {
		if err := repository.CreateJobRun(context.Background(), job); err != nil {
			t.Fatalf("CreateJobRun(%s) error = %v", job.JobID, err)
		}
	}
	if err := repository.TransitionJobRun(context.Background(), JobTransition{
		JobID: running.JobID, ExpectedState: JobQueued, State: JobRunning,
		Phase: running.Phase, ProgressCurrent: running.ProgressCurrent,
		ProgressTotal: running.ProgressTotal, ResumeCursor: running.ResumeCursor, AtMS: 12,
	}); err != nil {
		t.Fatalf("TransitionJobRun(running fixture) error = %v", err)
	}
	bypass := JobRun{
		JobID: "job-bypass-resume", JobType: "different", RequestedBy: "invalid", Priority: 1,
		State: JobQueued, Phase: JobPhaseDiscover, ResumeOfJobID: pointerTo(running.JobID),
		CreatedAtMS: 13, ResumeCursor: pointerTo(JobCursor{Generation: 9, Offset: 999}), UpdatedAtMS: 13,
	}
	if err := repository.CreateJobRun(context.Background(), bypass); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CreateJobRun(resume bypass) error = %v, want ErrInvalidRecord", err)
	}
	nonInterruptedResume := JobRun{
		JobID: "job-resume-running", JobType: running.JobType, RequestedBy: "recovery", Priority: running.Priority,
		State: JobQueued, Phase: running.Phase, ResumeOfJobID: pointerTo(running.JobID),
		CreatedAtMS: 13, ProgressCurrent: running.ProgressCurrent, ProgressTotal: running.ProgressTotal,
		ResumeCursor: running.ResumeCursor, UpdatedAtMS: 13,
	}
	if err := repository.ResumeInterruptedJob(
		context.Background(), running.JobID, nonInterruptedResume,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ResumeInterruptedJob(running parent) error = %v, want ErrInvalidRecord", err)
	}

	count, err := repository.InterruptIncompleteJobs(context.Background(), 20)
	if err != nil {
		t.Fatalf("InterruptIncompleteJobs() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("InterruptIncompleteJobs() count = %d, want 2", count)
	}
	if again, err := repository.InterruptIncompleteJobs(context.Background(), 21); err != nil || again != 0 {
		t.Fatalf("InterruptIncompleteJobs(replay) = (%d, %v), want (0, nil)", again, err)
	}

	old, err := repository.JobRun(context.Background(), running.JobID)
	if err != nil {
		t.Fatalf("JobRun(interrupted) error = %v", err)
	}
	if old.State != JobInterrupted || old.FinishedAtMS == nil || *old.FinishedAtMS != 20 ||
		old.ResumeCursor == nil || *old.ResumeCursor != (JobCursor{Generation: 0, Offset: 40}) {
		t.Fatalf("interrupted job = %#v, want cursor-preserving terminal state", old)
	}
	queuedOld, err := repository.JobRun(context.Background(), queued.JobID)
	if err != nil {
		t.Fatalf("JobRun(queued interrupted) error = %v", err)
	}
	if queuedOld.State != JobInterrupted || queuedOld.StartedAtMS != nil || queuedOld.FinishedAtMS == nil {
		t.Fatalf("queued interrupted lifecycle = %#v, want no started time and a finished time", queuedOld)
	}

	resumed := JobRun{
		JobID: "job-resumed", JobType: old.JobType, RequestedBy: "recovery", Priority: old.Priority,
		State: JobQueued, Phase: old.Phase, ResumeOfJobID: pointerTo(old.JobID),
		CreatedAtMS: 30, ProgressCurrent: old.ProgressCurrent, ProgressTotal: old.ProgressTotal,
		ResumeCursor: old.ResumeCursor, UpdatedAtMS: 30,
	}
	tooEarly := resumed
	tooEarly.JobID = "job-resumed-too-early"
	tooEarly.CreatedAtMS = 19
	tooEarly.UpdatedAtMS = 19
	if err := repository.ResumeInterruptedJob(context.Background(), old.JobID, tooEarly); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ResumeInterruptedJob(before interruption) error = %v, want ErrInvalidRecord", err)
	}
	resumeConflicts := []struct {
		name string
		job  JobRun
	}{
		{"job type", func() JobRun {
			value := resumed
			value.JobID = "resume-wrong-type"
			value.JobType = "other"
			return value
		}()},
		{"priority", func() JobRun { value := resumed; value.JobID = "resume-wrong-priority"; value.Priority++; return value }()},
		{"phase", func() JobRun {
			value := resumed
			value.JobID = "resume-wrong-phase"
			value.Phase = JobPhaseMaintenance
			return value
		}()},
		{"progress", func() JobRun {
			value := resumed
			value.JobID = "resume-wrong-progress"
			value.ProgressCurrent = pointerTo(int64(41))
			return value
		}()},
		{"cursor", func() JobRun {
			value := resumed
			value.JobID = "resume-wrong-cursor"
			value.ResumeCursor = pointerTo(JobCursor{Generation: 0, Offset: 41})
			return value
		}()},
	}
	for _, testCase := range resumeConflicts {
		t.Run("resume conflict "+testCase.name, func(t *testing.T) {
			if err := repository.ResumeInterruptedJob(
				context.Background(), old.JobID, testCase.job,
			); !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("ResumeInterruptedJob() error = %v, want ErrInvalidRecord", err)
			}
		})
	}
	if err := repository.ResumeInterruptedJob(context.Background(), old.JobID, resumed); err != nil {
		t.Fatalf("ResumeInterruptedJob() error = %v", err)
	}
	if err := repository.ResumeInterruptedJob(context.Background(), old.JobID, resumed); err != nil {
		t.Fatalf("ResumeInterruptedJob(replay) error = %v", err)
	}
	gotResumed, err := repository.JobRun(context.Background(), resumed.JobID)
	if err != nil {
		t.Fatalf("JobRun(resumed) error = %v", err)
	}
	if !reflect.DeepEqual(gotResumed, resumed) {
		t.Fatalf("JobRun(resumed) = %#v, want %#v", gotResumed, resumed)
	}
	oldAfter, err := repository.JobRun(context.Background(), old.JobID)
	if err != nil {
		t.Fatalf("JobRun(old after resume) error = %v", err)
	}
	if !reflect.DeepEqual(oldAfter, old) {
		t.Fatalf("old job changed after resume: got %#v, want %#v", oldAfter, old)
	}

	jobs, err := repository.ListJobRuns(context.Background(), JobRunFilter{State: pointerTo(JobInterrupted), Limit: 10})
	if err != nil {
		t.Fatalf("ListJobRuns() error = %v", err)
	}
	if len(jobs) != 2 || jobs[0].JobID != "job-running" || jobs[1].JobID != "job-queued" {
		t.Fatalf("ListJobRuns(interrupted) = %#v, want stable update/id order", jobs)
	}
}
