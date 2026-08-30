// Package facade exposes the phase-3 candidate engine surface.  It is kept
// separate from the stable validate package so legacy workflow runs continue
// to use their existing writer and state namespace.
package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
)

const (
	EngineNamespace           = ".gates/engine"
	startIntentSuffix         = ".start.intent.json"
	terminalIntentName        = "terminal-summary.intent.json"
	DefaultDefinitionSource   = "definitions/workflow.json"
	RuntimeEngine             = "engine"
	RuntimeLegacy             = "legacy"
	DefaultProvider           = "engine"
	UnregisteredInstall       = "UNREGISTERED_INSTALL"
	UnsupportedEngineEntry    = "UNSUPPORTED_ENGINE_ENTRY"
	InvalidIntakeConfirmation = "INVALID_INTAKE_CONFIRMATION"
)

// IntakeArtifact and IntakeConfirmationReceipt are aliases for the typed
// protocol contracts.  Aliases make the façade API convenient without
// creating a second representation of the binding.
type IntakeArtifact = protocol.IntakeArtifact
type IntakeAuthority = protocol.IntakeAuthority
type IntakeConfirmationReceipt = protocol.IntakeConfirmationReceipt
type IntakeTransport = protocol.IntakeTransport
type IntakeReceipt = protocol.IntakeReceipt

const (
	DefaultIntakeSource    = "stable-driver"
	DefaultIntakeAuthority = protocol.IntakeAuthorityStableDriver
	DefaultIntakeTransport = protocol.IntakeTransportStableLauncher
)

// StartRequest is the candidate bootstrap request.  Runtime is not a caller
// selectable switch in normal use; an admitted target identity/package digest
// selects the candidate engine.  Runtime is retained as a read-only projection
// for diagnostics and is always set to "engine" by Start.
type StartRequest struct {
	RunID                     string                    `json:"runId"`
	Route                     string                    `json:"route"`
	Provider                  string                    `json:"provider,omitempty"`
	PackageDigest             string                    `json:"packageDigest"`
	InstalledTargetIdentity   string                    `json:"installedTargetIdentity"`
	AdmissionGeneration       uint64                    `json:"admissionGeneration"`
	AdmissionLease            string                    `json:"admissionLease"`
	AdmissionToken            string                    `json:"admissionToken"`
	DefinitionSource          string                    `json:"definitionSource"`
	DefinitionDigest          string                    `json:"definitionDigest"`
	IntakeConfirmationReceipt IntakeConfirmationReceipt `json:"intakeConfirmationReceipt"`
}

// Envelope is the exact six-field identity projected by the candidate state.
// The persistence store additionally records writer/revision/content digest.
type Envelope = persistence.Envelope

// Run is the read-only façade projection returned by show/status/next.
type Run struct {
	RunID                     string                     `json:"runId"`
	Runtime                   string                     `json:"runtime"`
	Route                     string                     `json:"route"`
	Status                    string                     `json:"status"`
	Revision                  uint64                     `json:"revision"`
	Envelope                  Envelope                   `json:"envelope"`
	IntakeConfirmationReceipt *IntakeConfirmationReceipt `json:"intakeConfirmationReceipt,omitempty"`
	IntakeReceipt             *IntakeReceipt             `json:"intakeReceipt,omitempty"`
	Next                      decision.NextResult        `json:"next"`
	AvailableActions          []string                   `json:"availableActions,omitempty"`
	SummaryPath               string                     `json:"summaryPath,omitempty"`
	Unverified                bool                       `json:"unverified,omitempty"`
}

// TerminalSummary is retained after the engine reaches Complete.  It is
// immutable for normal callers and supports terminal replay without reopening
// the active state writer.
type TerminalSummary struct {
	RunID         string              `json:"runId"`
	Runtime       string              `json:"runtime"`
	Route         string              `json:"route"`
	Status        string              `json:"status"`
	Revision      uint64              `json:"revision"`
	Envelope      Envelope            `json:"envelope"`
	IntakeReceipt *IntakeReceipt      `json:"intakeReceipt,omitempty"`
	Next          decision.NextResult `json:"next"`
	Unverified    bool                `json:"unverified,omitempty"`
	CompletedAt   string              `json:"completedAt"`
}

type startIntent struct {
	RunID                   string `json:"runId"`
	PackageDigest           string `json:"packageDigest"`
	InstalledTargetIdentity string `json:"installedTargetIdentity"`
	DefinitionSource        string `json:"definitionSource"`
	DefinitionDigest        string `json:"definitionDigest"`
}

type StartOptions struct {
	Root    string
	Request StartRequest
	// PackageRoot is used by the host/launcher to document which installed
	// target was admitted.  The façade never resolves a runtime from a caller
	// supplied identity; the launcher must provide Admission instead.
	PackageRoot string
	// ArtifactRoot is the candidate test-project root containing injected
	// requirement/solution artifacts. When set, receipt bindings are
	// recomputed before the first state write.
	ArtifactRoot string
	Admission    *Admission
	Definition   *compiler.CompiledDefinition
	Registry     *compiler.Registry
	Capacity     int
}

// Admission is a launcher-derived runtime binding.  It is intentionally
// separate from StartRequest so user input cannot select a package digest or
// installed identity.
type Admission struct {
	RegistryPath            string
	PackageDigest           string
	InstalledTargetIdentity string
	Generation              uint64
	Lease                   string
	Token                   string
}

type Facade struct {
	root       string
	runID      string
	stateDir   string
	metadata   string
	store      *persistence.Store
	engine     *protocol.Engine
	envelope   Envelope
	definition *compiler.CompiledDefinition
	capacity   int
}

// Start creates an engine candidate run after validating the complete typed
// intake receipt.  Validation happens before the first state directory write.
func Start(options StartOptions) (*Facade, Run, error) {
	if strings.TrimSpace(options.Root) == "" {
		return nil, Run{}, fmt.Errorf("engine start: root is required")
	}
	root, err := filepath.Abs(strings.TrimSpace(options.Root))
	if err != nil {
		return nil, Run{}, err
	}
	req := options.Request
	if strings.TrimSpace(req.RunID) == "" {
		return nil, Run{}, fmt.Errorf("engine start: run id is required")
	}
	if !validRunID(req.RunID) {
		return nil, Run{}, fmt.Errorf("engine start: run id %q is invalid", req.RunID)
	}
	runUnlock, err := coordination.AcquireRun(root, req.RunID)
	if err != nil {
		return nil, Run{}, err
	}
	defer runUnlock()
	if err := recoverStartIntent(root, req.RunID); err != nil {
		return nil, Run{}, err
	}
	var admissionUnlock func()
	if options.Admission != nil && strings.TrimSpace(options.Admission.RegistryPath) != "" {
		admissionUnlock, err = coordination.AcquirePath(options.Admission.RegistryPath+".lock", "admission")
		if err != nil {
			return nil, Run{}, err
		}
		defer admissionUnlock()
		if err := verifyAdmissionSnapshot(*options.Admission); err != nil {
			return nil, Run{}, err
		}
	}
	// Legacy and candidate runtimes share the run-id namespace. Refuse a
	// candidate writer before creating any engine files when the legacy run
	// already exists.
	legacyDir := filepath.Join(root, ".gates", "tmp", req.RunID)
	if _, statErr := os.Stat(legacyDir); statErr == nil {
		return nil, Run{}, fmt.Errorf("run %q already exists in legacy runtime", req.RunID)
	} else if !os.IsNotExist(statErr) {
		return nil, Run{}, statErr
	}
	if _, statErr := os.Stat(filepath.Join(root, ".gates", "results", req.RunID+".json")); statErr == nil {
		return nil, Run{}, fmt.Errorf("run %q already has a retained legacy result", req.RunID)
	} else if !os.IsNotExist(statErr) {
		return nil, Run{}, statErr
	}
	if strings.TrimSpace(req.Route) == "" {
		req.Route = "lightweight"
	}
	if req.Route != "lightweight" {
		return nil, Run{}, fmt.Errorf("engine start: route %q is unsupported in phase 3", req.Route)
	}
	if strings.TrimSpace(req.Provider) == "" {
		req.Provider = DefaultProvider
	}
	if options.Admission == nil || strings.TrimSpace(options.Admission.PackageDigest) == "" || strings.TrimSpace(options.Admission.InstalledTargetIdentity) == "" {
		return nil, Run{}, fmt.Errorf("UNREGISTERED_INSTALL: candidate start requires launcher admission")
	}
	if strings.TrimSpace(req.PackageDigest) != "" && req.PackageDigest != options.Admission.PackageDigest {
		return nil, Run{}, fmt.Errorf("UNREGISTERED_INSTALL: package digest does not match admitted target")
	}
	if strings.TrimSpace(req.InstalledTargetIdentity) != "" && req.InstalledTargetIdentity != options.Admission.InstalledTargetIdentity {
		return nil, Run{}, fmt.Errorf("UNREGISTERED_INSTALL: installed target identity does not match admitted target")
	}
	req.PackageDigest = options.Admission.PackageDigest
	req.InstalledTargetIdentity = options.Admission.InstalledTargetIdentity
	req.AdmissionGeneration = options.Admission.Generation
	req.AdmissionLease = options.Admission.Lease
	req.AdmissionToken = options.Admission.Token
	if strings.TrimSpace(req.PackageDigest) == "" {
		return nil, Run{}, fmt.Errorf("UNREGISTERED_INSTALL: package digest is required")
	}
	if strings.TrimSpace(req.InstalledTargetIdentity) == "" {
		return nil, Run{}, fmt.Errorf("UNREGISTERED_INSTALL: installed target identity is required")
	}
	if strings.TrimSpace(req.DefinitionSource) == "" {
		req.DefinitionSource = DefaultDefinitionSource
	}
	if strings.TrimSpace(req.DefinitionDigest) == "" {
		req.DefinitionDigest = definition.WorkflowDefinitionDigest
	}
	req.IntakeConfirmationReceipt = canonicalizeIntakeReceipt(req.IntakeConfirmationReceipt)
	if err := validateConfirmation(req.IntakeConfirmationReceipt, req.DefinitionSource); err != nil {
		return nil, Run{}, err
	}
	if strings.TrimSpace(options.ArtifactRoot) != "" {
		if err := validateArtifactBindings(options.ArtifactRoot, req.IntakeConfirmationReceipt); err != nil {
			return nil, Run{}, err
		}
	}
	digest, err := IntakeDigest(req.IntakeConfirmationReceipt)
	if err != nil {
		return nil, Run{}, err
	}
	req.IntakeConfirmationReceipt.Digest = digest
	if strings.TrimSpace(options.Request.IntakeConfirmationReceipt.Digest) != "" && options.Request.IntakeConfirmationReceipt.Digest != digest {
		return nil, Run{}, fmt.Errorf("%s: intake confirmation digest mismatch", InvalidIntakeConfirmation)
	}
	if options.Capacity < 0 {
		return nil, Run{}, fmt.Errorf("engine start: capacity must be non-negative")
	}
	cd := options.Definition
	reg := options.Registry
	if cd == nil {
		cd, err = compiler.Compile(definition.Workflow(), definition.Registry())
		if err != nil {
			return nil, Run{}, err
		}
	}
	if reg == nil {
		reg = definition.Registry()
	}
	if req.DefinitionDigest != definition.WorkflowDefinitionDigest {
		return nil, Run{}, unsupported("definitionDigest", definition.WorkflowDefinitionDigest, req.DefinitionDigest)
	}
	if req.DefinitionSource != DefaultDefinitionSource {
		return nil, Run{}, unsupported("definitionSource", DefaultDefinitionSource, req.DefinitionSource)
	}
	stateDir := filepath.Join(root, filepath.FromSlash(EngineNamespace), req.RunID)
	if _, err := os.Stat(stateDir); err == nil {
		return nil, Run{}, fmt.Errorf("engine start: run %q already exists", req.RunID)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, Run{}, err
	}
	intentPath := startIntentPath(root, req.RunID)
	if err := writeJSONAtomic(intentPath, startIntent{RunID: req.RunID, PackageDigest: req.PackageDigest, InstalledTargetIdentity: req.InstalledTargetIdentity, DefinitionSource: req.DefinitionSource, DefinitionDigest: req.DefinitionDigest}); err != nil {
		return nil, Run{}, err
	}
	intentWritten := true
	defer func() {
		if intentWritten {
			_ = os.Remove(intentPath)
		}
	}()
	// Atomically reserve the run directory. A separate Stat followed by
	// MkdirAll lets concurrent starts race past the collision check; when one
	// then fails its cleanup could remove the other process's active directory.
	// Creating the parent is safe to share, while Mkdir on the final component
	// gives exactly one starter ownership of this run id.
	if err := os.MkdirAll(filepath.Dir(stateDir), 0o700); err != nil {
		return nil, Run{}, err
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, Run{}, fmt.Errorf("engine start: run %q already exists", req.RunID)
		}
		return nil, Run{}, err
	}
	env := Envelope{
		Writer: persistence.Writer, StateSchemaVersion: encoder.StateSchemaVersion,
		WorkflowDefinitionVersion: definition.WorkflowDefinitionVersion,
		DefinitionSource:          req.DefinitionSource, DefinitionDigest: req.DefinitionDigest,
		PackageDigest: req.PackageDigest, InstalledTargetIdentity: req.InstalledTargetIdentity,
	}
	store, err := persistence.NewStoreWithIdentity(stateDir, persistence.Config{
		PackageDigest: req.PackageDigest, DefinitionSource: req.DefinitionSource,
		InstalledTargetIdentity: req.InstalledTargetIdentity,
	})
	if err != nil {
		_ = os.RemoveAll(stateDir)
		return nil, Run{}, err
	}
	eng, err := protocol.New(store, protocol.Config{Definition: cd, Registry: reg, Capacity: options.Capacity}, nil)
	if err != nil {
		_ = os.RemoveAll(stateDir)
		return nil, Run{}, err
	}
	view, err := decision.NewState(cd.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		_ = os.RemoveAll(stateDir)
		return nil, Run{}, err
	}
	if err := eng.InitWithMetadata(view, req.Provider, emptyFingerprint(), req.RunID, req.Route, &req.IntakeConfirmationReceipt); err != nil {
		_ = os.RemoveAll(stateDir)
		return nil, Run{}, err
	}
	f := &Facade{root: root, runID: req.RunID, stateDir: stateDir, metadata: filepath.Join(stateDir, "request.json"), store: store, engine: eng, envelope: env, definition: cd, capacity: options.Capacity}
	if err := writeJSONAtomic(f.metadata, req); err != nil {
		_ = os.RemoveAll(stateDir)
		return nil, Run{}, err
	}
	run, err := f.project()
	if err != nil {
		_ = os.RemoveAll(stateDir)
		return nil, Run{}, err
	}
	intentWritten = false
	_ = os.Remove(intentPath)
	return f, run, nil
}

// Open loads an existing engine run and validates all six identity fields
// before exposing a writer.  No fallback to the legacy namespace occurs.
func Open(root, runID string) (*Facade, error) {
	root, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, err
	}
	if !validRunID(runID) {
		return nil, fmt.Errorf("engine open: run id %q is invalid", runID)
	}
	stateDir := filepath.Join(root, filepath.FromSlash(EngineNamespace), runID)
	var req StartRequest
	data, err := os.ReadFile(filepath.Join(stateDir, "request.json"))
	if err != nil {
		return nil, fmt.Errorf("%s: intake receipt is unavailable", InvalidIntakeConfirmation)
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("engine open: request JSON is invalid: %w", err)
	}
	if req.InstalledTargetIdentity == "" || req.PackageDigest == "" {
		return nil, unsupported("installedTargetIdentity", "non-empty", req.InstalledTargetIdentity)
	}
	if req.DefinitionSource != DefaultDefinitionSource {
		return nil, unsupported("definitionSource", DefaultDefinitionSource, req.DefinitionSource)
	}
	if req.DefinitionDigest != definition.WorkflowDefinitionDigest {
		return nil, unsupported("definitionDigest", definition.WorkflowDefinitionDigest, req.DefinitionDigest)
	}
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		return nil, err
	}
	store, err := persistence.NewStoreWithIdentity(stateDir, persistence.Config{PackageDigest: req.PackageDigest, DefinitionSource: req.DefinitionSource, InstalledTargetIdentity: req.InstalledTargetIdentity})
	if err != nil {
		return nil, err
	}
	if _, err := store.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	} else if errors.Is(err, fs.ErrNotExist) {
		// A completed run may have had its active state cleaned while retaining
		// the immutable terminal summary. During a crash window the summary is
		// still represented by a durable terminal intent; Drive will reconcile
		// cleanup and publish that summary before returning Complete.
		for _, name := range []string{"terminal-summary.json", terminalIntentName} {
			data, summaryErr := os.ReadFile(filepath.Join(stateDir, name))
			if summaryErr != nil {
				continue
			}
			var summary TerminalSummary
			if summaryErr := json.Unmarshal(data, &summary); summaryErr != nil {
				return nil, summaryErr
			}
			if summaryErr := persistence.ValidateEnvelopeWithIdentity(summary.Envelope, req.PackageDigest, req.DefinitionSource, req.InstalledTargetIdentity); summaryErr != nil {
				return nil, summaryErr
			}
			break
		}
		if _, summaryErr := os.Stat(filepath.Join(stateDir, "terminal-summary.json")); summaryErr != nil {
			if _, intentErr := os.Stat(filepath.Join(stateDir, terminalIntentName)); intentErr != nil {
				return nil, err
			}
		}
	}
	eng, err := protocol.New(store, protocol.Config{Definition: cd, Registry: definition.Registry(), Capacity: 16}, nil)
	if err != nil {
		return nil, err
	}
	env := Envelope{Writer: persistence.Writer, StateSchemaVersion: encoder.StateSchemaVersion, WorkflowDefinitionVersion: definition.WorkflowDefinitionVersion, DefinitionSource: req.DefinitionSource, DefinitionDigest: req.DefinitionDigest, PackageDigest: req.PackageDigest, InstalledTargetIdentity: req.InstalledTargetIdentity}
	return &Facade{root: root, runID: runID, stateDir: stateDir, metadata: filepath.Join(stateDir, "request.json"), store: store, engine: eng, envelope: env, definition: cd, capacity: 16}, nil
}

// Drive performs Observe → Decide → SelectIssued.  Lightweight runs record
// the intake receipt on the first call, settle deterministic steps, and retain
// an immutable Complete summary.
func (f *Facade) Drive() (Run, error) {
	if f == nil || f.engine == nil {
		return Run{}, fmt.Errorf("engine drive: nil façade")
	}
	// Recovery is a write-side reconciliation step: it may create/remove the
	// protocol lock and orphan temp files.  Keep it out of Open/Show/Status/Next
	// so those read-only paths cannot mutate directory metadata.
	if err := f.recoverTerminalIntent(); err != nil {
		return Run{}, err
	}
	if _, statErr := os.Stat(filepath.Join(f.stateDir, "state.json")); statErr == nil {
		if _, recoverErr := f.store.Recover(); recoverErr != nil {
			return Run{}, recoverErr
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return Run{}, statErr
	}
	snap, err := f.engine.Load()
	if err != nil {
		if !isMissingEngineState(err) {
			return Run{}, err
		}
		if summary, sumErr := f.readSummary(); sumErr == nil {
			return runFromSummary(summary), nil
		}
		return Run{}, err
	}
	if snap.State.IntakeReceipt == nil {
		confirmation := snap.State.IntakeConfirmationReceipt
		if confirmation == nil {
			return Run{}, fmt.Errorf("%s: start confirmation is missing", InvalidIntakeConfirmation)
		}
		digest, err := IntakeDigest(*confirmation)
		if err != nil {
			return Run{}, err
		}
		if _, err := f.engine.RecordIntakeReceipt(protocol.IntakeReceipt{Confirmation: *confirmation, IntakeDigest: digest}, emptyFingerprint()); err != nil {
			return Run{}, err
		}
	}
	var plan *decision.Plan
	if snap.State.Route == "lightweight" {
		if _, err := f.engine.CompleteAll(emptyFingerprint()); err != nil {
			return Run{}, err
		}
	} else {
		plan, _, err = f.engine.Drive(emptyFingerprint())
		if err != nil {
			return Run{}, err
		}
	}
	run, err := f.project()
	if err != nil {
		return Run{}, err
	}
	if run.Status == "COMPLETE" {
		if run.Route == "lightweight" {
			if err := f.finalizeTerminal(run); err != nil {
				return Run{}, err
			}
		} else if err := f.writeSummaryValue(summaryForRun(run)); err != nil {
			return Run{}, err
		}
	}
	_ = plan
	return run, nil
}

// Submit accepts one typed protocol event and immediately computes the next
// boundary.  The event itself remains the only external write channel.
func (f *Facade) Submit(event protocol.Event) (protocol.Acceptance, Run, error) {
	if f == nil || f.engine == nil {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("engine submit: nil façade")
	}
	acceptance, err := f.engine.Submit(event, emptyFingerprint())
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	run, err := f.project()
	return acceptance, run, err
}

func (f *Facade) Next() (decision.NextResult, error) {
	run, err := f.project()
	if err != nil {
		if !isMissingEngineState(err) {
			return decision.NextResult{}, err
		}
		if summary, sumErr := f.readSummary(); sumErr == nil {
			return summary.Next, nil
		}
		return decision.NextResult{}, err
	}
	return run.Next, nil
}

func (f *Facade) Show() (Run, error)   { return f.projectOrSummary() }
func (f *Facade) Status() (Run, error) { return f.projectOrSummary() }

func (f *Facade) projectOrSummary() (Run, error) {
	run, err := f.project()
	if err == nil {
		return run, nil
	}
	if !isMissingEngineState(err) {
		return Run{}, err
	}
	summary, summaryErr := f.readSummary()
	if summaryErr != nil {
		return Run{}, err
	}
	run = runFromSummary(summary)
	run.SummaryPath = filepath.Join(f.stateDir, "terminal-summary.json")
	return run, nil
}

func isMissingEngineState(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	var rejected *protocol.RejectedError
	return errors.As(err, &rejected) && rejected.Code == protocol.CodeNotInitialized
}

func (f *Facade) project() (Run, error) {
	snap, err := f.engine.Load()
	if err != nil {
		return Run{}, err
	}
	plan, _, err := f.engine.Plan()
	if err != nil {
		return Run{}, err
	}
	status := "ACTIVE"
	if snap.State.Phase == runtime.PhaseTerminal || plan.Next.Kind == decision.KindComplete {
		status = "COMPLETE"
	}
	if status == "COMPLETE" {
		if _, intentErr := os.Stat(terminalIntentPath(f.stateDir)); intentErr == nil {
			// A terminal intent means cleanup is in progress. Keep the read
			// projection non-terminal until Drive reconciles the intent and the
			// immutable summary is committed.
			status = "FINALIZING_CLEANUP"
			plan.Next = decision.NextResult{Kind: decision.KindWait, Wait: &decision.WaitPayload{Reason: decision.WaitEngineInternal}}
		} else if !errors.Is(intentErr, fs.ErrNotExist) {
			return Run{}, intentErr
		}
	}
	run := Run{RunID: f.runID, Runtime: RuntimeEngine, Route: snap.State.Route, Status: status, Revision: snap.Revision, Envelope: f.envelope, Next: plan.Next, SummaryPath: filepath.Join(f.stateDir, "terminal-summary.json"), Unverified: snap.State.Route == "lightweight"}
	run.IntakeConfirmationReceipt = snap.State.IntakeConfirmationReceipt
	run.IntakeReceipt = snap.State.IntakeReceipt
	if plan.Next.Kind == decision.KindAsk {
		run.AvailableActions = []string{"submit"}
	}
	return run, nil
}

func terminalSummaryPath(stateDir string) string {
	return filepath.Join(stateDir, "terminal-summary.json")
}

func terminalIntentPath(stateDir string) string {
	return filepath.Join(stateDir, terminalIntentName)
}

func summaryForRun(run Run) TerminalSummary {
	return TerminalSummary{RunID: run.RunID, Runtime: run.Runtime, Route: run.Route, Status: "COMPLETE", Revision: run.Revision, Envelope: run.Envelope, IntakeReceipt: run.IntakeReceipt, Next: run.Next, Unverified: run.Unverified, CompletedAt: time.Now().UTC().Format(time.RFC3339Nano)}
}

func (f *Facade) writeSummaryValue(summary TerminalSummary) error {
	path := terminalSummaryPath(f.stateDir)
	if data, err := os.ReadFile(path); err == nil {
		var existing TerminalSummary
		if decodeErr := json.Unmarshal(data, &existing); decodeErr != nil {
			return fmt.Errorf("engine terminal summary is invalid: %w", decodeErr)
		}
		// A terminal summary is immutable. Repeated drive/replay calls return
		// the already committed bytes and never refresh CompletedAt.
		if existing.RunID == summary.RunID && existing.Status == "COMPLETE" && existing.Revision == summary.Revision && existing.Envelope == summary.Envelope && existing.Unverified == summary.Unverified {
			return nil
		}
		return fmt.Errorf("engine terminal summary conflicts with completed run")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return writeJSONAtomic(path, summary)
}

func (f *Facade) readTerminalIntent() (TerminalSummary, error) {
	data, err := os.ReadFile(terminalIntentPath(f.stateDir))
	if err != nil {
		return TerminalSummary{}, err
	}
	var summary TerminalSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return TerminalSummary{}, err
	}
	if summary.Status != "COMPLETE" || summary.RunID != f.runID {
		return TerminalSummary{}, fmt.Errorf("engine terminal intent is not a complete summary")
	}
	if err := persistence.ValidateEnvelopeWithIdentity(summary.Envelope, f.envelope.PackageDigest, f.envelope.DefinitionSource, f.envelope.InstalledTargetIdentity); err != nil {
		return TerminalSummary{}, err
	}
	return summary, nil
}

// recoverTerminalIntent finishes a terminal cleanup whose process response
// was lost. It is called only by Drive, keeping Show/Status/Next read-only.
func (f *Facade) recoverTerminalIntent() error {
	summary, err := f.readTerminalIntent()
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return f.commitTerminalCleanup(summary)
}

func (f *Facade) commitTerminalCleanup(summary TerminalSummary) error {
	if err := f.store.CleanupTerminal(); err != nil {
		return err
	}
	if err := f.writeSummaryValue(summary); err != nil {
		return err
	}
	if err := os.Remove(terminalIntentPath(f.stateDir)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (f *Facade) finalizeTerminal(run Run) error {
	summary, err := f.readTerminalIntent()
	if errors.Is(err, fs.ErrNotExist) {
		summary = summaryForRun(run)
		err = writeJSONAtomic(terminalIntentPath(f.stateDir), summary)
	}
	if err != nil {
		return err
	}
	return f.commitTerminalCleanup(summary)
}

func (f *Facade) readSummary() (TerminalSummary, error) {
	data, err := os.ReadFile(filepath.Join(f.stateDir, "terminal-summary.json"))
	if err != nil {
		return TerminalSummary{}, err
	}
	var summary TerminalSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return TerminalSummary{}, err
	}
	if err := persistence.ValidateEnvelopeWithIdentity(summary.Envelope, f.envelope.PackageDigest, f.envelope.DefinitionSource, f.envelope.InstalledTargetIdentity); err != nil {
		return TerminalSummary{}, err
	}
	return summary, nil
}

func runFromSummary(summary TerminalSummary) Run {
	return Run{RunID: summary.RunID, Runtime: summary.Runtime, Route: summary.Route, Status: summary.Status, Revision: summary.Revision, Envelope: summary.Envelope, IntakeReceipt: summary.IntakeReceipt, Next: summary.Next, Unverified: summary.Unverified, SummaryPath: ""}
}

// Diagnose is deliberately raw and read-only.  It reports malformed/old
// envelopes without opening the normal writer.
type DiagnosticReport struct {
	Path             string         `json:"path"`
	JSONReadable     bool           `json:"jsonReadable"`
	DetectedVersions map[string]any `json:"detectedVersions,omitempty"`
	Supported        Envelope       `json:"supported"`
	Integrity        string         `json:"integrity"`
	Recommendation   string         `json:"recommendation,omitempty"`
	Summary          map[string]any `json:"summary,omitempty"`
}

func Diagnose(path string, packageDigest, targetIdentity string) (DiagnosticReport, error) {
	// When invoked with only a run path, recover the owning identity from the
	// immutable start request. This remains read-only and lets diagnose report a
	// non-empty-but-mismatched package/target binding, not just missing fields.
	if strings.TrimSpace(packageDigest) == "" || strings.TrimSpace(targetIdentity) == "" {
		if requestData, requestErr := os.ReadFile(filepath.Join(filepath.Dir(path), "request.json")); requestErr == nil {
			var request StartRequest
			if json.Unmarshal(requestData, &request) == nil {
				if strings.TrimSpace(packageDigest) == "" {
					packageDigest = request.PackageDigest
				}
				if strings.TrimSpace(targetIdentity) == "" {
					targetIdentity = request.InstalledTargetIdentity
				}
			}
		}
	}
	report := DiagnosticReport{Path: filepath.Clean(path), Integrity: "unknown", Supported: Envelope{Writer: persistence.Writer, StateSchemaVersion: encoder.StateSchemaVersion, WorkflowDefinitionVersion: definition.WorkflowDefinitionVersion, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, PackageDigest: packageDigest, InstalledTargetIdentity: targetIdentity}}
	data, err := os.ReadFile(path)
	if err != nil {
		return report, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		report.Recommendation = "jsonReadable:false; rebuild with the owning writer"
		return report, nil
	}
	report.JSONReadable = true
	report.Integrity = "readable"
	report.DetectedVersions = map[string]any{}
	observedRaw := raw
	if nested, ok := raw["envelope"].(map[string]any); ok {
		observedRaw = nested
		report.Summary = raw
	}
	for _, key := range []string{"writer", "stateSchemaVersion", "workflowDefinitionVersion", "definitionSource", "definitionDigest", "packageDigest", "installedTargetIdentity"} {
		if v, ok := observedRaw[key]; ok {
			report.DetectedVersions[key] = v
		}
	}
	var observed Envelope
	if value, ok := raw["envelope"].(map[string]any); ok {
		observed = decodeEnvelope(value)
	} else {
		observed = decodeEnvelope(raw)
	}
	if strings.TrimSpace(packageDigest) != "" && strings.TrimSpace(targetIdentity) != "" {
		if err := persistence.ValidateEnvelopeWithIdentity(observed, packageDigest, report.Supported.DefinitionSource, targetIdentity); err != nil {
			report.Integrity = "unsupported"
			report.Recommendation = err.Error() + "; rebuild with the owning writer"
		}
	} else {
		for _, field := range []string{observed.Writer, observed.StateSchemaVersion, observed.WorkflowDefinitionVersion, observed.DefinitionSource, observed.DefinitionDigest, observed.PackageDigest, observed.InstalledTargetIdentity} {
			if strings.TrimSpace(field) == "" {
				report.Integrity = "unsupported"
				report.Recommendation = persistence.UnsupportedRunVersionCode + ": state envelope is incomplete; rebuild with the owning writer"
				break
			}
		}
	}
	return report, nil
}

func decodeEnvelope(raw map[string]any) Envelope {
	str := func(k string) string { v, _ := raw[k].(string); return v }
	return Envelope{Writer: str("writer"), StateSchemaVersion: str("stateSchemaVersion"), WorkflowDefinitionVersion: str("workflowDefinitionVersion"), DefinitionSource: str("definitionSource"), DefinitionDigest: str("definitionDigest"), PackageDigest: str("packageDigest"), InstalledTargetIdentity: str("installedTargetIdentity")}
}

func IntakeDigest(receipt IntakeConfirmationReceipt) (string, error) {
	receipt = canonicalizeIntakeReceipt(receipt)
	// Timestamps and the digest field are transport metadata, not intake
	// bindings. Excluding them keeps the digest stable when a receipt is
	// replayed and makes ConfirmedAt/ExpiresAt non-authoritative as required.
	canonical := struct {
		Source              string           `json:"source"`
		Authority           IntakeAuthority  `json:"authority"`
		Transport           IntakeTransport  `json:"transport"`
		RequirementSource   string           `json:"requirementSource"`
		RequirementRevision string           `json:"requirementRevision"`
		Artifacts           []IntakeArtifact `json:"artifacts"`
		SolutionRevision    string           `json:"solutionRevision"`
		SolutionDigest      string           `json:"solutionDigest"`
	}{receipt.Source, receipt.Authority, receipt.Transport, receipt.RequirementSource, receipt.RequirementRevision, receipt.Artifacts, receipt.SolutionRevision, receipt.SolutionDigest}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateConfirmation(receipt IntakeConfirmationReceipt, definitionSource string) error {
	if receipt.Source != DefaultIntakeSource || receipt.Authority != DefaultIntakeAuthority || receipt.Transport != DefaultIntakeTransport {
		return fmt.Errorf("%s: source, authority or transport is not the fixed stable launcher binding", InvalidIntakeConfirmation)
	}
	if strings.TrimSpace(receipt.RequirementSource) == "" || strings.TrimSpace(receipt.RequirementRevision) == "" || strings.TrimSpace(receipt.SolutionRevision) == "" || strings.TrimSpace(receipt.SolutionDigest) == "" {
		return fmt.Errorf("%s: requirement and solution bindings are required", InvalidIntakeConfirmation)
	}
	_ = definitionSource // requirement source is a caller-owned artifact path.
	seen := map[string]bool{}
	if len(receipt.Artifacts) == 0 {
		return fmt.Errorf("%s: complete artifact set is required", InvalidIntakeConfirmation)
	}
	for _, artifact := range receipt.Artifacts {
		if strings.TrimSpace(artifact.Path) == "" || strings.TrimSpace(artifact.Revision) == "" {
			return fmt.Errorf("%s: artifact path and revision are required", InvalidIntakeConfirmation)
		}
		cleanPath := filepath.ToSlash(filepath.Clean(artifact.Path))
		if filepath.IsAbs(filepath.FromSlash(cleanPath)) || cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
			return fmt.Errorf("%s: artifact path %q must be relative to the candidate project", InvalidIntakeConfirmation, artifact.Path)
		}
		if seen[cleanPath] {
			return fmt.Errorf("%s: duplicate artifact path %q", InvalidIntakeConfirmation, artifact.Path)
		}
		seen[cleanPath] = true
	}
	// Receipt freshness is bound to the canonical requirement/solution fields
	// and their digest.  No wall-clock expiry is imposed because the confirmed
	// revision itself is the stable acceptance boundary.
	return nil
}

// validateArtifactBindings recomputes the injected artifact revisions using
// the same deterministic content hash used by the stable intake writer. It is
// intentionally scoped to normal repository files beneath ArtifactRoot;
// callers that construct an in-memory protocol fixture may omit ArtifactRoot.
func validateArtifactBindings(root string, receipt IntakeConfirmationReceipt) error {
	rootAbs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return fmt.Errorf("%s: resolve artifact root: %w", InvalidIntakeConfirmation, err)
	}
	solutionDigest := ""
	for _, artifact := range receipt.Artifacts {
		path := filepath.FromSlash(artifact.Path)
		if !filepath.IsAbs(path) {
			path = filepath.Join(rootAbs, path)
		}
		path = filepath.Clean(path)
		rel, relErr := filepath.Rel(rootAbs, path)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s: artifact path %q escapes candidate root", InvalidIntakeConfirmation, artifact.Path)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("%s: artifact %s is unavailable: %w", InvalidIntakeConfirmation, artifact.Path, readErr)
		}
		normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
		sum := sha256.Sum256([]byte(normalized))
		observed := hex.EncodeToString(sum[:])
		if observed != artifact.Revision {
			return fmt.Errorf("%s: artifact %s revision mismatch (receipt %s, current %s)", InvalidIntakeConfirmation, artifact.Path, artifact.Revision, observed)
		}
		if artifact.Revision == receipt.SolutionRevision {
			solutionDigest = "sha256:" + observed
		}
	}
	requirementPath := filepath.Clean(receipt.RequirementSource)
	if filepath.IsAbs(requirementPath) {
		if rel, relErr := filepath.Rel(rootAbs, requirementPath); relErr == nil {
			requirementPath = rel
		}
	}
	requirementPath = filepath.ToSlash(requirementPath)
	matched := false
	solutionMatched := false
	for _, artifact := range receipt.Artifacts {
		if filepath.ToSlash(filepath.Clean(artifact.Path)) == requirementPath {
			matched = true
			if artifact.Revision != receipt.RequirementRevision {
				return fmt.Errorf("%s: requirement revision does not match artifact %s", InvalidIntakeConfirmation, artifact.Path)
			}
		}
		if artifact.Revision == receipt.SolutionRevision {
			solutionMatched = true
		}
	}
	if !matched {
		return fmt.Errorf("%s: requirement source %q is not in the complete artifact set", InvalidIntakeConfirmation, receipt.RequirementSource)
	}
	if !solutionMatched {
		return fmt.Errorf("%s: solution revision is not in the complete artifact set", InvalidIntakeConfirmation)
	}
	if solutionDigest == "" || solutionDigest != receipt.SolutionDigest {
		return fmt.Errorf("%s: solution digest does not match the normalized solution artifact", InvalidIntakeConfirmation)
	}
	return nil
}

// canonicalizeIntakeReceipt returns the exact persisted representation for a
// receipt. Relative paths use slash separators and clean components, and the
// complete artifact set is sorted by canonical path then revision. Keeping the
// same value in StartRequest, state, intake receipt, digest and terminal
// summary prevents input order from becoming observable state.
func canonicalizeIntakeReceipt(receipt IntakeConfirmationReceipt) IntakeConfirmationReceipt {
	receipt.RequirementSource = filepath.ToSlash(filepath.Clean(strings.TrimSpace(receipt.RequirementSource)))
	receipt.Artifacts = append([]IntakeArtifact(nil), receipt.Artifacts...)
	for i := range receipt.Artifacts {
		receipt.Artifacts[i].Path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(receipt.Artifacts[i].Path)))
	}
	sort.Slice(receipt.Artifacts, func(i, j int) bool {
		if receipt.Artifacts[i].Path != receipt.Artifacts[j].Path {
			return receipt.Artifacts[i].Path < receipt.Artifacts[j].Path
		}
		return receipt.Artifacts[i].Revision < receipt.Artifacts[j].Revision
	})
	return receipt
}

func validRunID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !(r == '-' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func startIntentPath(root, runID string) string {
	return filepath.Join(root, filepath.FromSlash(EngineNamespace), runID+startIntentSuffix)
}

// recoverStartIntent removes only a start directory that is still incomplete
// and is paired with our durable start intent. A directory with a request is a
// completed start and is never deleted; the stale intent is simply cleared.
func recoverStartIntent(root, runID string) error {
	path := startIntentPath(root, runID)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var intent startIntent
	if err := json.Unmarshal(data, &intent); err != nil {
		return fmt.Errorf("engine start intent is invalid: %w", err)
	}
	if intent.RunID != runID {
		return fmt.Errorf("engine start intent run id mismatch")
	}
	stateDir := filepath.Join(root, filepath.FromSlash(EngineNamespace), runID)
	requestPath := filepath.Join(stateDir, "request.json")
	if _, statErr := os.Stat(requestPath); statErr == nil {
		return os.Remove(path)
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return statErr
	}
	if _, statErr := os.Stat(stateDir); statErr == nil {
		if err := os.RemoveAll(stateDir); err != nil {
			return err
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return statErr
	}
	return os.Remove(path)
}

// verifyAdmissionSnapshot closes the admission read/commit gap: the caller's
// admission was resolved before Start, but the registry is rechecked while
// the shared admission lock is held so install/uninstall cannot replace the
// target between resolution and the first engine write.
func verifyAdmissionSnapshot(admission Admission) error {
	data, err := os.ReadFile(filepath.Clean(admission.RegistryPath))
	if err != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: admission registry cannot be read: %w", err)
	}
	var doc struct {
		Records []struct {
			ID              string `json:"id"`
			Status          string `json:"status"`
			PackageDigest   string `json:"packageDigest"`
			InstalledDigest string `json:"installedDigest"`
			Generation      uint64 `json:"generation"`
			Lease           string `json:"lease"`
			Token           string `json:"token"`
		} `json:"records"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: admission registry is invalid: %w", err)
	}
	for _, record := range doc.Records {
		if record.ID != admission.InstalledTargetIdentity {
			continue
		}
		packageDigest := record.PackageDigest
		if packageDigest == "" {
			packageDigest = record.InstalledDigest
		}
		if !strings.EqualFold(record.Status, "active") || packageDigest != admission.PackageDigest ||
			record.Generation != admission.Generation || record.Lease != admission.Lease || record.Token != admission.Token {
			return fmt.Errorf("UNREGISTERED_INSTALL: candidate admission changed before start")
		}
		return nil
	}
	return fmt.Errorf("UNREGISTERED_INSTALL: candidate admission record %q is no longer active", admission.InstalledTargetIdentity)
}

func emptyFingerprint() string {
	digest, _ := (decision.Observation{Facts: []decision.Fact{}}).Digest()
	return digest
}

func unsupported(field, want, got string) error {
	return &persistence.UnsupportedRunVersionError{Field: field, Expected: want, Observed: got}
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".engine-*-.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
