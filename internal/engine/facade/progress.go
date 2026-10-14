package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"formal-gates/internal/engine/protocol"
)

// Progress is a read-only view of authoritative state and original job evidence.
// Durations include waits and remain individual intervals, never parallel sums.
type Progress struct {
	Root                string              `json:"root"`
	Failed              int                 `json:"failed"`
	Retired             int                 `json:"retired"`
	CountScope          string              `json:"countScope"`
	RunID               string              `json:"runId"`
	Revision            uint64              `json:"revision"`
	AfterRevision       uint64              `json:"afterRevision"`
	ElapsedMillis       *int64              `json:"elapsedMillis"`
	TimeSource          string              `json:"timeSource"`
	Completed           int                 `json:"completed"`
	Running             int                 `json:"running"`
	NotStarted          int                 `json:"notStarted"`
	Blocked             map[string]string   `json:"blocked"`
	Current             []OperationProgress `json:"current"`
	NewResults          []ResultProgress    `json:"newResults"`
	PreparationReused   int                 `json:"preparationReused"`
	PreparationExecuted int                 `json:"preparationExecuted"`
	NativeCalls         int                 `json:"observedNativeCalls"`
	NativeCallSource    string              `json:"nativeCallSource"`
	RepeatedExecutions  []RepeatProgress    `json:"repeatedExecutions"`
}
type OperationProgress struct {
	ActionID       string `json:"actionId"`
	Stage          string `json:"stage"`
	Status         string `json:"status"`
	DurationMillis *int64 `json:"durationMillis"`
	TimeSource     string `json:"timeSource"`
	Evidence       string `json:"evidence"`
}
type ResultProgress struct {
	ActionID string `json:"actionId"`
	Revision uint64 `json:"revision"`
	Status   string `json:"status"`
}
type RepeatProgress struct {
	ActionID string `json:"actionId"`
	Calls    int    `json:"calls"`
	Reason   string `json:"reason"`
}

func (f *Facade) ProgressSince(after uint64, observe func(string) bool) (Progress, error) {
	snap, err := f.engine.Load()
	var state *protocol.State
	revision := snap.Revision
	end := time.Now().UTC()
	if err == nil {
		state = snap.State
	} else if isMissingEngineState(err) {
		summary, e := f.readSummary()
		if e != nil {
			return Progress{}, e
		}
		state = summary.Termination
		revision = summary.Revision
		end, _ = time.Parse(time.RFC3339Nano, summary.CompletedAt)
	} else {
		return Progress{}, err
	}
	if after > revision {
		return Progress{}, fmt.Errorf("progress cursor exceeds current revision")
	}
	p := Progress{Root: f.root, RunID: f.runID, Revision: revision, AfterRevision: after, TimeSource: "unavailable: run start not recorded", NativeCallSource: "native-process.json: observed native invocations, not billable model requests", CountScope: "issued worker actions; accepted completion, failed result, retired/replaced action and native calls are distinct", Blocked: map[string]string{}, Current: []OperationProgress{}, NewResults: []ResultProgress{}, RepeatedExecutions: []RepeatProgress{}}
	if state == nil {
		return p, nil
	}
	if start, e := time.Parse(time.RFC3339Nano, state.StartedAt); e == nil && !end.IsZero() && !end.Before(start) {
		duration := end.Sub(start).Milliseconds()
		p.ElapsedMillis = &duration
		p.TimeSource = "state.startedAt to observation/terminal-summary.completedAt; includes waits"
	}
	ids := []string{}
	for id := range state.DispatchIssueRevisions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		pending, active := state.PendingActions[id]

		jobs, e := os.ReadDir(workerActionDirectory(f.stateDir, id))
		if e != nil && !os.IsNotExist(e) {
			return Progress{}, e
		}
		calls := 0
		var current *OperationProgress
		for _, job := range jobs {
			if !job.IsDir() {
				continue
			}
			directory := filepath.Join(workerActionDirectory(f.stateDir, id), job.Name())
			_, exists, e := progressProcess(directory, "native-process.json")
			if e != nil {
				return Progress{}, e
			}
			if !exists {
				continue
			}
			calls++
			p.NativeCalls++
			process, _, e := progressProcess(directory, "process.json")
			if e != nil {
				return Progress{}, e
			}
			view := OperationProgress{ActionID: id, Stage: "native waiting", Status: "BLOCKED", Evidence: directory, TimeSource: "unavailable: native timing source missing"}
			stopped, finished, e := progressProcess(directory, "exit.json")
			if e != nil {
				return Progress{}, e
			}
			if !finished && observe != nil && observe(directory) {
				view.Status = "RUNNING"
			}
			if finished {
				view.Stage = "result processing"
				view.Status = "BLOCKED"
				if message, _ := stopped["error"].(string); message != "" {
					p.Blocked[id] = message
				}
			}
			view.DurationMillis, view.TimeSource = progressDuration(process, stopped, end)
			if current == nil || view.Status == "RUNNING" || progressEpoch(process["started_at"]) > progressDirectoryStart(current.Evidence) {
				copy := view
				current = &copy
			}
		}
		if calls > 1 {
			reason := "same action continuation or recovery; original invocation requests retain the exact reason"
			for _, record := range state.RecoveryRecords {
				if record.ActionID == id {
					reason = record.Detail
					if record.InterruptionReason != "" {
						reason = record.InterruptionReason
					}
				}
			}
			p.RepeatedExecutions = append(p.RepeatedExecutions, RepeatProgress{id, calls, reason})
		}
		if !active {
			result, accepted := state.Results[id]
			switch {
			case accepted && result.Outcome == protocol.OutcomePass && (result.Validation == nil || result.Validation.Status != "CONTINUE"):
				p.Completed++
			case accepted && (result.Outcome == protocol.OutcomeFail || result.Outcome == protocol.OutcomeRuntimeError):
				p.Failed++
			default:
				p.Retired++
			}
			continue
		}
		switch {
		case current != nil && current.Status == "RUNNING":
			p.Running++
			p.Current = append(p.Current, *current)
		case current != nil:
			p.Blocked[id] = firstProgressReason(p.Blocked[id], "native invocation stopped; result or recovery pending")
			p.Current = append(p.Current, *current)
		default:
			p.NotStarted++
			if receipt, ok := state.SpawnReceipts[id]; ok && receipt.Status != protocol.SpawnStatusSpawned && receipt.Status != protocol.SpawnStatusAttached {
				p.Blocked[id] = receipt.Status + ": native start not established"
			}
		}
		for _, record := range state.RecoveryRecords {
			if record.ActionID == pending.ActionID && record.RequestID != "" {
				if ask, known := state.PendingAsks[record.RequestID]; known && !ask.Resolved {
					p.Blocked[id] = firstProgressReason(record.InterruptionReason, record.Detail)
				}
			}
		}
	}
	for _, event := range state.Events {
		a := event.Acceptance
		if a.Kind == string(protocol.KindWorkerResult) && a.Revision > after {
			p.NewResults = append(p.NewResults, ResultProgress{a.ActionID, a.Revision, a.Status})
		}
	}
	sort.Slice(p.NewResults, func(i, j int) bool { return p.NewResults[i].Revision < p.NewResults[j].Revision })
	for id, receipt := range state.HostActionReceipts {
		if receipt.Status != protocol.HostActionStatusExecuted || receipt.AdapterOperation != "op.check" || receipt.AdapterEvidence == nil {
			continue
		}
		intent, pending := state.PendingHostActions[id]
		if !pending {
			effect := state.ReconciledEffects[id]
			if effect.Intent == nil {
				continue
			}
			intent = *effect.Intent
		}
		var params protocol.CheckParameters
		if err := decodeNativeParams(intent, &params); err != nil {
			return Progress{}, err
		}
		if params.QA == nil || params.QA.Preparation == nil {
			continue
		}
		if source, _ := receipt.AdapterEvidence.Values["preparationSourceAction"].(string); source != "" {
			p.PreparationReused++
		} else {
			p.PreparationExecuted++
		}
	}
	for id, intent := range state.ExecutableHostActions(f.definition) {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.check" {
			continue
		}
		directory := filepath.Join(f.stateDir, "checks", strings.ReplaceAll(id, ":", "-"))
		view := OperationProgress{ActionID: id, Stage: "command", Status: "NOT_STARTED", Evidence: directory, TimeSource: "unavailable: command timing source missing"}
		process, exists, e := progressProcess(directory, "process.json")
		if e != nil {
			return Progress{}, e
		}
		exit, finished, e := progressProcess(directory, "exit.json")
		if e != nil {
			return Progress{}, e
		}
		if exists {
			view.Status = "BLOCKED"
			if !finished && observe != nil && observe(directory) {
				view.Status = "RUNNING"
			}
			view.DurationMillis, view.TimeSource = progressDuration(process, exit, end)
		}
		if params, ok := intent.Adapter.Params["qa"].(map[string]any); ok {
			if source, _ := params["source"].(string); strings.HasPrefix(source, "shared:") {
				view.Stage = "preparation"
			}
		}
		p.Current = append(p.Current, view)
	}
	sort.Slice(p.Current, func(i, j int) bool { return p.Current[i].ActionID < p.Current[j].ActionID })
	return p, nil
}
func firstProgressReason(first, fallback string) string {
	if first != "" {
		return first
	}
	return fallback
}
func progressEpoch(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case json.Number:
		n, _ := v.Float64()
		return n
	}
	return 0
}
func progressProcess(directory, name string) (map[string]any, bool, error) {
	raw, err := os.ReadFile(filepath.Join(directory, name))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, false, err
	}
	return values, true, nil
}
func progressDirectoryStart(directory string) float64 {
	value, _, _ := progressProcess(directory, "process.json")
	return progressEpoch(value["started_at"])
}
func progressDuration(process, exit map[string]any, now time.Time) (*int64, string) {
	start := progressEpoch(process["started_at"])
	end := progressEpoch(exit["finished_at"])
	if end == 0 {
		end = float64(now.UnixNano()) / 1e9
	}
	if start <= 0 || end < start {
		return nil, "unavailable: timestamps missing"
	}
	duration := int64((end - start) * 1000)
	return &duration, "original process.started_at to exit.finished_at or observation; includes waits"
}
