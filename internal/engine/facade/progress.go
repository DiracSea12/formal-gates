package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"formal-gates/internal/engine/protocol"
)

// Progress is a disposable view of existing execution records, never an input
// to admission, scheduling, checkpoint acceptance or cost settlement.
type Progress struct {
	NativeCallSourcesUnavailable []string                  `json:"nativeCallSourcesUnavailable"`
	BlockedCount                 int                       `json:"blockedCount"`
	PendingCases                 map[string]QACaseProgress `json:"pendingCases"`
	RepeatedExecutions           []RepeatProgress          `json:"repeatedExecutions"`
	RunID                        string                    `json:"runId"`
	Revision                     uint64                    `json:"revision"`
	AfterRevision                uint64                    `json:"afterRevision"`
	Status                       string                    `json:"status"`
	ElapsedMillis                *int64                    `json:"elapsedMillis"`
	TimeSource                   string                    `json:"timeSource"`
	CountScope                   string                    `json:"countScope"`
	Completed                    int                       `json:"completed"`
	Running                      int                       `json:"running"`
	NotStarted                   int                       `json:"notStarted"`
	Blocked                      map[string]string         `json:"blocked"`
	Messages                     []TaskMessage             `json:"messages"`
	NewResults                   []OperationProgress       `json:"newResults"`
	Current                      []OperationProgress       `json:"current"`
	PreparationReused            int                       `json:"preparationReused"`
	PreparationExecuted          int                       `json:"preparationExecuted"`
	NativeCalls                  int                       `json:"observedNativeCalls"`
	Continuations                []ContinuationProgress    `json:"continuations"`
	Cost                         *CostProgress             `json:"cost,omitempty"`
	CostSource                   string                    `json:"costSource"`
	EvidenceRoot                 string                    `json:"evidenceRoot"`
}
type TaskMessage struct {
	ActionID  string `json:"actionId"`
	Identity  string `json:"identity"`
	Candidate string `json:"candidate"`
	Operation string `json:"operation"`
	Path      string `json:"path"`
}
type ContinuationProgress struct {
	ActionID string `json:"actionId"`
	Count    int    `json:"count"`
	Reason   string `json:"reason"`
}
type OperationProgress struct {
	Command           []string `json:"command,omitempty"`
	ExitCode          *int     `json:"exitCode,omitempty"`
	Candidate         string   `json:"candidate"`
	InputDigest       string   `json:"inputDigest,omitempty"`
	PreparationReason string   `json:"preparationReason,omitempty"`
	ActionID          string   `json:"actionId"`
	OperationID       string   `json:"operationId,omitempty"`
	CaseID            string   `json:"caseId,omitempty"`
	Stage             string   `json:"stage"`
	Status            string   `json:"status"`
	DurationMillis    *int64   `json:"durationMillis"`
	TimeSource        string   `json:"timeSource"`
	Evidence          string   `json:"evidence"`
	Error             string   `json:"error,omitempty"`
}

// ProgressSince uses the writer's accepted revision as a cursor. Repeated reads
// are side-effect free; a caller retains the returned revision for the next read.
func (f *Facade) ProgressSince(after uint64) (Progress, error) {
	run, err := f.Show()
	if err != nil {
		return Progress{}, err
	}
	if after > run.Revision {
		return Progress{}, fmt.Errorf("progress cursor exceeds current revision")
	}
	p := Progress{RunID: run.RunID, Revision: run.Revision, AfterRevision: after, Status: run.Status, TimeSource: "unavailable: run start not recorded", CountScope: "issued actions; blocked keys identify QA cases or actions", Blocked: map[string]string{}, PendingCases: map[string]QACaseProgress{}, RepeatedExecutions: []RepeatProgress{}, NativeCallSourcesUnavailable: []string{}, Messages: []TaskMessage{}, NewResults: []OperationProgress{}, Current: []OperationProgress{}, Cost: summarizeCost(run), CostSource: "internal/cost; totals include measured entries only; unavailable entries are not zero usage", EvidenceRoot: f.stateDir}
	now := time.Now().UTC()
	end := now
	if run.Status == "COMPLETE" {
		if summary, err := f.readSummary(); err == nil {
			end, _ = time.Parse(time.RFC3339Nano, summary.CompletedAt)
		} else {
			end = time.Time{}
		}
	}
	var start time.Time
	if run.Regular != nil {
		start = run.Regular.StartedAt
		if start.IsZero() {
			start = run.Regular.OwnerStartedAt
		}
	}
	if !start.IsZero() && !end.IsZero() {
		value := end.Sub(start).Milliseconds()
		if value >= 0 {
			p.ElapsedMillis = &value
			p.TimeSource = "regular.startedAt (legacy ownerStartedAt fallback) to observation/terminal-summary.completedAt; includes waits"
		}
	}
	ids := make([]string, 0, len(run.Executions))
	for id := range run.Executions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		x := run.Executions[id]
		switch {
		case x.Finished:
			p.Completed++
		case x.Claimed:
			p.Running++
		default:
			p.NotStarted++
		}
		jobs := nativeProgressJobs(f.codexJob(id), x.ResumeCount)
		currentJob := jobs[0]
		for _, call := range jobs {
			if call.prompt {
				p.Messages = append(p.Messages, TaskMessage{id, x.Identity, x.Claim.Candidate, call.operation, filepath.Join(call.job, "prompt.txt")})
			}
			if call.process {
				p.NativeCalls++
			} else if x.Claimed && call.expected {
				p.NativeCallSourcesUnavailable = append(p.NativeCallSourcesUnavailable, filepath.Join(call.job, "process.json"))
			}
			if call.expected {
				currentJob = call
			}
		}
		if x.ResumeCount > 0 {
			reason := "same-identity continuation; detailed reason unavailable"
			if x.Recovery != nil {
				reason = x.Recovery.NativeMethod + ": " + x.Recovery.Outcome() + "; " + x.Recovery.TransportError
			}
			p.Continuations = append(p.Continuations, ContinuationProgress{id, x.ResumeCount, reason})
		}
		if !x.Finished && x.Source != nil && x.Source.Reason != "" {
			p.Blocked[id] = x.Source.Reason
		}
		active := false
		opIDs := make([]string, 0, len(x.Operations))
		for opID := range x.Operations {
			opIDs = append(opIDs, opID)
		}
		sort.Strings(opIDs)
		for _, opID := range opIDs {
			op := x.Operations[opID]
			job := f.taskOperationDir(opID)
			view := OperationProgress{Candidate: x.Claim.Candidate, ActionID: id, OperationID: opID, CaseID: op.Request.CaseID, Stage: op.Request.Kind, Status: "RUNNING", Evidence: job, TimeSource: "unavailable: operation timestamps absent"}
			if op.Command != nil {
				view.Command = append([]string{op.Command.Executable}, op.Command.Args...)
			}
			view.DurationMillis, view.TimeSource = operationDuration(job, now)
			if op.Receipt == nil {
				active = true
				p.Current = append(p.Current, view)
				continue
			}
			if op.Receipt.StartedAt != "" && op.Receipt.CompletedAt != "" {
				view.DurationMillis, view.TimeSource = intervalDuration(op.Receipt.StartedAt, op.Receipt.CompletedAt, filepath.Join(job, "observed.json"))
			}
			if op.Request.Kind == "qa.target" && op.Receipt.Status == "COMPLETED" {
				code := op.Receipt.ExitCode
				view.ExitCode = &code
			}
			view.InputDigest = op.Receipt.InputDigest
			view.Status = op.Receipt.Status
			view.Error = op.Receipt.Message
			if view.Status == "COMPLETED" {
				view.Error = ""
			}
			if op.Request.Kind == "qa.prepare" && op.Receipt.Status == "COMPLETED" {
				if op.Receipt.PreparationReused {
					p.PreparationReused++
					view.PreparationReason = "reused preparation with matching recorded input binding"
				} else {
					p.PreparationExecuted++
					view.PreparationReason = "executed preparation; input binding and commands in receipt/request evidence"
				}
			}
			if op.Request.Kind == "model.run" {
				if _, err := os.Stat(filepath.Join(job, "process.json")); err == nil {
					p.NativeCalls++
				}
			}
			if op.CompletedRevision > after || (after == 0 && op.CompletedRevision == 0) {
				p.NewResults = append(p.NewResults, view)
			}
			if view.Error != "" && !x.Finished {
				if latest := latestTaskOperation(x, op.Request.Kind, op.Request.CaseID); latest != nil && latest.ID == opID {
					p.Blocked[opID] = view.Error
				}
			}
		}
		if x.Claimed && !x.Finished && !active {
			job := currentJob.job
			duration, source := operationDuration(job, now)
			stage := "native wait"
			if currentJob.coverage {
				stage = "whitebox.coverage"
			}
			var process codexProcess
			if data, err := os.ReadFile(filepath.Join(job, "process.json")); err == nil && json.Unmarshal(data, &process) == nil && process.Stopped {
				stage = "result handling"
				if currentJob.coverage {
					stage = "whitebox.coverage result handling"
				}
				duration, source = intervalDuration(process.CompletedAt, now.Format(time.RFC3339Nano), filepath.Join(job, "process.json")+" native completion to result handling observation")
			}
			p.Current = append(p.Current, OperationProgress{Candidate: x.Claim.Candidate, ActionID: id, Stage: stage, Status: "RUNNING", DurationMillis: duration, TimeSource: source, Evidence: job})
		}
	}
	// Derive dependency and resource reasons through the existing pending view.
	snapshot := &protocol.State{RunID: run.RunID, Regular: run.Regular, RegularExecutions: run.Executions}
	if run.Regular != nil {
		for _, id := range ids {
			x := run.Executions[id]
			if x.Finished {
				continue
			}
			binding := x.Claim
			binding.Identity = x.Identity
			pending, err := protocol.PendingQA(snapshot, binding, "")
			if err == nil {
				p.PendingCases[id] = QACaseProgress{Remaining: len(pending.Ready) + len(pending.Running) + len(pending.Judgment) + len(pending.Blocked), Ready: len(pending.Ready), Running: len(pending.Running), AwaitingJudgment: len(pending.Judgment), Blocked: len(pending.Blocked)}
				for key, reason := range pending.Blocked {
					p.Blocked[key] = reason
				}
			}
		}
	}
	p.BlockedCount = len(p.Blocked)
	p.RepeatedExecutions = repeatedOperations(run)
	return p, nil
}

// Coverage runs before the main turn in each initial/resume job. Project only
// existing invocation files for coverage; creating a directory is not a call.
type nativeProgressJob struct {
	job, operation                      string
	coverage, prompt, process, expected bool
}

func nativeProgressJobs(base string, resumes int) []nativeProgressJob {
	fileExists := func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir()
	}
	var jobs []nativeProgressJob
	for n := 0; n <= resumes; n++ {
		job, operation := base, "spawn"
		if n > 0 {
			job, operation = codexResumeJob(base, n), "resume"
		}
		coverage := filepath.Join(job, "coverage")
		coveragePrompt := fileExists(filepath.Join(coverage, "prompt.txt"))
		coverageProcess := fileExists(filepath.Join(coverage, "process.json"))
		hasCoverage := coveragePrompt || coverageProcess
		if hasCoverage {
			jobs = append(jobs, nativeProgressJob{coverage, "coverage." + operation, true, coveragePrompt, coverageProcess, true})
			// The bridge continues the identity returned by coverage.
			operation = "resume"
		}
		prompt := fileExists(filepath.Join(job, "prompt.txt"))
		process := fileExists(filepath.Join(job, "process.json"))
		// While coverage runs, the main turn has not been invoked yet.
		jobs = append(jobs, nativeProgressJob{job, operation, false, prompt, process, !hasCoverage || prompt || process})
	}
	return jobs
}

func operationDuration(job string, now time.Time) (*int64, string) {
	var process struct {
		StartedAt   string `json:"startedAt"`
		CompletedAt string `json:"completedAt"`
		Stopped     bool   `json:"stopped"`
	}
	data, err := os.ReadFile(filepath.Join(job, "process.json"))
	if err != nil || json.Unmarshal(data, &process) != nil {
		var request protocol.TaskOperation
		if data, err := os.ReadFile(filepath.Join(job, "request.json")); err == nil && json.Unmarshal(data, &request) == nil && request.StartedAt != "" {
			return intervalDuration(request.StartedAt, now.Format(time.RFC3339Nano), filepath.Join(job, "request.json"))
		}
		return nil, "unavailable: native command timestamps absent"
	}
	start, err := time.Parse(time.RFC3339Nano, process.StartedAt)
	if err != nil {
		return nil, "unavailable: native command start absent"
	}
	end := now
	if process.Stopped {
		end, err = time.Parse(time.RFC3339Nano, process.CompletedAt)
		if err != nil {
			return nil, "unavailable: native command completion absent"
		}
	}
	value := end.Sub(start).Milliseconds()
	if value < 0 {
		return nil, "unavailable: inconsistent command timestamps"
	}
	return &value, filepath.Join(job, "process.json") + "; wall interval includes tool waits, not added across parallel tasks"
}

func intervalDuration(started, completed, source string) (*int64, string) {
	start, err := time.Parse(time.RFC3339Nano, started)
	if err != nil {
		return nil, "unavailable: invalid start timestamp"
	}
	end, err := time.Parse(time.RFC3339Nano, completed)
	if err != nil || end.Before(start) {
		return nil, "unavailable: invalid completion timestamp"
	}
	value := end.Sub(start).Milliseconds()
	return &value, source + "; elapsed interval includes waits, never sum overlapping operations"
}

// These are the ledger's measured subtotals, not a second accumulation.
type CostProgress struct {
	MeasuredInputTokens     int64             `json:"measuredInputTokens"`
	MeasuredCacheHitTokens  int64             `json:"measuredCacheHitTokens"`
	MeasuredCacheMissTokens int64             `json:"measuredCacheMissTokens"`
	MeasuredOutputTokens    int64             `json:"measuredOutputTokens"`
	Unavailable             map[string]string `json:"unavailable"`
}

func summarizeCost(run Run) *CostProgress {
	if run.Cost == nil {
		return nil
	}
	c := run.Cost
	p := &CostProgress{MeasuredInputTokens: c.TotalInputTokens, MeasuredCacheHitTokens: c.InputCacheHitTokens, MeasuredCacheMissTokens: c.InputCacheMissTokens, MeasuredOutputTokens: c.OutputTokens, Unavailable: map[string]string{}}
	for id, entry := range c.Dispatches {
		if entry.Source != "transcript" {
			reason := entry.SourceReason
			if reason == "" {
				reason = "unavailable: transcript source diagnosis absent"
			}
			p.Unavailable[id] = reason
		}
	}
	if c.Owner == nil {
		p.Unavailable["owner"] = "unavailable: owner interval not settled"
	} else if c.Owner.Source != "transcript" {
		reason := c.Owner.SourceReason
		if reason == "" {
			reason = "unavailable: owner source diagnosis absent"
		}
		p.Unavailable["owner"] = reason
	}
	return p
}

type RepeatProgress struct {
	OperationID         string `json:"operationId"`
	PreviousOperationID string `json:"previousOperationId"`
	CaseID              string `json:"caseId"`
	Kind                string `json:"kind"`
	Reason              string `json:"reason"`
}

func repeatedOperations(run Run) []RepeatProgress {
	ops := []protocol.TaskOperation{}
	for _, x := range run.Executions {
		for _, op := range x.Operations {
			if op.Receipt != nil && (op.Request.Kind == "qa.prepare" || op.Request.Kind == "qa.target") {
				ops = append(ops, op)
			}
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].ClaimedRevision == ops[j].ClaimedRevision {
			return ops[i].ID < ops[j].ID
		}
		return ops[i].ClaimedRevision < ops[j].ClaimedRevision
	})
	previous := map[string]protocol.TaskOperation{}
	result := []RepeatProgress{}
	for _, op := range ops {
		key := op.Request.Kind + "/" + op.Request.CaseID
		if old, ok := previous[key]; ok {
			reason := "unavailable: explicit repeat reason absent; inspect bound requests and checkpoint history"
			if old.Request.Binding.Candidate != op.Request.Binding.Candidate {
				reason = "candidate changed: " + old.Request.Binding.Candidate + " -> " + op.Request.Binding.Candidate + "; authorization is in bound request"
			} else if old.CheckpointDigest != op.CheckpointDigest {
				reason = "checkpoint binding changed; inspect retained invalidation history"
			} else if old.Receipt.InputDigest != op.Receipt.InputDigest {
				reason = "preparation input binding changed; inspect preparation evidence"
			}
			result = append(result, RepeatProgress{op.ID, old.ID, op.Request.CaseID, op.Request.Kind, reason})
		}
		previous[key] = op
	}
	return result
}

type QACaseProgress struct {
	Remaining        int `json:"remaining"`
	Ready            int `json:"ready"`
	Running          int `json:"running"`
	AwaitingJudgment int `json:"awaitingJudgment"`
	Blocked          int `json:"blocked"`
}
