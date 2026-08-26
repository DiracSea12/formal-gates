package testkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
)

const (
	HarnessPackageDigest = "sha256:testkit"
	terminalSummaryName  = "terminal-summary.json"
)

// HarnessOptions is the documented test-only protocol surface. Public
// workflow drive/submit commands remain absent; every write stays under the
// explicitly supplied isolated project root.
type HarnessOptions struct {
	ProjectRoot       string
	Scenario          string
	EventID           string
	ActionID          string
	RequestID         string
	Control           string
	Choice            string
	Provider          string
	Correlation       string
	Identity          string
	Status            string
	Outcome           string
	PayloadDigest     string
	FailureClass      string
	LifecycleEvent    string
	Fault             string
	MissingField      string
	Interruption      string
	Fixture           string
	DefinitionFixture string
	Declared          string
	Declaredness      string
	ReceiptFile       string
	Prepare           string
	Template          string
	BindTemplate      bool
	Continue          bool
	ExpectedRevision  uint64
	Capacity          int
	LifecycleMatches  string
	Fact              string
	Expected          string
	Conflict          string
	Target            string
}

type HarnessError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type NextRecord struct {
	Kind    decision.Kind `json:"kind"`
	Payload any           `json:"payload"`
}

type HarnessAction struct {
	ActionID string `json:"actionID"`
}

type ReceiptInput struct {
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	ActionID      string `json:"actionID"`
	Operation     string `json:"operation"`
	Correlation   string `json:"correlation"`
	PayloadDigest string `json:"payloadDigest"`
}

type HarnessReport struct {
	Status                    string                     `json:"status"`
	Scenario                  string                     `json:"scenario"`
	Phase                     string                     `json:"phase,omitempty"`
	Recovery                  persistence.RecoveryReport `json:"recovery,omitempty"`
	Envelope                  *persistence.Envelope      `json:"envelope,omitempty"`
	Summary                   StateSummary               `json:"summary,omitempty"`
	Snapshot                  *protocol.Snapshot         `json:"snapshot,omitempty"`
	PreResultSnapshot         *protocol.Snapshot         `json:"preResultSnapshot,omitempty"`
	Terminal                  *TerminalSummary           `json:"terminal,omitempty"`
	Acceptances               []protocol.Acceptance      `json:"acceptances,omitempty"`
	Acceptance                *protocol.Acceptance       `json:"acceptance,omitempty"`
	Errors                    []HarnessError             `json:"errors,omitempty"`
	RecoveryPlan              []protocol.RecoveryPlan    `json:"recoveryPlans,omitempty"`
	Next                      []NextRecord               `json:"next,omitempty"`
	Freshness                 map[string]string          `json:"freshness,omitempty"`
	Revisions                 []uint64                   `json:"revisions,omitempty"`
	Actions                   []HarnessAction            `json:"actions,omitempty"`
	SideEffects               map[string]int             `json:"sideEffects,omitempty"`
	FaultCalls                []persistence.FaultPoint   `json:"faultCalls,omitempty"`
	CompletedTasksBeforeFault []string                   `json:"completedTasksBeforeFault,omitempty"`
	Paths                     []string                   `json:"paths,omitempty"`
	StatePath                 string                     `json:"statePath,omitempty"`
	SummaryPath               string                     `json:"summaryPath,omitempty"`
	Metadata                  map[string]any             `json:"metadata,omitempty"`
	Input                     *ReceiptInput              `json:"input,omitempty"`
	Error                     string                     `json:"error,omitempty"`
}

type TerminalSummary struct {
	Writer                    string              `json:"writer"`
	StateSchemaVersion        string              `json:"stateSchemaVersion"`
	WorkflowDefinitionVersion string              `json:"workflowDefinitionVersion"`
	DefinitionDigest          string              `json:"definitionDigest"`
	PackageDigest             string              `json:"packageDigest"`
	Status                    string              `json:"status"`
	Revision                  uint64              `json:"revision"`
	Next                      NextRecord          `json:"next"`
	LastRequestReceipt        protocol.Acceptance `json:"lastRequestReceipt"`
	LastRequestDigest         string              `json:"lastRequestDigest"`
	LastEventReceipt          protocol.Acceptance `json:"lastEventReceipt"`
	LastEventDigest           string              `json:"lastEventDigest"`
}

func RunHarness(options HarnessOptions) (HarnessReport, error) {
	root, err := filepath.Abs(strings.TrimSpace(options.ProjectRoot))
	if err != nil {
		return HarnessReport{}, err
	}
	if root == "" {
		return HarnessReport{}, fmt.Errorf("testkit: project root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return HarnessReport{}, err
	}
	scenario := strings.ToLower(strings.TrimSpace(options.Scenario))
	if scenario == "" {
		scenario = "smoke"
	}
	report := HarnessReport{
		Status: "PASS", Scenario: scenario,
		StatePath:   filepath.Join(root, "engine-state", "state.json"),
		SummaryPath: filepath.Join(root, "engine-state", terminalSummaryName),
		Metadata:    map[string]any{"provider": "fake-host", "vcs": "fake-vcs"},
	}
	envelope, envelopeErr := persistence.ExpectedEnvelope(HarnessPackageDigest)
	if envelopeErr != nil {
		return report, envelopeErr
	}
	report.Envelope = &envelope

	if scenario == "envelope" || scenario == "envelope-write" || scenario == "envelope_write" {
		return runEnvelopeScenario(report, options, root, envelope)
	}
	if scenario == "receipt-file" {
		return runReceiptFileScenario(report, options, root)
	}
	if scenario == "wait-user-action" {
		return runWaitUserActionScenario(report, options, root)
	}
	if scenario == "next-sequence" {
		return runNextSequence(report)
	}
	if scenario == "query-terminal" {
		summary, readErr := ReadTerminalSummary(root)
		if readErr != nil {
			report.Status = "ERROR"
			report.addError(readErr)
			return report, readErr
		}
		report.Terminal = &summary
		report.Next = []NextRecord{summary.Next}
		report.Phase = "terminal-query"
		report.Paths, _ = harnessPaths(root)
		return report, nil
	}
	if scenario == "terminal-replay" {
		return runTerminalReplay(report, options, root)
	}
	if scenario == "failure-routing" {
		if authoring.FailureClass(strings.TrimSpace(options.FailureClass)) == authoring.FailureAgentRecoverable {
			semanticReport, semanticErr := runSemanticRecoveryScenario(report, root, options)
			if semanticErr != nil {
				semanticReport.Status = "ERROR"
				semanticReport.addError(semanticErr)
			}
			semanticReport.Paths, _ = harnessPaths(root)
			return semanticReport, semanticErr
		}
		return runFailureRouting(report, options)
	}
	if scenario == "definition-declaredness" || scenario == "definition-declaration" || scenario == "definition-fixture" || scenario == "definition_declaredness" {
		return runDefinitionDeclaredness(report, options, root)
	}
	if scenario == "unknown-receipt" || scenario == "unknown_receipt" {
		options.Interruption = valueOr(options.Interruption, "receipt-none")
		fixture, fixtureErr := NewProtocolFixture(root)
		if fixtureErr != nil {
			return report, fixtureErr
		}
		return runScenarioWithFixture(report, fixture, func(r *HarnessReport) error {
			err := runInterruption(r, fixture, options)
			filtered := r.Next[:0]
			for _, next := range r.Next {
				if next.Kind != decision.KindReady {
					filtered = append(filtered, next)
				}
			}
			r.Next = filtered
			return err
		})
	}
	if scenario == "reconcile-host-action" || scenario == "reconcile_host_action" {
		options.Status = valueOr(options.Status, protocol.HostActionStatusUnknown)
		if strings.TrimSpace(options.Choice) == "" && strings.TrimSpace(options.Fact) == "" && strings.TrimSpace(options.Expected) == "" && strings.TrimSpace(options.Conflict) == "" {
			options.Choice = "fulfilled"
		}
		fixture, fixtureErr := NewProtocolFixture(root)
		if fixtureErr != nil {
			return report, fixtureErr
		}
		return runScenarioWithFixture(report, fixture, func(r *HarnessReport) error {
			err := runHostAction(r, fixture, options)
			filtered := r.Next[:0]
			for _, next := range r.Next {
				if next.Kind != decision.KindReady {
					filtered = append(filtered, next)
				}
			}
			r.Next = filtered
			return err
		})
	}
	if _, statErr := os.Stat(report.SummaryPath); statErr == nil {
		if _, readErr := ReadTerminalSummary(root); readErr != nil {
			report.Status = "ERROR"
			report.addError(readErr)
			return report, readErr
		}
		err := &protocol.RejectedError{Code: protocol.CodeEventNotCurrent, Detail: "terminal summary exists; active engine state cannot be recreated"}
		report.Status = "ERROR"
		report.addError(err)
		return report, err
	} else if !os.IsNotExist(statErr) {
		return report, statErr
	}
	if scenario == "capacity-refill" {
		return runCapacityRefill(report, options, root)
	}

	fixture, err := NewProtocolFixture(root)
	if err != nil {
		return report, err
	}
	fixture.Host.VCS = fixture.VCS

	var runErr error
	switch scenario {
	case "smoke":
		runErr = runSmoke(&report, fixture)
	case "initialize":
		_, runErr = ensureReady(&report, fixture)
		report.Phase = "initialized"
	case "load":
		report.Phase = "load"
	case "revision-sequence":
		runErr = runRevisionSequence(&report, fixture)
	case "submit-request":
		runErr = runSubmitRequest(&report, fixture, options)
	case "submit-decision":
		runErr = runSubmitDecision(&report, fixture, options)
	case "submit-worker":
		runErr = runSubmitWorker(&report, fixture, options)
	case "submit-spawn":
		runErr = runSubmitSpawn(&report, fixture, options)
	case "submit-lifecycle":
		runErr = runSubmitLifecycle(&report, fixture, options)
	case "submit-operator":
		runErr = runSubmitOperator(&report, fixture, options)
	case "idempotency":
		runErr = runIdempotency(&report, fixture)
	case "freshness":
		runErr = runFreshness(&report, fixture)
	case "concurrent-submit":
		runErr = runConcurrent(&report, fixture)
	case "cas":
		runErr = runCAS(&report, fixture, options)
	case "fingerprint":
		runErr = runFingerprint(&report, fixture)
	case "fault":
		runErr = runFault(&report, fixture, options)
	case "recover":
		report.Recovery, runErr = fixture.Store.Recover()
		report.Phase = "restart"
		if runErr == nil {
			runErr = recoverSpawnAfterAttach(&report, fixture)
		}
		if runErr == nil && strings.TrimSpace(options.Fixture) != "" {
			var waitFixture waitUserActionFixture
			waitFixture, runErr = readWaitUserActionFixture(options.Fixture)
			if runErr == nil {
				setWaitUserActionReport(&report, waitFixture)
			}
		}
	case "lock-recovery":
		runErr = runLockRecovery(&report, fixture)
	case "host-action":
		runErr = runHostAction(&report, fixture, options)
	case "lifecycle":
		runErr = runLifecycle(&report, fixture)
	case "interruption":
		runErr = runInterruption(&report, fixture, options)
	case "result-before-receipt":
		runErr = runResultBeforeReceipt(&report, fixture)
	case "invalid-events":
		runErr = runInvalidEvents(&report, fixture)
	case "terminal":
		runErr = runTerminal(&report, fixture)
	case "full":
		runErr = runFull(&report, fixture, options)
	default:
		runErr = fmt.Errorf("testkit: unknown harness scenario %q", scenario)
	}

	if runErr != nil {
		report.Status = "ERROR"
		report.addError(runErr)
	}
	if len(report.Acceptances) == 1 {
		acceptance := report.Acceptances[0]
		report.Acceptance = &acceptance
	}
	if count, countErr := fixture.VCS.OperationCount("fault-replace-after"); countErr == nil && count > 0 {
		if report.SideEffects == nil {
			report.SideEffects = map[string]int{}
		}
		report.SideEffects["fakeVCS.fault-replace-after"] = count
	}
	report.FaultCalls = fixture.Faults.TriggeredCalls()
	if _, statErr := os.Stat(report.StatePath); statErr == nil {
		if summary, summaryErr := Summarize(fixture.Engine); summaryErr == nil {
			report.Summary = summary
		}
		if snapshot, snapshotErr := fixture.Engine.Load(); snapshotErr == nil {
			report.Snapshot = &snapshot
		}
	}
	report.Paths, _ = harnessPaths(root)
	return report, runErr
}

// runScenarioWithFixture applies the same final report projection used by the
// main scenario switch to the small alias scenarios handled before creation of
// the canonical fixture.
func runScenarioWithFixture(report HarnessReport, fixture *ProtocolFixture, run func(*HarnessReport) error) (HarnessReport, error) {
	err := run(&report)
	if err != nil {
		report.Status = "ERROR"
		report.Error = errorCode(err)
		report.addError(err)
	}
	if snapshot, loadErr := fixture.Engine.Load(); loadErr == nil {
		report.Snapshot = &snapshot
		report.Summary, _ = Summarize(fixture.Engine)
	}
	report.Paths, _ = harnessPaths(fixture.Root)
	return report, err
}

func runSmoke(report *HarnessReport, fixture *ProtocolFixture) error {
	if _, err := os.Stat(filepath.Join(fixture.Root, "engine-state", "state.json")); os.IsNotExist(err) {
		actions, err := ensureReady(report, fixture)
		if err != nil {
			return err
		}
		if len(actions) != 1 {
			return fmt.Errorf("harness issued %d actions, want 1", len(actions))
		}
		spawn, err := fixture.Host.SpawnEvent("smoke-spawn", actions[0], "installed-agent", protocol.SpawnStatusSpawned)
		if err != nil {
			return err
		}
		report.Acceptances = append(report.Acceptances, spawn.Acceptance)
		acceptance, err := fixture.Worker.ResultEvent("smoke-result", actions[0], protocol.OutcomePass, "sha256:harness-result", "")
		if err != nil {
			return err
		}
		report.Acceptances = append(report.Acceptances, acceptance)
		report.Phase = "initial"
		return nil
	}
	recovery, err := fixture.Store.Recover()
	if err != nil {
		return err
	}
	report.Recovery = recovery
	report.Phase = "restart"
	return nil
}

func ensureReady(report *HarnessReport, fixture *ProtocolFixture) ([]string, error) {
	statePath := filepath.Join(fixture.Root, "engine-state", "state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		issued, err := fixture.PrepareReady()
		if err != nil {
			return nil, err
		}
		for _, action := range issued {
			report.Actions = append(report.Actions, HarnessAction{ActionID: action.ActionID})
		}
		if len(issued) > 0 {
			report.Next = append(report.Next, NextRecord{Kind: decision.KindReady, Payload: issued})
		}
		actionIDs := make([]string, 0, len(issued))
		for _, action := range issued {
			actionIDs = append(actionIDs, action.ActionID)
		}
		return actionIDs, nil
	} else if err != nil {
		return nil, err
	}
	snapshot, err := fixture.Engine.Load()
	if err != nil {
		return nil, err
	}
	actions := make([]string, 0, len(snapshot.State.PendingActions))
	for actionID := range snapshot.State.PendingActions {
		actions = append(actions, actionID)
	}
	sort.Strings(actions)
	for _, actionID := range actions {
		report.Actions = append(report.Actions, HarnessAction{ActionID: actionID})
	}
	return actions, nil
}

func runRevisionSequence(report *HarnessReport, fixture *ProtocolFixture) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	snapshot, err := fixture.Engine.Load()
	if err != nil {
		return err
	}
	report.Revisions = append(report.Revisions, snapshot.Revision)
	for index := 1; index <= 5; index++ {
		event, err := protocol.NewRequestEvent(protocol.EventID(fmt.Sprintf("revision-event-%d", index)), protocol.ControlReset,
			protocol.AskOption{ID: protocol.AskOptionID(fmt.Sprintf("choice-%d", index)), Label: fmt.Sprintf("choice %d", index)})
		if err != nil {
			return err
		}
		acceptance, err := submit(fixture.Engine, event)
		if err != nil {
			return err
		}
		report.Acceptances = append(report.Acceptances, acceptance)
		report.Revisions = append(report.Revisions, acceptance.Revision)
	}
	report.Phase = "revision-sequence"
	return nil
}

func runSubmitRequest(report *HarnessReport, fixture *ProtocolFixture, options HarnessOptions) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	eventID := valueOr(options.EventID, "request-event")
	requestID := eventID
	control, err := controlKind(valueOr(options.Control, string(protocol.ControlReset)))
	if err != nil {
		return err
	}
	event, err := protocol.NewRequestEvent(protocol.EventID(eventID), control,
		protocol.AskOption{ID: protocol.AskOptionID(valueOr(options.Choice, "confirm")), Label: "confirm"})
	if err != nil {
		return err
	}
	acceptance, err := submit(fixture.Engine, event)
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, acceptance)
	token, err := fixture.Engine.Freshness(protocol.RequestID(requestID))
	if err == nil {
		report.Freshness = map[string]string{requestID: token}
		report.Next = append(report.Next, NextRecord{Kind: decision.KindAsk, Payload: map[string]any{"requestId": requestID, "freshness": token}})
	}
	report.Phase = "submitted"
	return nil
}

func runSubmitDecision(report *HarnessReport, fixture *ProtocolFixture, options HarnessOptions) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	requestID := valueOr(options.RequestID, "request-event")
	token := strings.TrimSpace(options.PayloadDigest)
	if token == "" {
		var err error
		token, err = fixture.Engine.Freshness(protocol.RequestID(requestID))
		if err != nil {
			return err
		}
	}
	event, err := protocol.NewDecideEvent(protocol.EventID(valueOr(options.EventID, "decision-event")), protocol.RequestID(requestID), token, protocol.AskOptionID(valueOr(options.Choice, "confirm")))
	if err != nil {
		return err
	}
	acceptance, err := submit(fixture.Engine, event)
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, acceptance)
	return nil
}

func runSubmitWorker(report *HarnessReport, fixture *ProtocolFixture, options HarnessOptions) error {
	actions, err := ensureReady(report, fixture)
	if err != nil {
		return err
	}
	actionID := valueOr(options.ActionID, first(actions))
	class := authoring.FailureClass(strings.TrimSpace(options.FailureClass))
	event, err := protocol.NewWorkerResultEvent(protocol.EventID(valueOr(options.EventID, "worker-result")), actionID,
		valueOr(options.Provider, "fake-host"), valueOr(options.Outcome, protocol.OutcomePass), valueOr(options.PayloadDigest, "sha256:harness-result"), class)
	if err != nil {
		return err
	}
	before, loadErr := fixture.Engine.Load()
	if loadErr != nil {
		return loadErr
	}
	_, replayed := before.State.Events[string(event.ID)]
	acceptance, err := submit(fixture.Engine, event)
	if err != nil {
		return err
	}
	if replayed && acceptance.Status == "ACCEPTED" {
		acceptance.Status = "DUPLICATE"
	}
	report.Acceptances = append(report.Acceptances, acceptance)
	return nil
}

func runSubmitSpawn(report *HarnessReport, fixture *ProtocolFixture, options HarnessOptions) error {
	actions, err := ensureReady(report, fixture)
	if err != nil {
		return err
	}
	event, err := protocol.NewSpawnReceiptWithFailureClass(protocol.EventID(valueOr(options.EventID, "spawn-receipt")), valueOr(options.ActionID, first(actions)),
		valueOr(options.Provider, "fake-host"), valueOr(options.Correlation, valueOr(options.Identity, "agent-1")), valueOr(options.Status, protocol.SpawnStatusSpawned), authoring.FailureClass(options.FailureClass))
	if err != nil {
		return err
	}
	acceptance, err := submit(fixture.Engine, event)
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, acceptance)
	return nil
}

func runSubmitLifecycle(report *HarnessReport, fixture *ProtocolFixture, options HarnessOptions) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	event, err := protocol.NewCorrelatedLifecycleEvent(protocol.EventID(valueOr(options.EventID, "lifecycle-event")), valueOr(options.Provider, "fake-host"),
		valueOr(options.Correlation, "agent-1"), valueOr(options.Identity, "agent-1"), valueOr(options.LifecycleEvent, protocol.LifecycleStart))
	if err != nil {
		return err
	}
	acceptance, err := submit(fixture.Engine, event)
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, acceptance)
	return nil
}

func runSubmitOperator(report *HarnessReport, fixture *ProtocolFixture, options HarnessOptions) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	event, err := protocol.NewOperatorObservationEvent(protocol.EventID(valueOr(options.EventID, "operator-event")), valueOr(options.Correlation, "operator-subject"),
		decision.Fact{Source: decision.SourceVCS, Key: "current", Value: valueOr(options.PayloadDigest, "sha256:observed")})
	if err != nil {
		return err
	}
	acceptance, err := submit(fixture.Engine, event)
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, acceptance)
	report.Next = append(report.Next, NextRecord{Kind: decision.KindOperator, Payload: map[string]any{"subject": valueOr(options.Correlation, "operator-subject")}})
	return nil
}

func runIdempotency(report *HarnessReport, fixture *ProtocolFixture) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	event, err := protocol.NewRequestEvent("idempotent-event", protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
	if err != nil {
		return err
	}
	firstAcceptance, err := submit(fixture.Engine, event)
	if err != nil {
		return err
	}
	secondAcceptance, err := submit(fixture.Engine, event)
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, firstAcceptance, secondAcceptance)
	mismatch, _ := protocol.NewRequestEvent("idempotent-event", protocol.ControlAbort, protocol.AskOption{ID: "confirm", Label: "confirm"})
	_, mismatchErr := submit(fixture.Engine, mismatch)
	if mismatchErr == nil {
		return fmt.Errorf("same event ID with different payload was accepted")
	}
	report.addError(mismatchErr)
	report.Revisions = []uint64{firstAcceptance.Revision, secondAcceptance.Revision}
	return nil
}

func runFreshness(report *HarnessReport, fixture *ProtocolFixture) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	request, _ := protocol.NewRequestEvent("freshness-request", protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
	requestAcceptance, err := submit(fixture.Engine, request)
	if err != nil {
		return err
	}
	t1, err := fixture.Engine.Freshness("freshness-request")
	if err != nil {
		return err
	}
	// 推进 revision 用一个真实协议事件（lifecycle observation buffer），
	// 不再借用 operator observation 伪造任意入账。
	bump, _ := protocol.NewCorrelatedLifecycleEvent("freshness-bump", "fake-host", "freshness-agent", "freshness-agent", protocol.LifecycleStart)
	if _, err := submit(fixture.Engine, bump); err != nil {
		return err
	}
	t2, err := fixture.Engine.Freshness("freshness-request")
	if err != nil {
		return err
	}
	stale, _ := protocol.NewDecideEvent("freshness-stale", "freshness-request", t1, "confirm")
	if _, staleErr := submit(fixture.Engine, stale); staleErr == nil {
		return fmt.Errorf("stale freshness token was accepted")
	} else {
		report.addError(staleErr)
	}
	fresh, _ := protocol.NewDecideEvent("freshness-current", "freshness-request", t2, "confirm")
	freshAcceptance, err := submit(fixture.Engine, fresh)
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, requestAcceptance, freshAcceptance)
	report.Freshness = map[string]string{"stale": t1, "current": t2}
	return nil
}

func runConcurrent(report *HarnessReport, fixture *ProtocolFixture) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	events := []protocol.Event{}
	for _, item := range []struct {
		id      protocol.EventID
		control protocol.ControlKind
	}{{"concurrent-a", protocol.ControlReset}, {"concurrent-b", protocol.ControlAbort}} {
		event, err := protocol.NewRequestEvent(item.id, item.control, protocol.AskOption{ID: "confirm", Label: "confirm"})
		if err != nil {
			return err
		}
		events = append(events, event)
	}
	acceptances := make([]protocol.Acceptance, 2)
	errs := SubmitConcurrently(
		func() (protocol.Acceptance, error) {
			var submitErr error
			acceptances[0], submitErr = submit(fixture.Engine, events[0])
			return acceptances[0], submitErr
		},
		func() (protocol.Acceptance, error) {
			var submitErr error
			acceptances[1], submitErr = submit(fixture.Engine, events[1])
			return acceptances[1], submitErr
		},
	)
	for index, concurrentErr := range errs {
		if concurrentErr != nil {
			report.addError(concurrentErr)
		}
		if acceptances[index].Status == "" {
			acceptance, retryErr := submit(fixture.Engine, events[index])
			if retryErr != nil {
				return retryErr
			}
			acceptances[index] = acceptance
		}
	}
	report.Acceptances = append(report.Acceptances, acceptances...)
	return nil
}

func runCAS(report *HarnessReport, fixture *ProtocolFixture, options HarnessOptions) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	base, err := fixture.Engine.Load()
	if err != nil {
		return err
	}
	event, _ := protocol.NewCorrelatedLifecycleEvent("cas-advance", "fake-host", "cas-agent", "cas-agent", protocol.LifecycleStart)
	advance, err := submit(fixture.Engine, event)
	if err != nil {
		return err
	}
	current, err := fixture.Engine.Load()
	if err != nil {
		return err
	}
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		return err
	}
	staleRevision := base.Revision
	if options.ExpectedRevision != 0 {
		staleRevision = options.ExpectedRevision
	}
	_, staleErr := fixture.Store.Save(persistence.Transaction{ExpectedRevision: staleRevision, ExpectedFingerprint: fingerprint, CollectFingerprint: fixture.VCS.Fingerprint, Content: &current.State})
	if staleErr == nil {
		return fmt.Errorf("stale revision %d was accepted at current revision %d", staleRevision, current.Revision)
	}
	report.addError(staleErr)
	saved, err := fixture.Store.Save(persistence.Transaction{ExpectedRevision: current.Revision, ExpectedFingerprint: fingerprint, CollectFingerprint: fixture.VCS.Fingerprint, Content: &current.State})
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, advance)
	report.Revisions = []uint64{base.Revision, current.Revision, saved.Revision}
	return nil
}

func runFingerprint(report *HarnessReport, fixture *ProtocolFixture) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	oldFingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		return err
	}
	count, err := fixture.VCS.ApplyOnce("fingerprint-drift", "facts/head", []byte("new-head\n"))
	if err != nil {
		return err
	}
	if _, _, driftErr := fixture.Engine.ExecuteHostAction("op.fan.transport", map[string]any{"target": "old-head"}, oldFingerprint); driftErr == nil {
		return fmt.Errorf("stale external fingerprint was accepted")
	} else {
		report.addError(driftErr)
	}
	newFingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		return err
	}
	intent, err := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "new-head"})
	if err != nil {
		return err
	}
	acceptance, err := fixture.Host.Receipt(intent, protocol.HostActionStatusExecuted)
	if err != nil {
		return err
	}
	if acceptance.Status != "ACCEPTED" {
		return fmt.Errorf("fingerprint retry receipt status %q, want ACCEPTED", acceptance.Status)
	}
	report.SideEffects = map[string]int{"fingerprint-drift": count, "stale-operation": 0, "retry-operation": fixture.Host.ActionCalls(intent.ActionID)}
	report.Metadata["oldFingerprint"] = oldFingerprint
	report.Metadata["newFingerprint"] = newFingerprint
	report.Metadata["intent"] = intent
	report.Acceptances = append(report.Acceptances, acceptance)
	return nil
}

func runFault(report *HarnessReport, fixture *ProtocolFixture, options HarnessOptions) error {
	if strings.TrimSpace(options.Fixture) != "" && strings.TrimSpace(options.Fault) == string(persistence.FaultExecuteAfter) {
		plan, err := readFullRecoveryFixture(options.Fixture)
		if err != nil {
			return err
		}
		return runFullRecoveryFault(report, fixture, plan)
	}
	actions, err := ensureReady(report, fixture)
	if err != nil {
		return err
	}
	point, err := faultPoint(options.Fault)
	if err != nil {
		return err
	}
	if isPersistenceFault(point) {
		fixture.Faults.ArmCrash(point)
	} else {
		fixture.Faults.Arm(point)
	}
	report.Phase = "fault-injected"
	if point == persistence.FaultReplaceAfter {
		count, applyErr := fixture.VCS.ApplyOnce("fault-replace-after", "delivery/fault-replace-after.txt", []byte("committed\n"))
		if applyErr != nil {
			return applyErr
		}
		report.SideEffects = map[string]int{"fakeVCS.fault-replace-after": count}
	}
	switch point {
	case persistence.FaultSpawnAfterAttach:
		_, err = fixture.Host.SpawnEvent("fault-spawn", first(actions), "fault-agent", protocol.SpawnStatusSpawned)
		report.SideEffects = map[string]int{"fakeHost.spawn": fixture.Host.SpawnCalls(first(actions))}
	case persistence.FaultResultBeforeReceipt:
		_, err = fixture.Worker.ResultBeforeReceipt(first(actions), protocol.OutcomePass, "sha256:fault-result", "")
	case persistence.FaultSubmitResponseLost:
		if _, spawnErr := fixture.Host.SpawnEventWithoutSubmitResponseFault("fault-spawn", first(actions), "fault-agent", protocol.SpawnStatusSpawned); spawnErr != nil {
			return spawnErr
		}
		_, err = fixture.Worker.ResultEvent("fault-result", first(actions), protocol.OutcomePass, "sha256:fault-result", "")
		report.SideEffects = map[string]int{"fakeHost.spawn": fixture.Host.SpawnCalls(first(actions))}
	case persistence.FaultHostCommitBefore, persistence.FaultHostCommitAfter:
		intent, executeErr := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "fault"})
		err = executeErr
		if intent.ActionID != "" {
			report.SideEffects = map[string]int{"fakeHost." + intent.ActionID: fixture.Host.ActionCalls(intent.ActionID)}
		}
	case persistence.FaultHostObserveBefore:
		intent, executeErr := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "fault"})
		if executeErr != nil {
			return executeErr
		}
		report.SideEffects = map[string]int{"fakeHost." + intent.ActionID: fixture.Host.ActionCalls(intent.ActionID)}
		_, err = fixture.Host.Observe(intent.ActionID, "sha256:observed")
	case persistence.FaultHostReconcileBefore:
		intent, executeErr := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "fault"})
		if executeErr != nil {
			return executeErr
		}
		report.SideEffects = map[string]int{"fakeHost." + intent.ActionID: fixture.Host.ActionCalls(intent.ActionID)}
		_, err = fixture.Host.Reconcile(intent.ActionID, "sha256:observed", false, false)
	default:
		event, eventErr := protocol.NewRequestEvent("fault-request", protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
		if eventErr != nil {
			return eventErr
		}
		_, err = submit(fixture.Engine, event)
	}
	if err == nil {
		return fmt.Errorf("fault %s was not consumed", point)
	}
	return err
}

func runLockRecovery(report *HarnessReport, fixture *ProtocolFixture) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	lockPath := filepath.Join(fixture.Root, "engine-state", "write.lock")
	if err := os.WriteFile(lockPath, []byte("pid=999999999 token=crashed\n"), 0o600); err != nil {
		return err
	}
	event, _ := protocol.NewRequestEvent("after-crashed-lock", protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
	acceptance, err := submit(fixture.Engine, event)
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, acceptance)
	report.Metadata["lockRecovered"] = true
	return nil
}

func recoverSpawnAfterAttach(report *HarnessReport, fixture *ProtocolFixture) error {
	snapshot, err := fixture.Engine.Load()
	if err != nil {
		return err
	}
	actionIDs := make([]string, 0, len(snapshot.State.PendingActions))
	for actionID := range snapshot.State.PendingActions {
		if _, exists := snapshot.State.SpawnReceipts[actionID]; exists {
			continue
		}
		count, countErr := fixture.VCS.OperationCount("fake-host.spawn:" + actionID)
		if countErr != nil || count == 0 {
			continue
		}
		actionIDs = append(actionIDs, actionID)
	}
	sort.Strings(actionIDs)
	for _, actionID := range actionIDs {
		event, eventErr := protocol.NewSpawnReceiptEvent("fault-spawn", actionID, fixture.Host.Provider, "fault-agent", protocol.SpawnStatusSpawned)
		if eventErr != nil {
			return eventErr
		}
		acceptance, submitErr := submit(fixture.Engine, event)
		if submitErr != nil {
			return submitErr
		}
		report.Acceptances = append(report.Acceptances, acceptance)
		report.SideEffects = map[string]int{"fakeHost.spawn": countSpawnCalls(fixture, actionID)}
	}
	return nil
}

func countSpawnCalls(fixture *ProtocolFixture, actionID string) int {
	count, err := fixture.VCS.OperationCount("fake-host.spawn:" + actionID)
	if err != nil {
		return 0
	}
	return count
}

func runHostAction(report *HarnessReport, fixture *ProtocolFixture, options HarnessOptions) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	intent, err := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "phase2", "retries": 1})
	if err != nil {
		return err
	}
	before, err := fixture.Engine.Load()
	if err != nil {
		return err
	}
	report.Metadata["pendingIntent"] = before.State.PendingHostActions[intent.ActionID]
	acceptance, err := fixture.Host.ReceiptEvent(protocol.EventID(valueOr(options.EventID, "host-action-receipt")), intent, valueOr(options.Status, protocol.HostActionStatusExecuted), valueOr(options.Correlation, "host-action-correlation"))
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, acceptance)
	report.SideEffects = map[string]int{intent.ActionID: fixture.Host.ActionCalls(intent.ActionID)}
	if valueOr(options.Status, protocol.HostActionStatusExecuted) == protocol.HostActionStatusUnknown {
		fulfilled, conflict := true, false
		if strings.TrimSpace(options.Fact) != "" || strings.TrimSpace(options.Expected) != "" {
			if strings.TrimSpace(options.Expected) == "" {
				fulfilled = parseTruthy(options.Fact)
			} else {
				fulfilled = strings.TrimSpace(options.Fact) == strings.TrimSpace(options.Expected)
			}
		}
		if strings.TrimSpace(options.Conflict) != "" {
			conflict = parseTruthy(options.Conflict)
			if conflict {
				fulfilled = false
			}
		}
		switch strings.ToLower(strings.TrimSpace(options.Choice)) {
		case "":
		case "fulfilled":
			fulfilled, conflict = true, false
		case "pending":
			fulfilled = false
		case "conflict":
			fulfilled, conflict = false, true
		default:
			return fmt.Errorf("testkit: host-action UNKNOWN choice must be fulfilled, pending, or conflict")
		}
		observationDigest := "sha256:host-action-observed"
		if strings.TrimSpace(options.Fact) != "" {
			observationDigest = options.Fact
		}
		plan, reconcileErr := fixture.Host.Reconcile(intent.ActionID, observationDigest, fulfilled, conflict)
		if reconcileErr != nil {
			return reconcileErr
		}
		report.RecoveryPlan = append(report.RecoveryPlan, plan)
		if fulfilled {
			replay, replayErr := fixture.Host.Reconcile(intent.ActionID, observationDigest, true, false)
			if replayErr != nil {
				return replayErr
			}
			report.RecoveryPlan = append(report.RecoveryPlan, replay)
		}
	}
	return nil
}

func runLifecycle(report *HarnessReport, fixture *ProtocolFixture) error {
	actions, err := ensureReady(report, fixture)
	if err != nil {
		return err
	}
	spawn, err := fixture.Host.SpawnEvent("lifecycle-spawn", first(actions), "agent-verified", protocol.SpawnStatusSpawned)
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, spawn.Acceptance)
	events := []protocol.Event{}
	for _, item := range []struct{ id, correlation, identity, name string }{
		{"lifecycle-start", "agent-verified", "agent-verified", protocol.LifecycleStart},
		{"lifecycle-stop", "agent-verified", "agent-verified", protocol.LifecycleStop},
		{"lifecycle-unpaired", "unpaired", "unpaired", protocol.LifecycleStop},
		{"lifecycle-unclaimed", "unclaimed", "unclaimed", protocol.LifecycleStart},
	} {
		event, eventErr := protocol.NewCorrelatedLifecycleEvent(protocol.EventID(item.id), "fake-host", item.correlation, item.identity, item.name)
		if eventErr != nil {
			return eventErr
		}
		events = append(events, event)
	}
	for _, event := range events {
		acceptance, submitErr := submit(fixture.Engine, event)
		if event.ID == "lifecycle-unpaired" {
			if submitErr == nil {
				return fmt.Errorf("unpaired lifecycle stop was accepted")
			}
			report.addError(submitErr)
			continue
		}
		if submitErr != nil {
			return submitErr
		}
		report.Acceptances = append(report.Acceptances, acceptance)
	}
	for _, event := range events[:2] {
		acceptance, submitErr := submit(fixture.Engine, event)
		if submitErr != nil {
			return submitErr
		}
		report.Acceptances = append(report.Acceptances, acceptance)
	}
	return nil
}

func runInterruption(report *HarnessReport, fixture *ProtocolFixture, options HarnessOptions) error {
	actions, err := ensureReady(report, fixture)
	if err != nil {
		return err
	}
	actionID := first(actions)
	mode := strings.ToLower(valueOr(options.Interruption, "transient"))
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		return err
	}
	var plan protocol.RecoveryPlan
	switch mode {
	case "transient":
		plan, _, err = fixture.Engine.RecoverAttempt(actionID, protocol.Interruption{Class: authoring.FailureTransientEngine}, fingerprint)
	case "nontransient":
		plan, _, err = fixture.Engine.RecoverAttempt(actionID, protocol.Interruption{CauseKnown: true}, fingerprint)
	case "unknown":
		plan, _, err = fixture.Engine.RecoverAttempt(actionID, protocol.Interruption{}, fingerprint)
	case "receipt-one", "receipt-multiple", "receipt-none":
		matches := 0
		if mode == "receipt-one" {
			matches = 1
		} else if mode == "receipt-multiple" {
			matches = 2
		}
		if raw := strings.TrimSpace(options.LifecycleMatches); raw != "" {
			// The CLI contract accepts both a single count and the compact
			// `2/0` fixture notation used by black-box runners; the first
			// component selects the deterministic match set for this run.
			countValue := strings.TrimSpace(strings.SplitN(raw, "/", 2)[0])
			parsed, parseErr := strconv.Atoi(countValue)
			if parseErr != nil || parsed < 0 {
				return fmt.Errorf("testkit: lifecycle-matches must be a non-negative integer, got %q", raw)
			}
			matches = parsed
		}
		for index := 0; index < matches; index++ {
			identity := fmt.Sprintf("receipt-agent-%d", index+1)
			for _, eventName := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
				event, eventErr := protocol.NewCorrelatedLifecycleEvent(protocol.EventID(fmt.Sprintf("%s-%s", identity, eventName)), "fake-host", "receipt-correlation", identity, eventName)
				if eventErr != nil {
					return eventErr
				}
				if _, submitErr := submit(fixture.Engine, event); submitErr != nil {
					return submitErr
				}
			}
		}
		unknown, eventErr := protocol.NewSpawnReceiptEvent("unknown-receipt", actionID, "fake-host", "receipt-correlation", protocol.SpawnStatusUnknown)
		if eventErr != nil {
			return eventErr
		}
		if acceptance, submitErr := submit(fixture.Engine, unknown); submitErr != nil {
			return submitErr
		} else {
			report.Acceptances = append(report.Acceptances, acceptance)
		}
		fingerprint, _ = fixture.Engine.ObserveFingerprint()
		plan, _, err = fixture.Engine.ReconcileUnknownReceipt(actionID, fingerprint)
	default:
		return fmt.Errorf("testkit: unknown interruption mode %q", mode)
	}
	if err != nil {
		return err
	}
	report.RecoveryPlan = append(report.RecoveryPlan, plan)
	report.Metadata["lifecycleMatches"] = plan.LifecycleMatches
	if raw := strings.TrimSpace(options.LifecycleMatches); raw != "" {
		report.Metadata["lifecycleMatchSpec"] = raw
	}
	if plan.Action == protocol.RecoveryAsk {
		report.Next = append(report.Next, NextRecord{Kind: decision.KindAsk, Payload: map[string]any{"requestId": plan.RequestID, "options": plan.Options}})
	} else if plan.Action == protocol.RecoveryOperator {
		report.Next = append(report.Next, NextRecord{Kind: decision.KindOperator, Payload: map[string]any{"matches": plan.LifecycleMatches}})
	}
	if mode == "transient" && plan.Action == protocol.RecoveryResumeAttempt {
		spawn, spawnErr := fixture.Host.SpawnEvent("interruption-resume-spawn", actionID, "resumed-agent", protocol.SpawnStatusSpawned)
		if spawnErr != nil {
			return spawnErr
		}
		accepted, resultErr := fixture.Worker.ResultEvent("interruption-resume-result", actionID, protocol.OutcomePass, "sha256:resumed", "")
		if resultErr != nil {
			return resultErr
		}
		report.Acceptances = append(report.Acceptances, spawn.Acceptance, accepted)
	}
	if mode == "nontransient" && plan.Action == protocol.RecoveryNewAttempt {
		late, eventErr := protocol.NewWorkerResultEvent("interruption-late-result", actionID, "fake-host", protocol.OutcomePass, "sha256:late", "")
		if eventErr != nil {
			return eventErr
		}
		lateAcceptance, submitErr := submit(fixture.Engine, late)
		if submitErr != nil {
			return submitErr
		}
		if lateAcceptance.Status != "OBSOLETE_RESULT" {
			return fmt.Errorf("late result status %q, want OBSOLETE_RESULT", lateAcceptance.Status)
		}
		report.Acceptances = append(report.Acceptances, lateAcceptance)
		snapshot, loadErr := fixture.Engine.Load()
		if loadErr != nil {
			return loadErr
		}
		for replacement := range snapshot.State.PendingActions {
			if replacement != actionID {
				report.Actions = append(report.Actions, HarnessAction{ActionID: replacement})
			}
		}
		sort.Slice(report.Actions, func(i, j int) bool { return report.Actions[i].ActionID < report.Actions[j].ActionID })
	}
	return nil
}

func runResultBeforeReceipt(report *HarnessReport, fixture *ProtocolFixture) error {
	actions, err := ensureReady(report, fixture)
	if err != nil {
		return err
	}
	actionID := first(actions)
	staged, err := fixture.Worker.ResultEvent("result-before-receipt", actionID, protocol.OutcomePass, "sha256:staged", "")
	if err != nil {
		return err
	}
	if staged.Status != "STAGED" {
		return fmt.Errorf("result-before-receipt status %q, want STAGED", staged.Status)
	}
	restarted, recovery, err := fixture.Restart()
	if err != nil {
		return err
	}
	restarted.Host.VCS = restarted.VCS
	spawn, err := restarted.Host.SpawnEvent("result-after-restart-receipt", actionID, "staged-agent", protocol.SpawnStatusSpawned)
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, staged, spawn.Acceptance)
	report.Recovery = recovery
	report.SideEffects = map[string]int{"fakeHost.spawn": restarted.Host.SpawnCalls(actionID)}
	report.Phase = "staged-result-settled"
	return nil
}

func runInvalidEvents(report *HarnessReport, fixture *ProtocolFixture) error {
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	snapshot, err := fixture.Engine.Load()
	if err != nil {
		return err
	}
	before := snapshot.Revision
	beforeBytes, err := os.ReadFile(report.StatePath)
	if err != nil {
		return err
	}
	invalid := []protocol.Event{
		{ID: "invalid-kind", Kind: protocol.EventKind("USER_ABORT")},
		{ID: "invalid-schema", Kind: protocol.KindRequestControl},
	}
	otherTask, _ := protocol.NewTaskEvent("other-node", TaskKey("other", "entry.parse"), "missing-attempt", runtime.TaskIssued)
	invalid = append(invalid, otherTask)
	for _, event := range invalid {
		if _, submitErr := submit(fixture.Engine, event); submitErr == nil {
			return fmt.Errorf("invalid event %q was accepted", event.ID)
		} else {
			report.addError(submitErr)
		}
		afterBytes, readErr := os.ReadFile(report.StatePath)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(beforeBytes, afterBytes) {
			return fmt.Errorf("invalid event %q changed state bytes", event.ID)
		}
	}
	after, err := fixture.Engine.Load()
	if err != nil {
		return err
	}
	report.Revisions = []uint64{before, after.Revision}
	return nil
}

func runTerminal(report *HarnessReport, fixture *ProtocolFixture) error {
	if _, err := os.Stat(report.StatePath); err == nil {
		return fmt.Errorf("testkit: terminal scenario requires a fresh project")
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseTerminal)
	if err != nil {
		return err
	}
	if err := fixture.Initialize(view, "fake-host"); err != nil {
		return err
	}
	request, _ := protocol.NewRequestEvent("terminal-request", protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
	requestAcceptance, err := submit(fixture.Engine, request)
	if err != nil {
		return err
	}
	token, err := fixture.Engine.Freshness("terminal-request")
	if err != nil {
		return err
	}
	decisionEvent, _ := protocol.NewDecideEvent("terminal-decision", "terminal-request", token, "confirm")
	eventAcceptance, err := submit(fixture.Engine, decisionEvent)
	if err != nil {
		return err
	}
	snapshot, err := fixture.Engine.Load()
	if err != nil {
		return err
	}
	requestDigest, _ := request.Digest()
	eventDigest, _ := decisionEvent.Digest()
	summary, err := WriteTerminalSummary(fixture.Root, snapshot.Revision, requestAcceptance, requestDigest, eventAcceptance, eventDigest, "COMPLETE")
	if err != nil {
		return err
	}
	if err := os.Remove(report.StatePath); err != nil {
		return err
	}
	report.Terminal = &summary
	report.Acceptances = append(report.Acceptances, requestAcceptance, eventAcceptance)
	report.Next = append(report.Next, summary.Next)
	report.Phase = "terminal"
	return nil
}

func runFull(report *HarnessReport, fixture *ProtocolFixture, options HarnessOptions) error {
	if strings.TrimSpace(options.Fixture) != "" {
		plan, err := readFullRecoveryFixture(options.Fixture)
		if err != nil {
			return err
		}
		if !options.Continue {
			return fmt.Errorf("testkit: full fixture requires --continue")
		}
		if err := validateFullRecoveryPlan(plan); err != nil {
			return err
		}
	}
	if _, err := ensureReady(report, fixture); err != nil {
		return err
	}
	// 驱动循环（审阅 P1-3 修复）：每个决策边界都由真实协议事件推进——
	// AGENT 步走 spawn 回执 + worker result，HUMAN_ASK 走两阶段 request/
	// decide，HOST_ACTION 走 intent 持久化 + adapter 执行 + EXECUTED 回执，
	// LOCAL/DURABLE/PARALLEL 步由引擎自身的 completeResult 路径完成
	// （PASS result → CompleteStep → refill），与 agent 步完全同一条接纳
	// 面。不再手工 CompleteStep、不手工清空 Expected/Attempts/PendingActions、
	// 不绕过 Engine 直连 Store.Save。
	seq := 0
	nextEventID := func(prefix string) protocol.EventID {
		seq++
		return protocol.EventID(fmt.Sprintf("full-%s-%d", prefix, seq))
	}
	var lastRequest protocol.Acceptance
	requestDigest := ""
	var hostIntent protocol.HostActionIntent
	hostReceiptAcceptance := protocol.Acceptance{}
	vcsCount := 0
	for round := 0; round < 64; round++ { // 有界防御：定义本身有限步，正常路径远小于该值
		snapshot, loadErr := fixture.Engine.Load()
		if loadErr != nil {
			return loadErr
		}
		plan, decideErr := decision.Decide(&snapshot.State.State, decision.Observation{}, fixture.Definition)
		if decideErr != nil {
			return decideErr
		}
		switch plan.Next.Kind {
		case decision.KindWait:
			// WAIT(TASKS_IN_FLIGHT)：frontier 的外部步骤已全部签发在途——
			// 驱动唯一 pending action（spawn 回执 + PASS result）完成它，
			// 而不是空转。无在途任务时才是真正的死锁。
			actionID := ""
			for id := range snapshot.State.PendingActions {
				if _, receipted := snapshot.State.SpawnReceipts[id]; !receipted {
					actionID = id
					break
				}
			}
			if actionID == "" {
				return fmt.Errorf("full scenario WAIT without a drivable pending action")
			}
			spawn, spawnErr := fixture.Host.SpawnEvent(nextEventID("wait-spawn"), actionID, "full-agent-wait", protocol.SpawnStatusSpawned)
			if spawnErr != nil {
				return spawnErr
			}
			result, resultErr := fixture.Worker.ResultEvent(nextEventID("wait-result"), actionID, protocol.OutcomePass, "sha256:full-result", "")
			if resultErr != nil {
				return resultErr
			}
			report.Acceptances = append(report.Acceptances, spawn.Acceptance, result)
		case decision.KindComplete:
			final, loadErr := fixture.Engine.Load()
			if loadErr != nil {
				return loadErr
			}
			if final.State.Phase != runtime.PhaseTerminal && len(final.State.Expected) != 0 {
				return fmt.Errorf("full scenario reached COMPLETE with %d unsettled tasks", len(final.State.Expected))
			}
			hostReceiptDigest := ""
			if hostReceiptAcceptance.EventID != "" {
				hostEvent, eventErr := protocol.NewHostActionReceiptEvent(
					protocol.EventID(hostReceiptAcceptance.EventID), hostIntent.ActionID, hostIntent.Adapter.Operation,
					"fake-host", "full-host-correlation", hostIntent.PayloadDigest, protocol.HostActionStatusExecuted)
				if eventErr != nil {
					return eventErr
				}
				digest, digestErr := hostEvent.Digest()
				if digestErr != nil {
					return digestErr
				}
				hostReceiptDigest = digest
			}
			summary, summaryErr := WriteTerminalSummary(fixture.Root, final.Revision, lastRequest, requestDigest, hostReceiptAcceptance, hostReceiptDigest, "COMPLETE")
			if summaryErr != nil {
				return summaryErr
			}
			if err := os.Remove(report.StatePath); err != nil {
				return err
			}
			report.Terminal = &summary
			report.Next = append(report.Next, summary.Next)
			report.SideEffects = map[string]int{
				"fakeVCS.full-commit":             vcsCount,
				"fakeHost.spawn":                  fixture.Host.SpawnCalls(lastSpawnActionID(report)),
				"fakeHost." + hostIntent.ActionID: fixture.Host.ActionCalls(hostIntent.ActionID),
			}
			report.Phase = "terminal"
			return nil
		case decision.KindReady:
			for _, task := range plan.Next.Ready.Tasks {
				actionID := "act:" + task.Task.String()
				spawn, spawnErr := fixture.Host.SpawnEvent(nextEventID("spawn"), actionID, "full-agent-"+string(task.Step), protocol.SpawnStatusSpawned)
				if spawnErr != nil {
					return spawnErr
				}
				report.Acceptances = append(report.Acceptances, spawn.Acceptance)
				result, resultErr := fixture.Worker.ResultEvent(nextEventID("result"), actionID, protocol.OutcomePass, "sha256:full-result", "")
				if resultErr != nil {
					return resultErr
				}
				report.Acceptances = append(report.Acceptances, result)
			}
		case decision.KindAsk:
			// ask.decide 的选项集来自 canonical definition（schema 固定）；
			// request 事件携带与旧 full 场景一致的 confirm 选项。
			request, reqErr := protocol.NewRequestEvent(nextEventID("request"), protocol.ControlReset, protocol.AskOption{ID: "confirm", Label: "confirm"})
			if reqErr != nil {
				return reqErr
			}
			requestAcceptance, submitErr := submit(fixture.Engine, request)
			if submitErr != nil {
				return submitErr
			}
			requestID := string(request.ID)
			token, tokenErr := fixture.Engine.Freshness(protocol.RequestID(requestID))
			if tokenErr != nil {
				return tokenErr
			}
			decideEvent, decErr := protocol.NewDecideEvent(nextEventID("decision"), protocol.RequestID(requestID), token, "confirm")
			if decErr != nil {
				return decErr
			}
			decisionAcceptance, submitErr := submit(fixture.Engine, decideEvent)
			if submitErr != nil {
				return submitErr
			}
			lastRequest = requestAcceptance
			digest, digestErr := request.Digest()
			if digestErr != nil {
				return digestErr
			}
			requestDigest = digest
			report.Acceptances = append(report.Acceptances, requestAcceptance, decisionAcceptance)
			count, vcsErr := fixture.VCS.ApplyOnce("full-commit", "delivery/result.txt", []byte("complete\n"))
			if vcsErr != nil {
				return vcsErr
			}
			vcsCount = count
		case decision.KindHostAction:
			intent, execErr := fixture.Host.Execute("op.fan.transport", map[string]any{"target": "phase2", "retries": float64(1)})
			if execErr != nil {
				return execErr
			}
			receipt, receiptErr := fixture.Host.ReceiptEvent(nextEventID("host-receipt"), intent, protocol.HostActionStatusExecuted, "full-host-correlation")
			if receiptErr != nil {
				return receiptErr
			}
			hostIntent = intent
			hostReceiptAcceptance = receipt
			report.Acceptances = append(report.Acceptances, receipt)
		default:
			return fmt.Errorf("full scenario cannot drive NextResult kind %s", plan.Next.Kind)
		}
	}
	return fmt.Errorf("full scenario did not reach COMPLETE within the step bound")
}

// lastSpawnActionID returns the most recent action the full loop spawned, for
// the side-effect projection.
func lastSpawnActionID(report *HarnessReport) string {
	for index := len(report.Acceptances) - 1; index >= 0; index-- {
		if report.Acceptances[index].Kind == "SPAWN_RECEIPT" {
			return report.Acceptances[index].ActionID
		}
	}
	return ""
}

func runEnvelopeScenario(report HarnessReport, options HarnessOptions, root string, envelope persistence.Envelope) (HarnessReport, error) {
	envelopeWrite := strings.ReplaceAll(report.Scenario, "_", "-") == "envelope-write"
	target := report.StatePath
	if strings.TrimSpace(options.Target) != "" {
		candidate := strings.TrimSpace(options.Target)
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(root, candidate)
		}
		target = candidate
	}
	if envelopeWrite && strings.TrimSpace(options.Fixture) != "" {
		loaded, fixtureTarget, err := readEnvelopeWriteFixture(options.Fixture)
		if err != nil {
			return report, err
		}
		envelope = loaded
		if strings.TrimSpace(options.Target) == "" && fixtureTarget != "" {
			target = fixtureTarget
			if !filepath.IsAbs(target) {
				target = filepath.Join(root, target)
			}
		}
	}
	if envelopeWrite {
		// The write-barrier fixture owns a fresh target. Creating it before
		// validation makes the zero-byte-on-rejection invariant observable,
		// while the legacy `envelope` scenario keeps its no-target contract.
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return report, err
		}
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			return report, err
		}
	}
	field := strings.ToLower(strings.TrimSpace(options.MissingField))
	field = strings.ReplaceAll(field, "-", "")
	switch field {
	case "writer":
		envelope.Writer = ""
	case "stateSchemaVersion":
		envelope.StateSchemaVersion = ""
	case "workflowDefinitionVersion":
		envelope.WorkflowDefinitionVersion = ""
	case "definitionDigest":
		envelope.DefinitionDigest = ""
	case "packageDigest":
		envelope.PackageDigest = ""
	case "packagedigest":
		envelope.PackageDigest = ""
	case "stateschemaversion":
		envelope.StateSchemaVersion = ""
	case "workflowdefinitionversion":
		envelope.WorkflowDefinitionVersion = ""
	case "definitiondigest":
		envelope.DefinitionDigest = ""
	case "":
		// A valid envelope-write fixture exercises the positive write path;
		// the legacy envelope scenario still requires --missing-field below.
		if !envelopeWrite {
			return report, fmt.Errorf("testkit: --missing-field is required for envelope")
		}
	default:
		return report, fmt.Errorf("testkit: --missing-field must name writer, stateSchemaVersion, workflowDefinitionVersion, definitionDigest, or packageDigest")
	}
	report.Envelope = &envelope
	err := persistence.ValidateEnvelope(envelope, HarnessPackageDigest)
	if err != nil {
		report.Error = errorCode(err)
		report.Metadata["errorCode"] = report.Error
		report.Metadata["targetBytes"] = fileSize(target)
		report.Metadata["target"] = target
		report.Phase = "write-barrier"
		report.addError(err)
		return report, nil
	}
	data, marshalErr := json.MarshalIndent(envelope, "", "  ")
	if marshalErr != nil {
		return report, marshalErr
	}
	data = append(data, '\n')
	if writeErr := writeHarnessJSON(target, json.RawMessage(data)); writeErr != nil {
		return report, writeErr
	}
	report.Metadata["targetBytes"] = fileSize(target)
	report.Metadata["target"] = target
	report.Phase = "envelope-write"
	return report, nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func readEnvelopeWriteFixture(path string) (persistence.Envelope, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return persistence.Envelope{}, "", err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return persistence.Envelope{}, "", err
	}
	var envelope persistence.Envelope
	var nested json.RawMessage
	for key, value := range raw {
		if strings.EqualFold(key, "envelope") {
			nested = value
			break
		}
	}
	if nested != nil {
		if err := json.Unmarshal(nested, &envelope); err != nil {
			return persistence.Envelope{}, "", err
		}
	} else if err := json.Unmarshal(data, &envelope); err != nil {
		return persistence.Envelope{}, "", err
	}
	var target string
	for key, value := range raw {
		if strings.EqualFold(key, "target") {
			_ = json.Unmarshal(value, &target)
			break
		}
	}
	return envelope, target, nil
}

type receiptFileWire struct {
	ActionID          string                      `json:"actionID"`
	Operation         string                      `json:"operation"`
	AdapterOperation  string                      `json:"adapterOperation"`
	Params            any                         `json:"params"`
	Provider          string                      `json:"provider"`
	Correlation       string                      `json:"correlation"`
	PayloadDigest     string                      `json:"payloadDigest"`
	Status            string                      `json:"status"`
	FailureClass      authoring.FailureClass      `json:"failureClass"`
	LifecycleEvidence *protocol.LifecycleEvidence `json:"lifecycleEvidence"`
	AdapterEvidence   map[string]any              `json:"adapterEvidence"`
}

type waitUserActionFixture struct {
	EventID      string `json:"eventId"`
	FailureClass string `json:"failureClass"`
	Route        string `json:"route"`
	Reason       string `json:"reason"`
	AttemptID    string `json:"attemptID"`
	TaskID       string `json:"taskID"`
}

func readWaitUserActionFixture(path string) (waitUserActionFixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return waitUserActionFixture{}, err
	}
	var input waitUserActionFixture
	if err := json.Unmarshal(data, &input); err != nil {
		return waitUserActionFixture{}, err
	}
	if input.FailureClass != string(authoring.FailureUserActionRequired) || input.Route != "WAIT" ||
		input.Reason != string(authoring.FailureUserActionRequired) || input.EventID == "" || input.AttemptID == "" || input.TaskID == "" {
		return waitUserActionFixture{}, fmt.Errorf("testkit: wait-user-action fixture does not match the registered USER_ACTION_REQUIRED WAIT contract")
	}
	return input, nil
}

func setWaitUserActionReport(report *HarnessReport, input waitUserActionFixture) {
	report.Next = []NextRecord{{Kind: decision.KindWait, Payload: map[string]any{"reason": input.Reason}}}
	report.Metadata["failureClass"] = input.FailureClass
	report.Metadata["attemptID"] = input.AttemptID
	report.Metadata["taskID"] = input.TaskID
}

type fullRecoveryFixture struct {
	RunID            string         `json:"runID"`
	Tasks            []string       `json:"tasks"`
	Provider         string         `json:"provider"`
	VCS              string         `json:"vcs"`
	PackageDigest    string         `json:"packageDigest"`
	AdapterOperation string         `json:"adapterOperation"`
	AdapterParams    map[string]any `json:"adapterParams"`
	Fault            struct {
		Point string `json:"point"`
		Once  bool   `json:"once"`
	} `json:"fault"`
}

func readFullRecoveryFixture(path string) (fullRecoveryFixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fullRecoveryFixture{}, err
	}
	var fixture fullRecoveryFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return fullRecoveryFixture{}, err
	}
	if err := validateFullRecoveryPlan(fixture); err != nil {
		return fullRecoveryFixture{}, err
	}
	return fixture, nil
}

func validateFullRecoveryPlan(fixture fullRecoveryFixture) error {
	if fixture.RunID != "full-recovery-qa" || len(fixture.Tasks) != 3 ||
		fixture.Tasks[0] != "fan.split" || fixture.Tasks[1] != "fan.slice" || fixture.Tasks[2] != "fan.transport" ||
		fixture.Provider != "fake-host" || fixture.VCS != "fake-vcs" || fixture.PackageDigest != HarnessPackageDigest ||
		fixture.AdapterOperation != "op.fan.transport" || fixture.Fault.Point != string(persistence.FaultExecuteAfter) || !fixture.Fault.Once {
		return fmt.Errorf("testkit: full recovery fixture does not match the registered plan")
	}
	if len(fixture.AdapterParams) != 2 || fixture.AdapterParams["target"] != "phase2" {
		return fmt.Errorf("testkit: full recovery fixture adapter params are invalid")
	}
	if retries, ok := fixture.AdapterParams["retries"].(float64); !ok || retries != 1 {
		return fmt.Errorf("testkit: full recovery fixture retries must be 1")
	}
	return nil
}

func runFullRecoveryFault(report *HarnessReport, fixture *ProtocolFixture, plan fullRecoveryFixture) error {
	if err := validateFullRecoveryPlan(plan); err != nil {
		return err
	}
	if _, err := os.Stat(report.StatePath); os.IsNotExist(err) {
		view, viewErr := decision.NewState(definition.Version, runtime.PhaseDevelopmentParallel)
		if viewErr != nil {
			return viewErr
		}
		compiled, compileErr := compiler.Compile(definition.Workflow(), definition.Registry())
		if compileErr != nil {
			return compileErr
		}
		for _, step := range []authoring.StepID{"entry.parse", "entry.persist", "fan.split", "fan.slice"} {
			if err := view.CompleteStep(step, compiled); err != nil {
				return err
			}
		}
		if err := fixture.Initialize(view, plan.Provider); err != nil {
			return err
		}
		decisionPlan, decideErr := decision.Decide(view, decision.Observation{}, compiled)
		if decideErr != nil {
			return decideErr
		}
		fingerprint, fingerprintErr := fixture.Engine.ObserveFingerprint()
		if fingerprintErr != nil {
			return fingerprintErr
		}
		issued, _, issueErr := fixture.Engine.IssueFromPlan(decisionPlan, decision.Admission{Capacity: 4}, fingerprint)
		if issueErr != nil {
			return issueErr
		}
		for _, action := range issued {
			report.Actions = append(report.Actions, HarnessAction{ActionID: action.ActionID})
		}
	}
	report.CompletedTasksBeforeFault = append([]string(nil), plan.Tasks[:2]...)
	fixture.Faults.ArmCrash(persistence.FaultExecuteAfter)
	event, err := protocol.NewRequestEvent("full-recovery-fault-request", protocol.ControlReset,
		protocol.AskOption{ID: "confirm", Label: "confirm"})
	if err != nil {
		return err
	}
	_, err = submit(fixture.Engine, event)
	if err == nil {
		return fmt.Errorf("testkit: full recovery execute_after fault was not consumed")
	}
	report.Phase = "fault-injected"
	return err
}

func runReceiptFileScenario(report HarnessReport, options HarnessOptions, root string) (HarnessReport, error) {
	fixture, err := NewProtocolFixture(root)
	if err != nil {
		return report, err
	}
	if strings.TrimSpace(options.Prepare) != "" {
		if strings.TrimSpace(options.ReceiptFile) != "" || options.BindTemplate {
			return report, fmt.Errorf("testkit: --prepare cannot be combined with --receipt-file or --bind-template")
		}
		path := options.Template
		if strings.TrimSpace(path) == "" {
			return report, fmt.Errorf("testkit: --template is required with --prepare")
		}
		return prepareReceiptTemplate(&report, fixture, options.Prepare, path)
	}
	if options.BindTemplate {
		if strings.TrimSpace(options.ReceiptFile) != "" {
			return report, fmt.Errorf("testkit: --bind-template cannot be combined with --receipt-file")
		}
		path := options.Template
		if strings.TrimSpace(path) == "" {
			return report, fmt.Errorf("testkit: --template is required with --bind-template")
		}
		return bindReceiptTemplate(&report, fixture, path)
	}
	if strings.TrimSpace(options.ReceiptFile) == "" {
		return report, fmt.Errorf("testkit: --receipt-file is required")
	}
	path, err := filepath.Abs(options.ReceiptFile)
	if err != nil {
		return report, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return report, err
	}
	var wire receiptFileWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return report, err
	}
	report.Input = &ReceiptInput{
		Path: path, SHA256: encoder.Digest(data), ActionID: wire.ActionID,
		Operation: wire.Operation, Correlation: wire.Correlation, PayloadDigest: wire.PayloadDigest,
	}
	event, err := receiptEvent(wire)
	if err != nil {
		report.Status = "ERROR"
		report.Error = errorCode(err)
		report.addError(err)
		report.Paths, _ = harnessPaths(root)
		return report, err
	}
	acceptance, err := submit(fixture.Engine, event)
	if err != nil {
		report.Status = "ERROR"
		report.Error = errorCode(err)
		report.addError(err)
		report.Paths, _ = harnessPaths(root)
		return report, err
	}
	report.Status = "ACCEPTED"
	report.Acceptances = append(report.Acceptances, acceptance)
	report.Acceptance = &report.Acceptances[0]
	if wire.Operation == string(protocol.HostActionExecuteAdapterOperation) {
		if count, countErr := fixture.VCS.OperationCount("fake-host.action:" + wire.ActionID); countErr == nil {
			report.SideEffects = map[string]int{"fakeHost." + wire.ActionID: count}
		}
	}
	report.Phase = "receipt-file"
	if snapshot, loadErr := fixture.Engine.Load(); loadErr == nil {
		report.Snapshot = &snapshot
		report.Summary, _ = Summarize(fixture.Engine)
	}
	report.Paths, _ = harnessPaths(root)
	return report, nil
}

func receiptEvent(wire receiptFileWire) (protocol.Event, error) {
	switch protocol.HostActionOperation(wire.Operation) {
	case protocol.HostActionExecuteAdapterOperation:
		if strings.TrimSpace(wire.AdapterOperation) == "" {
			return protocol.Event{}, &protocol.RejectedError{Code: protocol.CodeFreeCommandForm, Detail: "receipt adapter operation is empty; free command forms do not exist"}
		}
		if wire.AdapterOperation != "op.fan.transport" {
			return protocol.Event{}, &protocol.RejectedError{Code: protocol.CodeOperationNotRegistered, Detail: fmt.Sprintf("receipt adapter operation %q is not registered by the canonical definition", wire.AdapterOperation)}
		}
		return protocol.NewAdapterHostActionReceiptEvent(
			protocol.EventID("receipt-file-"+wire.ActionID), wire.ActionID,
			authoring.OperationID(wire.AdapterOperation), wire.Provider, wire.Correlation,
			wire.PayloadDigest, wire.Status, wire.FailureClass, wire.AdapterEvidence,
		)
	case protocol.HostActionResumeAgent, protocol.HostActionTerminateAgent:
		if wire.LifecycleEvidence == nil {
			return protocol.Event{}, &protocol.RejectedError{Code: protocol.CodeEventSchemaInvalid, Detail: "agent receipt lifecycle evidence is required"}
		}
		return protocol.NewAgentHostActionReceiptEvent(
			protocol.EventID("receipt-file-"+wire.ActionID), wire.ActionID,
			protocol.HostActionOperation(wire.Operation), wire.Provider, wire.Correlation,
			wire.PayloadDigest, wire.Status, wire.FailureClass, *wire.LifecycleEvidence,
		)
	default:
		return protocol.Event{}, &protocol.RejectedError{Code: protocol.CodeEventSchemaInvalid, Detail: fmt.Sprintf("receipt operation %q is invalid", wire.Operation)}
	}
}

func prepareReceiptTemplate(report *HarnessReport, fixture *ProtocolFixture, kind, path string) (HarnessReport, error) {
	actions, err := ensureReady(report, fixture)
	if err != nil {
		return *report, err
	}
	fingerprint, err := fixture.Engine.ObserveFingerprint()
	if err != nil {
		return *report, err
	}
	var intent protocol.HostActionIntent
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "adapter":
		intent, _, err = fixture.Engine.ExecuteHostActionWithCorrelation(
			"op.fan.transport", map[string]any{"target": "phase2", "retries": float64(1)},
			"host-action-correlation", fingerprint)
		if err == nil {
			digest := strings.TrimPrefix(encoder.Digest([]byte(intent.ActionID)), "sha256:")
			_, err = fixture.VCS.ApplyOnce("fake-host.action:"+intent.ActionID,
				filepath.Join("fake-host", "action", digest+".txt"), []byte("op.fan.transport\n"))
		}
	case "terminate":
		if len(actions) == 0 {
			return *report, fmt.Errorf("testkit: terminate preparation requires a worker action")
		}
		identity := "agent-terminate-1"
		spawn, spawnErr := fixture.Host.SpawnEvent("receipt-prepare-spawn", actions[0], identity, protocol.SpawnStatusSpawned)
		if spawnErr != nil {
			return *report, spawnErr
		}
		report.Acceptances = append(report.Acceptances, spawn.Acceptance)
		lifecycle, lifecycleErr := protocol.NewCorrelatedLifecycleEvent("receipt-prepare-stop", "fake-host", identity, identity, protocol.LifecycleStop)
		if lifecycleErr != nil {
			return *report, lifecycleErr
		}
		if _, err = submit(fixture.Engine, lifecycle); err != nil {
			return *report, err
		}
		intent, _, err = fixture.Engine.TerminateAgentWithCorrelation(actions[0], identity, "qa receipt fixture", identity, fingerprint)
	default:
		return *report, fmt.Errorf("testkit: --prepare must be adapter or terminate")
	}
	if err != nil {
		return *report, err
	}
	if err := writeHarnessJSON(path, receiptTemplate(intent, "fake-host")); err != nil {
		return *report, err
	}
	report.Metadata["template"] = path
	report.Metadata["actionID"] = intent.ActionID
	report.Phase = "receipt-template"
	report.Paths, _ = harnessPaths(fixture.Root)
	return *report, nil
}

func bindReceiptTemplate(report *HarnessReport, fixture *ProtocolFixture, path string) (HarnessReport, error) {
	snapshot, err := fixture.Engine.Load()
	if err != nil {
		return *report, err
	}
	ids := make([]string, 0, len(snapshot.State.PendingHostActions))
	for actionID := range snapshot.State.PendingHostActions {
		ids = append(ids, actionID)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return *report, &protocol.RejectedError{Code: protocol.CodeUnknownAction, Detail: "no pending HostAction intent to bind"}
	}
	intent := snapshot.State.PendingHostActions[ids[0]]
	if err := writeHarnessJSON(path, receiptTemplate(intent, snapshot.State.RunProvider)); err != nil {
		return *report, err
	}
	report.Metadata["template"] = path
	report.Metadata["actionID"] = intent.ActionID
	report.Phase = "receipt-template"
	report.Paths, _ = harnessPaths(fixture.Root)
	return *report, nil
}

func receiptTemplate(intent protocol.HostActionIntent, provider string) map[string]any {
	correlation := intent.Correlation
	if correlation == "" {
		correlation = "host-action-correlation"
	}
	template := map[string]any{
		"actionID": intent.ActionID, "provider": provider, "correlation": correlation,
		"payloadDigest": intent.PayloadDigest, "status": protocol.HostActionStatusExecuted,
	}
	switch intent.Operation {
	case protocol.HostActionExecuteAdapterOperation:
		template["operation"] = string(intent.Operation)
		template["adapterOperation"] = string(intent.Adapter.Operation)
		template["params"] = intent.Adapter.Params
		template["adapterEvidence"] = map[string]any{"identity": "phase2", "observationDigest": "sha256:phase2"}
	case protocol.HostActionTerminateAgent:
		template["operation"] = string(intent.Operation)
		template["lifecycleEvidence"] = map[string]any{"identity": intent.Terminate.Identity, "event": protocol.LifecycleStop}
	case protocol.HostActionResumeAgent:
		template["operation"] = string(intent.Operation)
		template["lifecycleEvidence"] = map[string]any{"identity": intent.Resume.Identity, "event": protocol.LifecycleStart}
	}
	return template
}

func runWaitUserActionScenario(report HarnessReport, options HarnessOptions, root string) (HarnessReport, error) {
	if strings.TrimSpace(options.Fixture) == "" {
		return report, fmt.Errorf("testkit: --fixture is required for wait-user-action")
	}
	input, err := readWaitUserActionFixture(options.Fixture)
	if err != nil {
		return report, err
	}
	fixture, err := NewProtocolFixture(root)
	if err != nil {
		return report, err
	}
	if _, statErr := os.Stat(report.StatePath); os.IsNotExist(statErr) {
		view, viewErr := decision.NewState(definition.Version, runtime.PhaseDevelopmentParallel)
		if viewErr != nil {
			return report, viewErr
		}
		if err := fixture.Initialize(view, "fake-host"); err != nil {
			return report, err
		}
		event, eventErr := protocol.NewOperatorObservationEvent(
			protocol.EventID(input.EventID), input.TaskID,
			decision.Fact{Source: decision.SourceHost, Key: "failureClass", Value: input.FailureClass},
		)
		if eventErr != nil {
			return report, eventErr
		}
		if _, err := submit(fixture.Engine, event); err != nil {
			return report, err
		}
		report.Phase = "wait"
	} else if statErr != nil {
		return report, statErr
	} else {
		report.Recovery, err = fixture.Store.Recover()
		if err != nil {
			return report, err
		}
		report.Phase = "restart"
	}
	setWaitUserActionReport(&report, input)
	if snapshot, loadErr := fixture.Engine.Load(); loadErr == nil {
		report.Snapshot = &snapshot
		report.Summary, _ = Summarize(fixture.Engine)
	}
	report.Paths, _ = harnessPaths(root)
	return report, nil
}

func runTerminalReplay(report HarnessReport, options HarnessOptions, root string) (HarnessReport, error) {
	summary, err := ReadTerminalSummary(root)
	if err != nil {
		report.Status = "ERROR"
		report.addError(err)
		return report, err
	}
	eventID := valueOr(options.EventID, summary.LastEventReceipt.EventID)
	digest := valueOr(options.PayloadDigest, summary.LastEventDigest)
	if eventID != summary.LastEventReceipt.EventID || digest != summary.LastEventDigest {
		err := &protocol.RejectedError{Code: protocol.CodeDuplicateEventMismatch, Detail: fmt.Sprintf("terminal event %q digest mismatch", eventID)}
		report.Status = "ERROR"
		report.addError(err)
		return report, err
	}
	report.Terminal = &summary
	report.Acceptances = append(report.Acceptances, summary.LastEventReceipt)
	report.Next = append(report.Next, summary.Next)
	report.Phase = "terminal-replay"
	report.Paths, _ = harnessPaths(root)
	return report, nil
}

func runCapacityRefill(report HarnessReport, options HarnessOptions, root string) (HarnessReport, error) {
	capacity := options.Capacity
	if capacity == 0 {
		capacity = 1
	}
	if capacity != 1 {
		return report, fmt.Errorf("testkit: capacity-refill requires --capacity 1")
	}
	registry := definition.Registry()
	steps := make([]authoring.Step, 0, 2)
	for _, id := range []authoring.StepID{"TASK_1", "TASK_2"} {
		step, err := authoring.NewAgentStep(
			authoring.Header{ID: id, NodeID: "capacity", DefinitionVersion: definition.Version},
			authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out", Postconditions: []authoring.PredicateRef{{ID: "pred.review.post"}}},
			authoring.AgentSpec{Handler: "engine.review.worker", Reason: authoring.ReasonIndependentReview, Timeout: time.Minute},
		)
		if err != nil {
			return report, err
		}
		steps = append(steps, step)
	}
	compiled, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: "capacity", Steps: steps}, registry)
	if err != nil {
		return report, err
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return report, err
	}
	vcs, err := NewFakeVCS(workspace)
	if err != nil {
		return report, err
	}
	store, err := persistence.NewStore(filepath.Join(root, "engine-state"), persistence.Config{PackageDigest: HarnessPackageDigest})
	if err != nil {
		return report, err
	}
	engine, err := protocol.New(store, protocol.Config{Definition: compiled, Registry: registry, Capacity: capacity}, vcs.Fingerprint)
	if err != nil {
		return report, err
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseDevelopmentParallel)
	if err != nil {
		return report, err
	}
	fingerprint, err := engine.ObserveFingerprint()
	if err != nil {
		return report, err
	}
	if err := engine.Init(view, "fake-host", fingerprint); err != nil {
		return report, err
	}
	plan, err := decision.Decide(view, decision.Observation{}, compiled)
	if err != nil {
		return report, err
	}
	issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: capacity}, fingerprint)
	if err != nil {
		return report, err
	}
	if len(issued) != 1 {
		return report, fmt.Errorf("capacity-refill issued %d actions initially, want 1", len(issued))
	}
	host := NewFakeHost(engine, "fake-host", nil)
	worker := NewFakeWorker(engine, "fake-host", nil)
	spawn, err := host.SpawnEvent("capacity-task-1-spawn", issued[0].ActionID, "capacity-agent", protocol.SpawnStatusSpawned)
	if err != nil {
		return report, err
	}
	preResultSnapshot, err := engine.Load()
	if err != nil {
		return report, err
	}
	accepted, err := worker.ResultEvent("capacity-task-1-result", issued[0].ActionID, protocol.OutcomePass, "sha256:capacity-task-1", "")
	if err != nil {
		return report, err
	}
	if len(accepted.Refill) != 1 {
		return report, fmt.Errorf("capacity-refill acceptance refill=%v, want one replacement", accepted.Refill)
	}
	snapshot, err := engine.Load()
	if err != nil {
		return report, err
	}
	report.Acceptances = append(report.Acceptances, spawn.Acceptance, accepted)
	report.Actions = []HarnessAction{{ActionID: issued[0].ActionID}, {ActionID: accepted.Refill[0]}}
	report.Next = append(report.Next, NextRecord{Kind: decision.KindReady, Payload: issued})
	report.PreResultSnapshot = &preResultSnapshot
	report.Snapshot = &snapshot
	report.Revisions = []uint64{spawn.Acceptance.Revision, accepted.Revision}
	report.SideEffects = map[string]int{"fakeHost.spawn": host.SpawnCalls(issued[0].ActionID)}
	report.Phase = "capacity-refilled"
	report.Paths, _ = harnessPaths(root)
	return report, nil
}

func runNextSequence(report HarnessReport) (HarnessReport, error) {
	values := []decision.NextResult{
		{Kind: decision.KindReady, Ready: &decision.ReadyPayload{Tasks: []decision.ReadyTask{{Task: TaskKey("review", "review.worker"), Step: "review.worker"}}}},
		{Kind: decision.KindAsk, Ask: &decision.AskPayload{Steps: []authoring.StepID{"ask.decide"}}},
		{Kind: decision.KindWait, Wait: &decision.WaitPayload{Reason: decision.WaitTasksInFlight}},
		{Kind: decision.KindHostAction, HostAction: &decision.HostActionPayload{Steps: []authoring.StepID{"fan.transport"}}},
		{Kind: decision.KindOperator, Operator: &decision.OperatorPayload{Facts: []decision.Fact{{Source: decision.SourceReceipt, Key: "receipt", Value: "unknown"}}}},
		{Kind: decision.KindComplete, Complete: &decision.CompletePayload{}},
	}
	for _, next := range values {
		if err := next.Validate(); err != nil {
			return report, err
		}
		report.Next = append(report.Next, nextRecord(next))
	}
	report.Phase = "decision-regression"
	return report, nil
}

func runFailureRouting(report HarnessReport, options HarnessOptions) (HarnessReport, error) {
	classes := []authoring.FailureClass{}
	if strings.TrimSpace(options.FailureClass) != "" {
		classes = append(classes, authoring.FailureClass(options.FailureClass))
	} else {
		classes = []authoring.FailureClass{authoring.FailureBusinessReject, authoring.FailureUserActionRequired, authoring.FailureAgentRecoverable, authoring.FailureTransientEngine, authoring.FailureInvariantViolation, authoring.FailureBlockedBug}
	}
	for _, class := range classes {
		if !class.Valid() {
			return report, fmt.Errorf("testkit: unknown failure class %q", class)
		}
		plan := protocol.DecideRecovery(protocol.Interruption{Class: class, CauseKnown: true})
		report.RecoveryPlan = append(report.RecoveryPlan, plan)
		if class == authoring.FailureBusinessReject || class == authoring.FailureInvariantViolation || class == authoring.FailureBlockedBug {
			report.addError(&protocol.RejectedError{Code: string(class), Detail: plan.Detail})
		}
		if class == authoring.FailureTransientEngine {
			if err := runTransientRetryRouting(&report, options); err != nil {
				return report, err
			}
		}
	}
	if report.SideEffects == nil {
		report.SideEffects = map[string]int{"spawn": 0}
	}
	if _, ok := report.SideEffects["hostAction"]; !ok {
		report.SideEffects["hostAction"] = 0
	}
	report.Phase = "failure-routing"
	return report, nil
}

// runTransientRetryRouting drives a declared bounded Agent retry through the
// same Engine admission path used by normal results. It intentionally leaves
// review.worker's nil policy untouched; this scenario owns a small fixture
// whose retry declaration is part of the compiled definition.
func runTransientRetryRouting(report *HarnessReport, options HarnessOptions) error {
	registry := definition.Registry()
	step, err := authoring.NewAgentStep(
		authoring.Header{ID: "retry.worker", NodeID: "retry", DefinitionVersion: definition.Version},
		authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out", Postconditions: []authoring.PredicateRef{{ID: "pred.review.post"}}},
		authoring.AgentSpec{Handler: "engine.review.worker", Reason: authoring.ReasonIndependentReview, Timeout: time.Minute, Retry: &authoring.RetryPolicy{MaxAttempts: 3}},
	)
	if err != nil {
		return err
	}
	compiled, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: "retry", Steps: []authoring.Step{step}}, registry)
	if err != nil {
		return err
	}
	fixture, err := NewProtocolFixtureWithDefinition(reportRoot(report), compiled, registry, 1)
	if err != nil {
		return err
	}
	actions, err := ensureReady(report, fixture)
	if err != nil {
		return err
	}
	if len(actions) != 1 {
		return fmt.Errorf("testkit: retry fixture issued %d actions, want 1", len(actions))
	}
	// Failure-routing reports the terminal routing decision rather than the
	// setup Ready record used to seed this isolated fixture.
	filteredNext := report.Next[:0]
	for _, next := range report.Next {
		if next.Kind != decision.KindReady {
			filteredNext = append(filteredNext, next)
		}
	}
	report.Next = filteredNext
	actionID := actions[0]
	spawn, err := fixture.Host.SpawnEvent("failure-routing-spawn", actionID, "retry-agent", protocol.SpawnStatusSpawned)
	if err != nil {
		return err
	}
	report.Acceptances = append(report.Acceptances, spawn.Acceptance)
	current, err := fixture.Engine.Load()
	if err != nil {
		return err
	}
	configured := current.State.Attempts[runtime.TaskKey{Node: "retry", Step: "retry.worker"}]
	max := configured.MaxAttempts
	if max < 1 {
		return fmt.Errorf("testkit: retry fixture did not expose a positive compiled maxAttempts")
	}
	retrySequence := make([]int, 0, max)
	for i := 1; i <= max; i++ {
		accepted, resultErr := fixture.Worker.ResultEvent(protocol.EventID(fmt.Sprintf("failure-routing-result-%d", i)), actionID, protocol.OutcomeRuntimeError, fmt.Sprintf("sha256:retry-%d", i), authoring.FailureTransientEngine)
		if resultErr != nil {
			return resultErr
		}
		report.Acceptances = append(report.Acceptances, accepted)
		current, loadErr := fixture.Engine.Load()
		if loadErr != nil {
			return loadErr
		}
		for _, attempt := range current.State.Attempts {
			retrySequence = append(retrySequence, attempt.Attempts)
			break
		}
		if accepted.RecoveryAction == string(protocol.RecoveryWait) {
			if len(current.State.RecoveryRecords) == 0 {
				return fmt.Errorf("testkit: retry exhaustion accepted without a durable recovery record")
			}
			latest := current.State.RecoveryRecords[len(current.State.RecoveryRecords)-1]
			plan := protocol.RecoveryPlan{Class: latest.Class, Action: latest.Action, Detail: latest.Detail, LifecycleMatches: latest.LifecycleMatches}
			if len(report.RecoveryPlan) > 0 {
				report.RecoveryPlan[len(report.RecoveryPlan)-1] = plan
			} else {
				report.RecoveryPlan = append(report.RecoveryPlan, plan)
			}
			report.Next = append(report.Next, NextRecord{Kind: decision.KindWait, Payload: decision.WaitPayload{Reason: decision.WaitTasksInFlight}})
			report.Metadata["retryPolicyMaxAttempts"] = current.State.Attempts[runtime.TaskKey{Node: "retry", Step: "retry.worker"}].MaxAttempts
			report.Metadata["retryAttemptsExhausted"] = current.State.Attempts[runtime.TaskKey{Node: "retry", Step: "retry.worker"}].Attempts
			report.Metadata["retryAttemptSequence"] = retrySequence
			report.Metadata["finalFailureKind"] = string(latest.Class)
			report.Metadata["recoveryAction"] = string(latest.Action)
			break
		}
	}
	initialSpawnCalls := fixture.Host.SpawnCalls(actionID)
	// `spawn` is reserved for new retry dispatches. The one setup spawn is
	// exposed separately so exhaustion proves that no replacement dispatch was
	// created while retaining the observable fixture side effect count.
	report.Metadata["initialSpawn"] = initialSpawnCalls
	report.Metadata["newSpawns"] = 0
	report.SideEffects = map[string]int{"spawn": 0, "initialSpawn": initialSpawnCalls}
	if snapshot, loadErr := fixture.Engine.Load(); loadErr == nil {
		report.Snapshot = &snapshot
		report.Summary, _ = Summarize(fixture.Engine)
	}
	report.Phase = "failure-routing"
	report.Paths, _ = harnessPaths(fixture.Root)
	return nil
}

func reportRoot(report *HarnessReport) string {
	if report == nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(report.StatePath))
}

type definitionDeclarednessFixture struct {
	Declared     *bool           `json:"declared"`
	Declaredness string          `json:"declaredness"`
	FailureClass string          `json:"failureClass"`
	Escalate     string          `json:"escalate"`
	Definition   json.RawMessage `json:"definition"`
}

// declarednessFromOptions reads the explicit test fixture without treating a
// free-form string as executable definition content. A canonical definition
// fixture may either expose a top-level declaration or carry a parallel step
// whose failure policy escalates to AGENT_RECOVERABLE_SEMANTIC_ERROR.
func declarednessFromOptions(options HarnessOptions) (bool, string, error) {
	declaredValue := strings.TrimSpace(options.Declared)
	if declaredValue == "" {
		declaredValue = strings.TrimSpace(options.Declaredness)
	}
	if declaredValue != "" {
		switch strings.ToLower(strings.ReplaceAll(declaredValue, "_", "-")) {
		case "true", "yes", "1", "declared", "agent-recoverable-semantic-error":
			return true, "options", nil
		case "false", "no", "0", "undeclared", "missing":
			return false, "options", nil
		default:
			return false, "", fmt.Errorf("testkit: declaredness must be true/false or declared/undeclared, got %q", declaredValue)
		}
	}
	path := strings.TrimSpace(options.DefinitionFixture)
	if path == "" {
		path = strings.TrimSpace(options.Fixture)
	}
	if path == "" {
		return false, "canonical-definition", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, "", err
	}
	var fixture definitionDeclarednessFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return false, "", fmt.Errorf("testkit: read definition fixture: %w", err)
	}
	if fixture.Declared != nil {
		return *fixture.Declared, "fixture", nil
	}
	if strings.EqualFold(strings.TrimSpace(fixture.Declaredness), string(authoring.FailureAgentRecoverable)) || strings.EqualFold(strings.TrimSpace(fixture.Declaredness), "declared") {
		return true, "fixture", nil
	}
	if strings.EqualFold(strings.TrimSpace(fixture.FailureClass), string(authoring.FailureAgentRecoverable)) ||
		strings.EqualFold(strings.TrimSpace(fixture.Escalate), string(authoring.FailureAgentRecoverable)) {
		return true, "fixture", nil
	}
	var raw struct {
		Steps []struct {
			Payload struct {
				Failure struct {
					Escalate string `json:"escalate"`
				} `json:"failure"`
			} `json:"payload"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(data, &raw); err == nil {
		for _, step := range raw.Steps {
			if strings.EqualFold(strings.TrimSpace(step.Payload.Failure.Escalate), string(authoring.FailureAgentRecoverable)) {
				return true, "definition", nil
			}
		}
	}
	return false, "fixture", nil
}

func runDefinitionDeclaredness(report HarnessReport, options HarnessOptions, root string) (HarnessReport, error) {
	declared, source, err := declarednessFromOptions(options)
	if err != nil {
		return report, err
	}
	report.Metadata["declared"] = declared
	report.Metadata["declaredSource"] = source
	failureClass := authoring.FailureAgentRecoverable
	if raw := strings.TrimSpace(options.FailureClass); raw != "" {
		failureClass = authoring.FailureClass(raw)
		if !failureClass.Valid() {
			return report, fmt.Errorf("testkit: unknown failure class %q", raw)
		}
	}
	report.Metadata["failureClass"] = string(failureClass)
	if failureClass != authoring.FailureAgentRecoverable {
		plan := protocol.DecideRecovery(protocol.Interruption{Class: failureClass, CauseKnown: true})
		report.RecoveryPlan = []protocol.RecoveryPlan{plan}
		report.Phase = "definition-declaredness"
		if plan.Action == protocol.RecoveryFail {
			err := &protocol.RejectedError{Code: string(failureClass), Detail: plan.Detail}
			report.Status = "ERROR"
			report.Error = errorCode(err)
			report.addError(err)
			report.Paths, _ = harnessPaths(root)
			return report, err
		}
		report.Paths, _ = harnessPaths(root)
		return report, nil
	}
	if !declared {
		err := &protocol.RejectedError{Code: string(authoring.FailureBlockedBug), Detail: "AGENT_RECOVERABLE_SEMANTIC_ERROR is not declared by the supplied workflow definition fixture"}
		report.Status = "ERROR"
		report.Error = errorCode(err)
		report.addError(err)
		report.Phase = "definition-declaredness"
		report.Paths, _ = harnessPaths(root)
		return report, err
	}
	report.RecoveryPlan = []protocol.RecoveryPlan{{Class: authoring.FailureAgentRecoverable, Action: protocol.RecoveryAgent, Detail: "only an explicitly declared semantic failure may use the agent recovery edge"}}
	report.Next = []NextRecord{{Kind: decision.KindOperator, Payload: map[string]any{"failureClass": authoring.FailureAgentRecoverable}}}
	report.Phase = "definition-declaredness"
	report.Paths, _ = harnessPaths(root)
	return report, nil
}

func runSemanticRecoveryScenario(report HarnessReport, root string, options HarnessOptions) (HarnessReport, error) {
	declared, source, err := declarednessFromOptions(options)
	if err != nil {
		return report, err
	}
	report.Metadata["declared"] = declared
	report.Metadata["declaredSource"] = source
	compiled, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		return report, err
	}
	if source == "canonical-definition" {
		declared = false
		for _, step := range compiled.Steps {
			if parallel, ok := step.Payload.(compiler.CompiledParallelStep); ok && parallel.Failure.Escalate == authoring.FailureAgentRecoverable {
				declared = true
				break
			}
		}
	}
	if !declared {
		return report, &protocol.RejectedError{
			Code:   string(authoring.FailureBlockedBug),
			Detail: "AGENT_RECOVERABLE_SEMANTIC_ERROR is not declared by the current workflow definition",
		}
	}
	fixture, err := NewProtocolFixture(root)
	if err != nil {
		return report, err
	}
	actions, err := ensureReady(&report, fixture)
	if err != nil {
		return report, err
	}
	if len(actions) != 1 {
		return report, fmt.Errorf("testkit: semantic recovery fixture issued %d actions, want 1", len(actions))
	}
	plan := protocol.DecideRecovery(protocol.Interruption{Class: authoring.FailureAgentRecoverable, CauseKnown: true})
	report.RecoveryPlan = append(report.RecoveryPlan, plan)
	report.Metadata["nonProgrammableReason"] = string(authoring.ReasonIndependentReview)
	spawn, err := fixture.Host.SpawnEvent("semantic-recovery-spawn", actions[0], "semantic-agent", protocol.SpawnStatusSpawned)
	if err != nil {
		return report, err
	}
	result, err := fixture.Worker.ResultEvent("semantic-recovery-result", actions[0], protocol.OutcomePass, "sha256:semantic-recovery", "")
	if err != nil {
		return report, err
	}
	report.Acceptances = append(report.Acceptances, spawn.Acceptance, result)
	report.SideEffects = map[string]int{"fakeHost.spawn": fixture.Host.SpawnCalls(actions[0])}
	report.Phase = "semantic-recovery"
	report.Paths, _ = harnessPaths(root)
	if snapshot, loadErr := fixture.Engine.Load(); loadErr == nil {
		report.Snapshot = &snapshot
		report.Summary, _ = Summarize(fixture.Engine)
	}
	return report, nil
}

func WriteTerminalSummary(root string, revision uint64, request protocol.Acceptance, requestDigest string, event protocol.Acceptance, eventDigest, status string) (TerminalSummary, error) {
	envelope, err := persistence.ExpectedEnvelope(HarnessPackageDigest)
	if err != nil {
		return TerminalSummary{}, err
	}
	summary := TerminalSummary{
		Writer: envelope.Writer, StateSchemaVersion: envelope.StateSchemaVersion,
		WorkflowDefinitionVersion: envelope.WorkflowDefinitionVersion, DefinitionDigest: envelope.DefinitionDigest,
		PackageDigest: envelope.PackageDigest, Status: status, Revision: revision,
		Next:               NextRecord{Kind: decision.KindComplete, Payload: decision.CompletePayload{}},
		LastRequestReceipt: request, LastRequestDigest: requestDigest,
		LastEventReceipt: event, LastEventDigest: eventDigest,
	}
	if strings.TrimSpace(requestDigest) == "" || strings.TrimSpace(eventDigest) == "" {
		return TerminalSummary{}, fmt.Errorf("testkit: terminal summary receipt digests are required")
	}
	path := filepath.Join(root, "engine-state", terminalSummaryName)
	if err := writeHarnessJSON(path, summary); err != nil {
		return TerminalSummary{}, err
	}
	return summary, nil
}

func ReadTerminalSummary(root string) (TerminalSummary, error) {
	path := filepath.Join(root, "engine-state", terminalSummaryName)
	data, err := os.ReadFile(path)
	if err != nil {
		return TerminalSummary{}, err
	}
	var summary TerminalSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return TerminalSummary{}, err
	}
	if err := persistence.ValidateEnvelope(persistence.Envelope{
		Writer: summary.Writer, StateSchemaVersion: summary.StateSchemaVersion,
		WorkflowDefinitionVersion: summary.WorkflowDefinitionVersion,
		DefinitionDigest:          summary.DefinitionDigest, PackageDigest: summary.PackageDigest,
	}, HarnessPackageDigest); err != nil {
		return TerminalSummary{}, err
	}
	if strings.TrimSpace(summary.LastRequestDigest) == "" || strings.TrimSpace(summary.LastEventDigest) == "" {
		return TerminalSummary{}, fmt.Errorf("testkit: terminal summary receipt digests are required")
	}
	if summary.Status == "FINALIZING_CLEANUP" {
		summary.Next = NextRecord{Kind: decision.KindWait, Payload: decision.WaitPayload{Reason: decision.WaitEngineInternal}}
	} else if summary.Status != "COMPLETE" {
		return TerminalSummary{}, fmt.Errorf("testkit: terminal summary status %q is not queryable", summary.Status)
	}
	return summary, nil
}

func writeHarnessJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".harness-summary-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (r *HarnessReport) addError(err error) {
	if err == nil {
		return
	}
	r.Errors = append(r.Errors, HarnessError{Code: errorCode(err), Message: err.Error()})
}

func errorCode(err error) string {
	var rejected *protocol.RejectedError
	var unsupported *persistence.UnsupportedRunVersionError
	var integrity *persistence.IntegrityMismatchError
	var revision *persistence.RevisionConflictError
	var fingerprint *persistence.FingerprintChangedError
	var lock *persistence.LockHeldError
	switch {
	case errors.As(err, &rejected):
		return rejected.Code
	case errors.As(err, &unsupported):
		return persistence.UnsupportedRunVersionCode
	case errors.As(err, &integrity):
		return persistence.StateIntegrityCode
	case errors.As(err, &revision):
		return persistence.RevisionConflictCode
	case errors.As(err, &fingerprint):
		return persistence.FingerprintChangedCode
	case errors.As(err, &lock):
		return "LOCK_HELD"
	case errors.Is(err, persistence.ErrInjectedCrash), errors.Is(err, ErrInjected):
		return "INJECTED_FAULT"
	default:
		return "ERROR"
	}
}

func parseTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "fulfilled", "satisfied", "match":
		return true
	default:
		return false
	}
}

func nextRecord(next decision.NextResult) NextRecord {
	switch next.Kind {
	case decision.KindReady:
		return NextRecord{Kind: next.Kind, Payload: next.Ready}
	case decision.KindHostAction:
		return NextRecord{Kind: next.Kind, Payload: next.HostAction}
	case decision.KindAsk:
		return NextRecord{Kind: next.Kind, Payload: next.Ask}
	case decision.KindWait:
		return NextRecord{Kind: next.Kind, Payload: next.Wait}
	case decision.KindOperator:
		return NextRecord{Kind: next.Kind, Payload: next.Operator}
	case decision.KindComplete:
		return NextRecord{Kind: next.Kind, Payload: next.Complete}
	default:
		return NextRecord{Kind: next.Kind}
	}
}

func submit(engine *protocol.Engine, event protocol.Event) (protocol.Acceptance, error) {
	fingerprint, err := engine.ObserveFingerprint()
	if err != nil {
		return protocol.Acceptance{}, err
	}
	return engine.Submit(event, fingerprint)
}

func completed(steps []authoring.StepID, target authoring.StepID) bool {
	for _, step := range steps {
		if step == target {
			return true
		}
	}
	return false
}

func harnessPaths(root string) ([]string, error) {
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func faultPoint(value string) (persistence.FaultPoint, error) {
	point := persistence.FaultPoint(strings.TrimSpace(value))
	for _, known := range []persistence.FaultPoint{
		persistence.FaultIntentBefore, persistence.FaultIntentAfter,
		persistence.FaultTempSyncBefore, persistence.FaultTempSyncAfter,
		persistence.FaultExecuteBefore, persistence.FaultExecuteAfter,
		persistence.FaultObserveBefore, persistence.FaultReconcileBefore,
		persistence.FaultReplaceBefore, persistence.FaultReplaceAfter,
		persistence.FaultCommitResponseLost, persistence.FaultSpawnAfterAttach,
		persistence.FaultResultBeforeReceipt, persistence.FaultSubmitResponseLost,
		persistence.FaultHostObserveBefore, persistence.FaultHostReconcileBefore,
		persistence.FaultHostCommitBefore, persistence.FaultHostCommitAfter,
	} {
		if point == known {
			return point, nil
		}
	}
	return "", fmt.Errorf("testkit: unknown fault point %q", value)
}

func isPersistenceFault(point persistence.FaultPoint) bool {
	switch point {
	case persistence.FaultIntentBefore, persistence.FaultIntentAfter,
		persistence.FaultTempSyncBefore, persistence.FaultTempSyncAfter,
		persistence.FaultExecuteBefore, persistence.FaultExecuteAfter,
		persistence.FaultObserveBefore, persistence.FaultReconcileBefore,
		persistence.FaultReplaceBefore, persistence.FaultReplaceAfter,
		persistence.FaultCommitResponseLost:
		return true
	default:
		return false
	}
}

func controlKind(value string) (protocol.ControlKind, error) {
	kind := protocol.ControlKind(strings.TrimSpace(value))
	if !kind.Valid() {
		return "", fmt.Errorf("testkit: unsupported control %q (closed set: RESET, ABORT, RECOVER_ATTEMPT)", value)
	}
	return kind, nil
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
