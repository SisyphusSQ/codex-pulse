package pricing

import "math/big"

var million = big.NewInt(TokensPerMillion)

// Calculate 对全部 token×rate numerator 精确求和后只做一次 round-half-up。
// Codex 的 reasoning token 是独立 output 类计数，因此与 output 共用 output rate。
func Calculate(usage Usage, rates Rates) (Calculation, error) {
	for _, value := range []*int64{
		usage.InputTokens, usage.CachedInputTokens, usage.OutputTokens, usage.ReasoningTokens,
		rates.InputMicrosPerMillion, rates.CachedInputMicrosPerMillion, rates.OutputMicrosPerMillion,
	} {
		if value != nil && *value < 0 {
			return Calculation{}, ErrInvalidCalculation
		}
	}
	if usage.InputTokens == nil || usage.CachedInputTokens == nil ||
		usage.OutputTokens == nil || usage.ReasoningTokens == nil {
		return Calculation{Status: CostStatusUnpriced, Reason: CostReasonMissingToken}, nil
	}
	if (*usage.InputTokens > 0 && rates.InputMicrosPerMillion == nil) ||
		(*usage.CachedInputTokens > 0 && rates.CachedInputMicrosPerMillion == nil) ||
		((*usage.OutputTokens > 0 || *usage.ReasoningTokens > 0) && rates.OutputMicrosPerMillion == nil) {
		return Calculation{Status: CostStatusUnpriced, Reason: CostReasonMissingPriceComponent}, nil
	}

	numerator := new(big.Int)
	addCostNumerator(numerator, *usage.InputTokens, rates.InputMicrosPerMillion)
	addCostNumerator(numerator, *usage.CachedInputTokens, rates.CachedInputMicrosPerMillion)
	addCostNumerator(numerator, *usage.OutputTokens, rates.OutputMicrosPerMillion)
	addCostNumerator(numerator, *usage.ReasoningTokens, rates.OutputMicrosPerMillion)

	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, million, remainder)
	if remainder.Cmp(big.NewInt(TokensPerMillion/2)) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return Calculation{}, ErrCostOverflow
	}
	estimated := quotient.Int64()
	return Calculation{
		Status: CostStatusPriced, Reason: CostReasonPriced, EstimatedUSDMicros: &estimated,
	}, nil
}

func addCostNumerator(total *big.Int, tokens int64, rate *int64) {
	if tokens == 0 || rate == nil {
		return
	}
	component := new(big.Int).Mul(big.NewInt(tokens), big.NewInt(*rate))
	total.Add(total, component)
}
