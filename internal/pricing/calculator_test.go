package pricing

import (
	"errors"
	"math"
	"testing"
)

func TestCalculateUsesExactMicroUSDAndPricesReasoningAsOutput(t *testing.T) {
	t.Parallel()

	calculation, err := Calculate(
		Usage{
			InputTokens: pointer(int64(1_000_000)), CachedInputTokens: pointer(int64(1_000_000)),
			OutputTokens: pointer(int64(1_000_000)), ReasoningTokens: pointer(int64(1_000_000)),
		},
		Rates{
			InputMicrosPerMillion:       pointer(int64(1_250_000)),
			CachedInputMicrosPerMillion: pointer(int64(125_000)),
			OutputMicrosPerMillion:      pointer(int64(10_000_000)),
		},
	)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if calculation.Status != CostStatusPriced || calculation.Reason != CostReasonPriced ||
		calculation.EstimatedUSDMicros == nil || *calculation.EstimatedUSDMicros != 21_375_000 {
		t.Fatalf("Calculate() = %#v", calculation)
	}
}

func TestCalculateRoundsCombinedNumeratorHalfUpOnce(t *testing.T) {
	t.Parallel()

	calculation, err := Calculate(
		Usage{
			InputTokens: pointer(int64(1)), CachedInputTokens: pointer(int64(1)),
			OutputTokens: pointer(int64(0)), ReasoningTokens: pointer(int64(0)),
		},
		Rates{
			InputMicrosPerMillion:       pointer(int64(250_000)),
			CachedInputMicrosPerMillion: pointer(int64(250_000)),
			OutputMicrosPerMillion:      pointer(int64(9_999_999)),
		},
	)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if calculation.EstimatedUSDMicros == nil || *calculation.EstimatedUSDMicros != 1 {
		t.Fatalf("combined half-up result = %#v, want 1 microUSD", calculation)
	}
}

func TestCalculatePreservesMissingZeroAndUnavailablePriceSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		usage      Usage
		rates      Rates
		wantStatus CostStatus
		wantReason CostReason
		wantMicros *int64
	}{
		{
			name: "missing token",
			usage: Usage{
				InputTokens: nil, CachedInputTokens: pointer(int64(0)),
				OutputTokens: pointer(int64(0)), ReasoningTokens: pointer(int64(0)),
			},
			rates: Rates{}, wantStatus: CostStatusUnpriced, wantReason: CostReasonMissingToken,
		},
		{
			name: "all observed zero accepts absent rates",
			usage: Usage{
				InputTokens: pointer(int64(0)), CachedInputTokens: pointer(int64(0)),
				OutputTokens: pointer(int64(0)), ReasoningTokens: pointer(int64(0)),
			},
			rates: Rates{}, wantStatus: CostStatusPriced, wantReason: CostReasonPriced,
			wantMicros: pointer(int64(0)),
		},
		{
			name: "positive token requires category price",
			usage: Usage{
				InputTokens: pointer(int64(1)), CachedInputTokens: pointer(int64(0)),
				OutputTokens: pointer(int64(0)), ReasoningTokens: pointer(int64(0)),
			},
			rates: Rates{}, wantStatus: CostStatusUnpriced, wantReason: CostReasonMissingPriceComponent,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, err := Calculate(testCase.usage, testCase.rates)
			if err != nil {
				t.Fatalf("Calculate() error = %v", err)
			}
			if got.Status != testCase.wantStatus || got.Reason != testCase.wantReason ||
				!equalOptionalInt64(got.EstimatedUSDMicros, testCase.wantMicros) {
				t.Fatalf("Calculate() = %#v, want status=%q reason=%q micros=%v", got, testCase.wantStatus, testCase.wantReason, testCase.wantMicros)
			}
		})
	}
}

func TestCalculateRejectsNegativeValuesAndOverflow(t *testing.T) {
	t.Parallel()

	validUsage := Usage{
		InputTokens: pointer(int64(0)), CachedInputTokens: pointer(int64(0)),
		OutputTokens: pointer(int64(0)), ReasoningTokens: pointer(int64(0)),
	}
	validRates := Rates{
		InputMicrosPerMillion:       pointer(int64(1)),
		CachedInputMicrosPerMillion: pointer(int64(1)),
		OutputMicrosPerMillion:      pointer(int64(1)),
	}

	negativeUsage := validUsage
	negativeUsage.InputTokens = pointer(int64(-1))
	if _, err := Calculate(negativeUsage, validRates); !errors.Is(err, ErrInvalidCalculation) {
		t.Fatalf("Calculate(negative usage) error = %v, want ErrInvalidCalculation", err)
	}

	negativeRates := validRates
	negativeRates.OutputMicrosPerMillion = pointer(int64(-1))
	if _, err := Calculate(validUsage, negativeRates); !errors.Is(err, ErrInvalidCalculation) {
		t.Fatalf("Calculate(negative rate) error = %v, want ErrInvalidCalculation", err)
	}
	missingAndNegative := validUsage
	missingAndNegative.InputTokens = nil
	if _, err := Calculate(missingAndNegative, negativeRates); !errors.Is(err, ErrInvalidCalculation) {
		t.Fatalf("Calculate(missing token with negative rate) error = %v, want ErrInvalidCalculation", err)
	}

	overflowUsage := Usage{
		InputTokens: pointer(int64(math.MaxInt64)), CachedInputTokens: pointer(int64(0)),
		OutputTokens: pointer(int64(0)), ReasoningTokens: pointer(int64(0)),
	}
	overflowRates := Rates{InputMicrosPerMillion: pointer(int64(math.MaxInt64))}
	if _, err := Calculate(overflowUsage, overflowRates); !errors.Is(err, ErrCostOverflow) {
		t.Fatalf("Calculate(overflow) error = %v, want ErrCostOverflow", err)
	}
}

func equalOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func pointer[T any](value T) *T { return &value }
