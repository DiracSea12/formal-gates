package protocol

import (
	"fmt"
	"formal-gates/internal/cost"
	"formal-gates/internal/host"
	"time"
)

func checkNativeTranscriptIdentity(provider, path, identity string) error {
	d, err := host.Lookup(provider)
	if err != nil || d.CheckIdentity == nil {
		return fmt.Errorf("COST_PENDING: no native source adapter for %s", provider)
	}
	return d.CheckIdentity(path, identity)
}
func nativeTranscriptTiming(provider, path string, after time.Time) (cost.TaskTiming, error) {
	d, err := host.Lookup(provider)
	if err != nil || d.ReadTiming == nil {
		return cost.TaskTiming{}, fmt.Errorf("native transcript timing unavailable for %s", provider)
	}
	return d.ReadTiming(path, after)
}
