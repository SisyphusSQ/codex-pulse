package pricing

import "math/big"

var million = big.NewInt(TokensPerMillion)

// Calculate 对全部 token×rate numerator 精确求和后只做一次 round-half-up。
// Codex 的 reasoning token 是独立 output 类计数，因此与 output 共用 output rate。
func Calculate(usage Usage, rates Rates) (Calculation, error) {
	result, err := CalculateExact(ExactUsage{InputTokens: exactToken(usage.InputTokens), CachedInputTokens: exactToken(usage.CachedInputTokens), OutputTokens: exactToken(usage.OutputTokens), ReasoningTokens: exactToken(usage.ReasoningTokens)}, rates)
	if err != nil {
		return Calculation{}, err
	}
	out := Calculation{Status: result.Status, Reason: result.Reason}
	if result.EstimatedUSDMicros != nil {
		if !result.EstimatedUSDMicros.IsInt64() {
			return Calculation{}, ErrCostOverflow
		}
		out.EstimatedUSDMicros = new(result.EstimatedUSDMicros.Int64())
	}
	return out, nil
}
func exactToken(value *int64) *big.Int {
	if value == nil {
		return nil
	}
	return big.NewInt(*value)
}

// CalculateExact 与 Calculate 共用同一公式，精确金额可以超过 int64。
func CalculateExact(usage ExactUsage, rates Rates) (ExactCalculation, error) {
	for _, value := range []*big.Int{usage.InputTokens, usage.CachedInputTokens, usage.OutputTokens, usage.ReasoningTokens} {
		if value != nil && value.Sign() < 0 {
			return ExactCalculation{}, ErrInvalidCalculation
		}
	}
	for _, value := range []*int64{rates.InputMicrosPerMillion, rates.CachedInputMicrosPerMillion, rates.OutputMicrosPerMillion} {
		if value != nil && *value < 0 {
			return ExactCalculation{}, ErrInvalidCalculation
		}
	}
	if usage.InputTokens == nil || usage.CachedInputTokens == nil || usage.OutputTokens == nil || usage.ReasoningTokens == nil {
		return ExactCalculation{Status: CostStatusUnpriced, Reason: CostReasonMissingToken}, nil
	}
	if usage.CachedInputTokens.Cmp(usage.InputTokens) > 0 {
		return ExactCalculation{}, ErrInvalidCalculation
	}
	uncached := new(big.Int).Sub(usage.InputTokens, usage.CachedInputTokens)
	if (uncached.Sign() > 0 && rates.InputMicrosPerMillion == nil) || (usage.CachedInputTokens.Sign() > 0 && rates.CachedInputMicrosPerMillion == nil) || ((usage.OutputTokens.Sign() > 0 || usage.ReasoningTokens.Sign() > 0) && rates.OutputMicrosPerMillion == nil) {
		return ExactCalculation{Status: CostStatusUnpriced, Reason: CostReasonMissingPriceComponent}, nil
	}
	numerator := new(big.Int)
	addExactNumerator(numerator, uncached, rates.InputMicrosPerMillion)
	addExactNumerator(numerator, usage.CachedInputTokens, rates.CachedInputMicrosPerMillion)
	addExactNumerator(numerator, usage.OutputTokens, rates.OutputMicrosPerMillion)
	addExactNumerator(numerator, usage.ReasoningTokens, rates.OutputMicrosPerMillion)
	return ExactCalculation{Status: CostStatusPriced, Reason: CostReasonPriced, EstimatedUSDMicros: RoundMicroUSD(numerator)}, nil
}

// RoundMicroUSD 对非负 token×每百万 token 微美元的合计只做一次 HALF-UP。
func RoundMicroUSD(numerator *big.Int) *big.Int {
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, million, remainder)
	if remainder.Cmp(big.NewInt(TokensPerMillion/2)) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}
func addExactNumerator(total, tokens *big.Int, rate *int64) {
	if tokens.Sign() == 0 || rate == nil {
		return
	}
	total.Add(total, new(big.Int).Mul(tokens, big.NewInt(*rate)))
}
