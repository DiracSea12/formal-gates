package cost

import (
	"formal-gates/internal/host"
	"time"
)

// These existing public timing entrypoints retain their native adapter owner.
func CodexTaskTiming(path string) (TaskTiming, error) { return host.CodexTaskTiming(path) }
func CodexTaskTimingAfter(path string, after time.Time) (TaskTiming, error) {
	return host.CodexTaskTimingAfter(path, after)
}
