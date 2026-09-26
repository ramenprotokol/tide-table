package almanac

import "testing"

// Benchmarks for the heaviest and a typical request (go test -bench .).
func benchCompute(b *testing.B, expr, mode string) {
	for i := 0; i < b.N; i++ {
		Compute(Request{Expr: expr, Zones: []string{"America/New_York", "Europe/London", "Australia/Sydney"}, Mode: mode, Now: 1790000000000})
	}
}

func BenchmarkEveryMinuteEachZone(b *testing.B) { benchCompute(b, "* * * * *", "each") }
func BenchmarkEvery15Shared(b *testing.B)       { benchCompute(b, "*/15 * * * *", "shared") }
func BenchmarkWeekdaysShared(b *testing.B)      { benchCompute(b, "0 9 * * 1-5", "shared") }
