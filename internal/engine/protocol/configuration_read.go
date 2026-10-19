package protocol

import (
	"errors"
	"fmt"
	"slices"

	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

// RecoveryConfigurationReadFailure identifies the previous failed library read.
// Availability is derived from this fact and the current read, not persisted.
const RecoveryConfigurationReadFailure RecoveryAction = "CONFIGURATION_READ_FAILURE"

func (s *State) ConfigurationReadFailure() *RecoveryRecord {
	for i := len(s.RecoveryRecords) - 1; i >= 0; i-- {
		if s.RecoveryRecords[i].Action == RecoveryConfigurationReadFailure {
			return &s.RecoveryRecords[i]
		}
	}
	return nil
}

// configurationEvaluation shares one library sample across an operation's
// transactions. Its initial failure fact is not changed by its own commits.
type configurationEvaluation struct {
	sampled         bool
	value           settings.Configuration
	err             error
	assessed        bool
	failedBefore    bool
	failureRevision uint64
}

func (e *Engine) evaluatingConfiguration() *Engine {
	if e.configuration != nil || e.cfg.Configuration == nil {
		return e
	}
	local := *e
	sample := &configurationEvaluation{}
	local.configuration = sample
	read := e.cfg.Configuration
	local.cfg.Configuration = func(name string) (settings.Configuration, error) {
		if !sample.sampled {
			sample.value, sample.err = read(name)
			sample.sampled = true
		}
		var unavailable *ConfigurationUnavailable
		if sample.err != nil && !errors.As(sample.err, &unavailable) && sample.failedBefore {
			return sample.value, &ConfigurationUnavailable{Reason: fmt.Sprintf("configuration %q could not be read in consecutive evaluations: %v", name, sample.err)}
		}
		return sample.value, sample.err
	}
	return &local
}

func (e *Engine) assessConfigurationRead(s *State) {
	if e.configuration != nil && !e.configuration.assessed {
		e.configuration.failedBefore = s.ConfigurationReadFailure() != nil
		e.configuration.assessed = true
	}
}

func (e *Engine) recordConfigurationRead(s *State, nextRevision uint64) bool {
	if e.cfg.Configuration == nil || s.Phase == runtime.PhaseTerminal || s.Phase == runtime.PhasePreparing || s.abortRequested() || s.resetPending() || s.requirementChangePending() {
		return false
	}
	e.assessConfigurationRead(s)
	_, _ = e.cfg.Configuration(s.ConfigurationName())
	sample := e.configuration
	previous := s.ConfigurationReadFailure()
	if sample.err == nil {
		if previous == nil {
			return false
		}
		s.clearConfigurationReadFailure()
		return true
	}
	var unavailable *ConfigurationUnavailable
	if errors.As(sample.err, &unavailable) {
		return false
	}
	if sample.failureRevision == 0 {
		sample.failureRevision = nextRevision
	}
	failure := RecoveryRecord{Action: RecoveryConfigurationReadFailure, Detail: sample.err.Error(), Revision: sample.failureRevision}
	if previous != nil && previous.Detail == failure.Detail && previous.Revision == failure.Revision {
		return false
	}
	s.clearConfigurationReadFailure()
	s.RecoveryRecords = append(s.RecoveryRecords, failure)
	return true
}

func (s *State) clearConfigurationReadFailure() {
	s.RecoveryRecords = slices.DeleteFunc(s.RecoveryRecords, func(record RecoveryRecord) bool { return record.Action == RecoveryConfigurationReadFailure })
}
