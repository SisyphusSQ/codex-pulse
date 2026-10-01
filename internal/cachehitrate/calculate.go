// Package cachehitrate 共享 Codex 生命周期缓存输入比例的精确整数口径。
package cachehitrate

import "math/big"

// BasisPoints 只接受有效的已知输入，cached 已包含在 input 中；100 表示 1%。
func BasisPoints(input, cached int64) *int64 {
	if input <= 0 || cached < 0 || cached > input {
		return nil
	}
	numerator := new(big.Int).Mul(big.NewInt(cached), big.NewInt(10_000))
	numerator.Add(numerator, big.NewInt(input/2))
	numerator.Quo(numerator, big.NewInt(input))
	return new(numerator.Int64())
}
