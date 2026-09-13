package facade

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/decision"
)

// ContinuousResult is a projection of one consumption call, not another task
// ledger. Claims, retries, native identities and results stay in the engine.
type ContinuousResult struct {
	Run      Run               `json:"run"`
	Boundary string            `json:"boundary"`
	Blocked  map[string]string `json:"blocked,omitempty"`
	External []string          `json:"external,omitempty"`
}

// DriveContinuous consumes already-issued work through the ordinary adapters.
// A dead owner's lease is reclaimable; each native action still owns its own
// lock and process journal. Restart therefore observes before attempting resume.
// Cancellation stops this consumer, not authorized native work: a subsequent
// consumer reconciles the same journals rather than creating replacements.
func (f *Facade) DriveContinuous(ctx context.Context) (ContinuousResult, error) {
	result := ContinuousResult{Blocked: map[string]string{}}
	if f == nil || f.engine == nil || ctx == nil {
		return result, fmt.Errorf("continuous execution requires a facade and context")
	}
	unlock, err := coordination.AcquirePath(filepath.Join(f.stateDir, "executor.lock"), "continuous executor")
	if err != nil {
		return result, err
	}
	defer unlock()
	type completion struct {
		id  string
		err error
	}
	done := make(chan completion, 64)
	active := map[string]bool{}
	waiting := map[string]bool{}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			result.Boundary = "INTERRUPTED"
			return result, err
		}
		run, err := f.Drive()
		if err != nil {
			if transientExecutorWait(err) {
				select {
				case <-ctx.Done():
					continue
				case <-ticker.C:
					continue
				}
			}
			return result, err
		}
		result.Run = run
		result.External = nil
		// Surface an explicit user request immediately. A branch-local recovery
		// boundary must not exit the CLI while an independent native invocation
		// is still owned by this process; closing its pipes interrupts that turn.
		userAsk := run.Regular != nil && run.Regular.Ask != nil && run.Regular.Ask.Kind != "qa-recovery"
		if userAsk || (run.Next.Kind == decision.KindAsk && len(active) == 0) {
			result.Boundary = "ASK"
			return result, nil
		}

		paused := run.Status == "COMPLETE" || run.Regular == nil || run.Regular.Ask != nil || run.Regular.ChangePending || run.Next.Kind == decision.KindOperator || run.Next.Kind == decision.KindAsk
		ids := make([]string, 0, len(run.Executions))
		for id, x := range run.Executions {
			if x.SupersededBy != "" {
				delete(waiting, id)
				delete(result.Blocked, id)
				continue
			}
			// A result may already be accepted while source/cost collection was
			// interrupted. That is still the same issued action, not a new model turn.
			unsettled := x.Claimed && (x.Source == nil || x.Source.Status == "RUNNING")
			if x.Finished && !unsettled {
				delete(waiting, id)
				delete(result.Blocked, id)
				continue
			}
			if x.Executor != nil && x.Executor.Kind == "external" {
				if !x.Finished {
					result.External = append(result.External, id)
				}
				continue
			}
			if paused && !unsettled {
				delete(waiting, id)
				continue
			}
			ids = append(ids, id)
		}
		sort.Strings(ids)
		sort.Strings(result.External)
		for _, id := range ids {
			if active[id] || result.Blocked[id] != "" {
				continue
			}
			x := run.Executions[id]
			if !x.Claimed && run.Regular.Guard.Status != "READY" {
				waiting[id] = true
				continue
			}
			// Capability capture/validation precede CLAIM and any model call. A
			// rejected platform never falls back to another provider or the owner.
			adapter, e := runtimeAdapter(x.Claim.Provider)
			if e == nil {
				binding := x.Claim.Context
				if binding == nil && !x.Claimed {
					binding, e = captureRuntimeContext(x.Claim.Provider, run.Regular.Settings.Values, f.stateDir, f.root)
				}
				if e == nil {
					e = adapter.validate(binding)
				}
			}
			if e != nil {
				result.Blocked[id] = e.Error()
				delete(waiting, id)
				continue
			}
			active[id] = true
			delete(waiting, id)
			resume := x.Claimed || x.Claim.Operation == "resume"
			go func(id string, resume bool) {
				_, e := f.ExecuteNative(id, resume)
				done <- completion{id, e}
			}(id, resume)
		}
		if len(active) == 0 && len(waiting) == 0 {
			switch {
			case paused:
				result.Boundary = string(run.Next.Kind)
			case len(result.Blocked) > 0:
				result.Boundary = "BLOCKED"
			case run.Regular.Preparation != nil && run.Regular.Preparation.Operation != nil:
				continue
			case len(result.External) > 0:
				result.Boundary = "EXTERNAL_EXECUTOR"
			default:
				result.Boundary = string(run.Next.Kind)
			}
			return result, nil
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		case finished := <-done:
			delete(active, finished.id)
			if finished.err == nil {
				delete(result.Blocked, finished.id)
				continue
			}
			if transientExecutorWait(finished.err) {
				waiting[finished.id] = true
			} else {
				// The engine alone grants a retry. UNKNOWN or capability errors are
				// reported without inventing a failure, attempt or replacement session.
				result.Blocked[finished.id] = finished.err.Error()
			}
		}
	}
}

func transientExecutorWait(err error) bool {
	message := err.Error()
	return strings.Contains(message, "HOST_ACTION_RUNNING") || strings.Contains(message, "COST_PENDING") || strings.Contains(message, "lock held") || strings.Contains(message, "STALE_FRESHNESS")
}
