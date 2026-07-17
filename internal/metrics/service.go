package metrics

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/robfig/cron/v3"
)

const (
	metricsNormalCronSpec   = "@every 30s"
	metricsDetailedCronSpec = "@every 5s"
)

var (
	ErrService          = errors.New("runtime metrics service")
	ErrServiceRunActive = errors.New("runtime metrics service is already running")
	ErrCollectionPanic  = errors.New("runtime metrics collection panicked")
)

type SamplingMode string

const (
	SamplingModeNormal   SamplingMode = "normal"
	SamplingModeDetailed SamplingMode = "detailed"
)

func (mode SamplingMode) cronSpec() (string, error) {
	switch mode {
	case SamplingModeNormal:
		return metricsNormalCronSpec, nil
	case SamplingModeDetailed:
		return metricsDetailedCronSpec, nil
	default:
		return "", fmt.Errorf("%w: invalid sampling mode %q", ErrService, mode)
	}
}

type SampleCollector interface {
	Collect(context.Context) error
}

type droppedSampleRecorder interface {
	RecordDroppedSample()
}

type metricsCronRunner interface {
	Start()
	Stop() context.Context
	SetMode(SamplingMode) error
}

type metricsCronRunnerFactory func(SamplingMode, cron.Job) (metricsCronRunner, error)

func defaultMetricsCronRunnerFactory(mode SamplingMode, job cron.Job) (metricsCronRunner, error) {
	runner := &defaultMetricsCronRunner{job: job}
	runner.cron = cron.New(cron.WithChain(
		cron.Recover(cron.DiscardLogger),
	))
	if err := runner.SetMode(mode); err != nil {
		return nil, err
	}
	return runner, nil
}

type defaultMetricsCronRunner struct {
	cron *cron.Cron
	job  cron.Job

	mu      sync.Mutex
	entryID cron.EntryID
	mode    SamplingMode
}

func (runner *defaultMetricsCronRunner) Start() { runner.cron.Start() }

func (runner *defaultMetricsCronRunner) Stop() context.Context { return runner.cron.Stop() }

func (runner *defaultMetricsCronRunner) SetMode(mode SamplingMode) error {
	spec, err := mode.cronSpec()
	if err != nil {
		return err
	}
	if runner == nil || runner.cron == nil || runner.job == nil {
		return fmt.Errorf("%w: invalid cron runner", ErrService)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.entryID != 0 && runner.mode == mode {
		return nil
	}
	entryID, err := runner.cron.AddJob(spec, runner.job)
	if err != nil {
		return fmt.Errorf("%w: register cron job: %w", ErrService, err)
	}
	if runner.entryID != 0 {
		runner.cron.Remove(runner.entryID)
	}
	runner.entryID = entryID
	runner.mode = mode
	return nil
}

type ServiceConfig struct {
	Collector SampleCollector
	Mode      SamplingMode
}

// Service 只负责用 robfig/cron 触发采样；单次采样失败不会终止应用生命周期。
type Service struct {
	collector     SampleCollector
	mode          SamplingMode
	newCronRunner metricsCronRunnerFactory

	runMu      sync.Mutex
	running    bool
	runner     metricsCronRunner
	collecting atomic.Bool
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.Collector == nil {
		return nil, fmt.Errorf("%w: collector is required", ErrService)
	}
	if _, err := config.Mode.cronSpec(); err != nil {
		return nil, err
	}
	return &Service{
		collector: config.Collector, mode: config.Mode,
		newCronRunner: defaultMetricsCronRunnerFactory,
	}, nil
}

func (service *Service) Run(ctx context.Context) error {
	if service == nil || service.collector == nil || service.newCronRunner == nil || ctx == nil {
		return fmt.Errorf("%w: invalid service", ErrService)
	}
	service.runMu.Lock()
	if service.running {
		service.runMu.Unlock()
		return ErrServiceRunActive
	}
	runner, err := service.newCronRunner(service.mode, cron.FuncJob(func() {
		_ = service.collectSafely(ctx)
	}))
	if err != nil {
		service.runMu.Unlock()
		return err
	}
	service.running = true
	service.runner = runner
	service.runMu.Unlock()
	defer func() {
		service.runMu.Lock()
		service.running = false
		service.runner = nil
		service.runMu.Unlock()
	}()

	jobCtx, cancelJobs := context.WithCancel(ctx)
	defer cancelJobs()
	// The cron job closes over ctx above only to make construction atomic with
	// SetMode. ctx and jobCtx are cancelled together before Stop drains jobs.
	_ = service.collectSafely(jobCtx)
	runner.Start()
	<-ctx.Done()
	cancelJobs()
	<-runner.Stop().Done()
	return ctx.Err()
}

func (service *Service) SetMode(mode SamplingMode) error {
	if service == nil {
		return fmt.Errorf("%w: invalid service", ErrService)
	}
	if _, err := mode.cronSpec(); err != nil {
		return err
	}
	service.runMu.Lock()
	defer service.runMu.Unlock()
	if service.mode == mode {
		return nil
	}
	if service.runner != nil {
		if err := service.runner.SetMode(mode); err != nil {
			return err
		}
	}
	service.mode = mode
	return nil
}

func (service *Service) collectSafely(ctx context.Context) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrCollectionPanic
		}
	}()
	if !service.collecting.CompareAndSwap(false, true) {
		if recorder, ok := service.collector.(droppedSampleRecorder); ok {
			recorder.RecordDroppedSample()
		}
		return nil
	}
	defer service.collecting.Store(false)
	return service.collector.Collect(ctx)
}
