//go:build !race

package runner

// raceBuild mirrors scalebound_race_test.go: false in regular builds,
// where the scale test's timing bound applies.

const raceBuild = false
