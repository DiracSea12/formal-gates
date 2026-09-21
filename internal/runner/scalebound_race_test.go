//go:build race

package runner

// raceBuild is true under the race detector, whose instrumentation
// slows the pure-Go SQLite driver far past any honest wall-clock bound;
// TestK3ScaleThousandIndependentTasks then asserts correctness only and
// leaves the timing bound to the regular build.  Mirror:
// scalebound_norace_test.go.

const raceBuild = true
