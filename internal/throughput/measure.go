package throughput

// Measure 复用本机定点平均；调用方负责完整生命周期区间并集与归因。
func Measure(output, activeMS int64) Stats {
	if output < 0 || output > MaxInteger || activeMS <= 0 || activeMS > MaxInteger {
		return Stats{Status: "unavailable", Reason: "numeric_overflow"}
	}
	return measured(output, activeMS)
}
