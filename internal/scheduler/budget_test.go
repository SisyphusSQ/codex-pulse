package scheduler

import (
	"errors"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// 测试 BudgetPolicy 按service class与系统压力返回冻结的协作式预算。
func TestBudgetPolicyResolvesSystemAwareBudgets(t *testing.T) {
	t.Parallel()

	policy := DefaultBudgetPolicy()
	tests := []struct {
		name     string
		class    store.SchedulerServiceClass
		system   SystemSnapshot
		expected ScanBudget
	}{
		{
			name: "background normal", class: store.SchedulerServiceBackground,
			expected: ScanBudget{MaxFiles: 8, MaxBytes: 4 << 20, MaxActive: 50 * time.Millisecond, YieldFor: 150 * time.Millisecond},
		},
		{
			name: "background low power", class: store.SchedulerServiceBackground,
			system:   SystemSnapshot{LowPower: true},
			expected: ScanBudget{MaxFiles: 4, MaxBytes: 2 << 20, MaxActive: 25 * time.Millisecond, YieldFor: 250 * time.Millisecond},
		},
		{
			name: "background cpu pressure wins low power", class: store.SchedulerServiceBackground,
			system:   SystemSnapshot{LowPower: true, CPUPressure: true},
			expected: ScanBudget{MaxFiles: 1, MaxBytes: 256 << 10, MaxActive: 10 * time.Millisecond, YieldFor: 500 * time.Millisecond},
		},
		{
			name: "background memory pressure", class: store.SchedulerServiceBackground,
			system:   SystemSnapshot{MemoryPressure: true},
			expected: ScanBudget{MaxFiles: 1, MaxBytes: 256 << 10, MaxActive: 10 * time.Millisecond, YieldFor: 500 * time.Millisecond},
		},
		{
			name: "interactive normal", class: store.SchedulerServiceInteractive,
			expected: ScanBudget{MaxFiles: 16, MaxBytes: 16 << 20, MaxActive: 200 * time.Millisecond},
		},
		{
			name: "interactive pressure", class: store.SchedulerServiceInteractive,
			system:   SystemSnapshot{CPUPressure: true},
			expected: ScanBudget{MaxFiles: 4, MaxBytes: 4 << 20, MaxActive: 50 * time.Millisecond, YieldFor: 50 * time.Millisecond},
		},
		{
			name: "store pressure blocks all work", class: store.SchedulerServiceInteractive,
			system:   SystemSnapshot{StorePressure: true},
			expected: ScanBudget{YieldFor: 500 * time.Millisecond, Blocked: true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := policy.Resolve(test.class, test.system)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got != test.expected {
				t.Fatalf("Resolve() = %#v, want %#v", got, test.expected)
			}
		})
	}
}

// 测试 BudgetPolicy 拒绝未知service class而不是静默套用后台默认值。
func TestBudgetPolicyRejectsUnknownServiceClass(t *testing.T) {
	t.Parallel()

	_, err := DefaultBudgetPolicy().Resolve(store.SchedulerServiceClass("unknown"), SystemSnapshot{})
	if !errors.Is(err, ErrInvalidBudget) {
		t.Fatalf("Resolve(unknown) error = %v, want ErrInvalidBudget", err)
	}
}
