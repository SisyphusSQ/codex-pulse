package scheduler

import (
	"errors"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var ErrInvalidBudget = errors.New("invalid scan budget")

// ScanBudget 是一次协作式worker slice的硬文件/字节边界和active/yield时间边界。
type ScanBudget struct {
	MaxFiles  int64
	MaxBytes  int64
	MaxActive time.Duration
	YieldFor  time.Duration
	Blocked   bool
}

// SystemSnapshot 只承载调度需要的稳定压力级别，不宣称是per-goroutine精确资源值。
type SystemSnapshot struct {
	LowPower       bool
	CPUPressure    bool
	MemoryPressure bool
	StorePressure  bool
}

// BudgetPolicy 使用显式整数预算，避免隐式浮点缩放在不同机器上漂移。
type BudgetPolicy struct {
	BackgroundNormal    ScanBudget
	BackgroundLowPower  ScanBudget
	BackgroundPressure  ScanBudget
	InteractiveNormal   ScanBudget
	InteractivePressure ScanBudget
	StoreBlocked        ScanBudget
}

func DefaultBudgetPolicy() BudgetPolicy {
	return BudgetPolicy{
		BackgroundNormal: ScanBudget{
			MaxFiles: 8, MaxBytes: 4 << 20, MaxActive: 50 * time.Millisecond,
			YieldFor: 150 * time.Millisecond,
		},
		BackgroundLowPower: ScanBudget{
			MaxFiles: 4, MaxBytes: 2 << 20, MaxActive: 25 * time.Millisecond,
			YieldFor: 250 * time.Millisecond,
		},
		BackgroundPressure: ScanBudget{
			MaxFiles: 1, MaxBytes: 256 << 10, MaxActive: 10 * time.Millisecond,
			YieldFor: 500 * time.Millisecond,
		},
		InteractiveNormal: ScanBudget{
			MaxFiles: 16, MaxBytes: 16 << 20, MaxActive: 200 * time.Millisecond,
		},
		InteractivePressure: ScanBudget{
			MaxFiles: 4, MaxBytes: 4 << 20, MaxActive: 50 * time.Millisecond,
			YieldFor: 50 * time.Millisecond,
		},
		StoreBlocked: ScanBudget{YieldFor: 500 * time.Millisecond, Blocked: true},
	}
}

func (policy BudgetPolicy) Resolve(
	class store.SchedulerServiceClass,
	system SystemSnapshot,
) (ScanBudget, error) {
	var budget ScanBudget
	switch {
	case system.StorePressure:
		budget = policy.StoreBlocked
	case class == store.SchedulerServiceInteractive && (system.CPUPressure || system.MemoryPressure):
		budget = policy.InteractivePressure
	case class == store.SchedulerServiceInteractive:
		budget = policy.InteractiveNormal
	case class == store.SchedulerServiceBackground && (system.CPUPressure || system.MemoryPressure):
		budget = policy.BackgroundPressure
	case class == store.SchedulerServiceBackground && system.LowPower:
		budget = policy.BackgroundLowPower
	case class == store.SchedulerServiceBackground:
		budget = policy.BackgroundNormal
	default:
		return ScanBudget{}, ErrInvalidBudget
	}
	if !validScanBudget(budget) {
		return ScanBudget{}, ErrInvalidBudget
	}
	return budget, nil
}

func validScanBudget(budget ScanBudget) bool {
	if budget.YieldFor < 0 {
		return false
	}
	if budget.Blocked {
		return budget.MaxFiles == 0 && budget.MaxBytes == 0 && budget.MaxActive == 0
	}
	return budget.MaxFiles > 0 && budget.MaxBytes > 0 && budget.MaxActive > 0
}
