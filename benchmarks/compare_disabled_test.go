//go:build !compare

package benchmarks

import "testing"

func compareIterativeFib(*testing.B, int32, int32) {}

func compareSieve(*testing.B, int32, int32) {}

func compareRecursiveFib(*testing.B, int32, int32) {}

func compareIndirectRecursiveFib(*testing.B, int32, int32) {}

func compareClosureCounter(*testing.B, int, int32) {}

func compareTypedArraySum(*testing.B, int32, int32) {}

func compareAllocationGraph(*testing.B, int32, int32) {}

func compareBranchTree(*testing.B, int32, int, int32) {}
