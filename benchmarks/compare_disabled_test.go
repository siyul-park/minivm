//go:build !compare

package benchmarks

import "testing"

func benchmarkCompare(*testing.B, benchmarkComparison, int32) {}
