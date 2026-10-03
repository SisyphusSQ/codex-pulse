// Package cachehitrate 共享 Codex 生命周期缓存输入比例的精确整数口径。
package cachehitrate

import "math/big"

// BasisPoints 只接受有效的已知输入，cached 已包含在 input 中；100 表示 1%。
func BasisPoints(input, cached int64) *int64 {
	return BasisPointsBig(big.NewInt(input), big.NewInt(cached))
}

// BasisPointsBig 使用同一四舍五入口径处理跨会话汇总，不修改输入的大整数。
func BasisPointsBig(input, cached *big.Int) *int64 {
	if input == nil || cached == nil || input.Sign() <= 0 || cached.Sign() < 0 || cached.Cmp(input) > 0 {
		return nil
	}
	numerator := new(big.Int).Mul(cached, big.NewInt(10_000))
	numerator.Add(numerator, new(big.Int).Quo(input, big.NewInt(2)))
	numerator.Quo(numerator, input)
	return new(numerator.Int64())
}
