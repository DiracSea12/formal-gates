package validate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	engineRuntime "formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/prompts"
)

type PortableCanaryOptions struct{ Root string }

const (
	canaryDeliveryContents  = "delivery\n"
	canaryBlackboxProcedure = "git show HEAD:delivery.txt"
	canaryBlackboxOracle    = `stdout equals "delivery"`
	canaryWhiteboxMarker    = "func TestWhiteboxDirectBehavior(t *testing.T) {}"
	canaryWhiteboxOracle    = "The exact approved Go test runs and passes"
)

type CanaryCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}
type PortableCanaryReport struct {
	SchemaVersion  int                     `json:"schemaVersion"`
	Root           string                  `json:"root"`
	VCSIdentity    string                  `json:"vcsIdentity,omitempty"`
	PackageDigest  string                  `json:"packageDigest,omitempty"`
	CanonicalPaths map[string]string       `json:"canonicalPaths,omitempty"`
	DigestBinding  map[string]string       `json:"digestBinding,omitempty"`
	Installed      []CanaryInstallIdentity `json:"installed,omitempty"`
	Checks         []CanaryCheck           `json:"checks"`
}

type CanaryInstallIdentity struct {
	Host             string            `json:"host"`
	Target           string            `json:"target"`
	ReleaseRoot      string            `json:"releaseRoot,omitempty"`
	VCSIdentity      string            `json:"vcsIdentity"`
	PackageDigest    string            `json:"packageDigest"`
	InstalledDigest  string            `json:"installedDigest"`
	CanonicalPaths   map[string]string `json:"canonicalPaths"`
	Disjoint         map[string]string `json:"disjoint"`
	Registry         string            `json:"registry,omitempty"`
	RegistryRecordID string            `json:"registryRecordId,omitempty"`
	RegistryEpoch    uint64            `json:"registryEpoch,omitempty"`
}

type InstallFaultMatrixOptions struct {
	Root    string
	Fixture string
}

type InstallFaultMatrixReport = PortableCanaryReport

// InstallFaultMatrix is the public, deterministic fixture for the native
// install transaction. Each named boundary injects one failure through the
// same process-wide hook used by the real installer, then verifies that the
// target, release root, launcher and transaction journal have no partial
// committed state. The fixture is read-only with respect to the source root.
func InstallFaultMatrix(options InstallFaultMatrixOptions) (InstallFaultMatrixReport, Result) {
	root := lifecycle.CleanRoot(options.Root)
	report := InstallFaultMatrixReport{SchemaVersion: 1, Root: slash(absPath(root))}
	if receipt, receiptErr := PackageReceipt(root); receiptErr == nil {
		report.PackageDigest = receipt.Digest
		report.VCSIdentity = sourceVCSIdentity(root, receipt.Digest)
		report.CanonicalPaths = map[string]string{"root": canonicalRegistryPath(root)}
		report.DigestBinding = map[string]string{"package": receipt.Digest}
	}
	var result Result
	add := func(name string, err error) {
		status, detail := "PASS", ""
		if err != nil {
			status, detail = "FAIL", err.Error()
			result.add(name, detail)
		}
		report.Checks = append(report.Checks, CanaryCheck{Name: name, Status: status, Detail: detail})
	}
	phases := []string{"journal-boundary", "intent", "registry", "copy-component:runtime", "prepared", "switched", "post-switch-smoke", "pointer", "hook", "managed-rule", "registry-commit", "copy-component:prompts", "copy-component:gates", "verify-stage:installed-target", "verify-stage:manifest", "verify-stage:realpath", "verify-stage:digest"}
	if fixture := strings.TrimSpace(options.Fixture); fixture != "" {
		phases = []string{canonicalInstallFaultPhase(fixture)}
	}
	for _, phase := range phases {
		phase := phase
		fixture, err := os.MkdirTemp("", "formal-gates-fault-matrix-")
		if err != nil {
			add("fault-"+phase, err)
			continue
		}
		project := filepath.Join(fixture, "project")
		release := filepath.Join(fixture, "release")
		launcher := filepath.Join(fixture, "stable", nativeBinaryName())
		registry := filepath.Join(fixture, "registry.json")
		faultPrior, hadFault := os.LookupEnv("FORMAL_GATES_INSTALL_FAULT")
		_ = os.Setenv("FORMAL_GATES_INSTALL_FAULT", phase)
		_, installErr := Install(InstallOptions{Source: root, Host: "codex", Scope: "project", Project: project, ReleaseRoot: release, BinaryTarget: launcher, RegistryPath: registry, Force: true})
		if hadFault {
			_ = os.Setenv("FORMAL_GATES_INSTALL_FAULT", faultPrior)
		} else {
			_ = os.Unsetenv("FORMAL_GATES_INSTALL_FAULT")
		}
		checkErr := installErr
		if checkErr == nil {
			checkErr = fmt.Errorf("fault %s unexpectedly succeeded", phase)
		} else if !strings.Contains(checkErr.Error(), "deterministic install fault injected") {
			// A fixture that fails before the requested boundary is still a
			// failure of the matrix: the named injection must be observable.
			checkErr = fmt.Errorf("fault %s did not reach its injection boundary: %w", phase, checkErr)
		} else {
			checkErr = nil
			target := filepath.Join(project, ".codex", "skills", "formal-gates")
			if exists(target) || exists(release) || exists(launcher) {
				checkErr = fmt.Errorf("fault %s left committed target/release/launcher state", phase)
			} else if matches, globErr := filepath.Glob(registry + ".transaction.json*"); globErr != nil {
				checkErr = globErr
			} else if len(matches) == 0 {
				checkErr = fmt.Errorf("fault %s did not leave an independent recovery receipt", phase)
			} else if strings.HasPrefix(phase, "copy-component:") {
				data, readErr := os.ReadFile(registry + ".transaction.json.failure.json")
				var receipt installRecoveryReceipt
				if readErr != nil {
					checkErr = readErr
				} else if json.Unmarshal(data, &receipt) != nil {
					checkErr = fmt.Errorf("fault %s left an unreadable recovery receipt", phase)
				} else if !receipt.Prepared || !receipt.PartialCopy || len(receipt.CopiedComponents) == 0 {
					checkErr = fmt.Errorf("fault %s did not record prepared partial-copy evidence: %+v", phase, receipt)
				}
			}
		}
		add("fault-"+phase, checkErr)
		_ = os.RemoveAll(fixture)
	}
	return report, result
}

// withoutHostEnv clears the host identity environment keys declared by the
// lifecycle registry and returns a restore function.
func withoutHostEnv() func() {
	prior := map[string]string{}
	for _, key := range lifecycle.ProviderEnvironmentKeys() {
		prior[key] = os.Getenv(key)
		os.Setenv(key, "")
	}
	return func() {
		for key, value := range prior {
			os.Setenv(key, value)
		}
	}
}

func PortableCanary(options PortableCanaryOptions) (PortableCanaryReport, Result) {
	restore := withoutHostEnv()
	defer restore()
	root := lifecycle.CleanRoot(options.Root)
	report := PortableCanaryReport{SchemaVersion: 1, Root: slash(absPath(root))}
	if receipt, receiptErr := PackageReceipt(root); receiptErr == nil {
		report.PackageDigest = receipt.Digest
		report.DigestBinding = map[string]string{"package": receipt.Digest}
	}
	report.VCSIdentity = sourceVCSIdentity(root, report.PackageDigest)
	report.CanonicalPaths = map[string]string{"root": canonicalRegistryPath(root)}
	var result Result
	add := func(name string, err error) {
		status, detail := "PASS", ""
		if err != nil {
			status, detail = "FAIL", err.Error()
			result.add(name, detail)
		}
		report.Checks = append(report.Checks, CanaryCheck{Name: name, Status: status, Detail: detail})
	}
	packageResult := Package(root)
	if packageResult.OK() {
		add("package-validate", nil)
	} else {
		add("package-validate", fmt.Errorf("%s", resultSummary(packageResult)))
	}
	_, err := prompts.LoadPromptCatalog(root)
	add("prompt-catalog", err)
	// An installed candidate invokes this command with a private marker.  It
	// must exercise the same regression path without recursively starting a
	// second install-canary from inside itself.
	if os.Getenv("FORMAL_GATES_INSTALLED_CANARY") == "1" {
		if err == nil {
			packageRoot := strings.TrimSpace(os.Getenv("FORMAL_GATES_CANARY_PACKAGE_ROOT"))
			workflowRoot := strings.TrimSpace(os.Getenv("FORMAL_GATES_CANARY_WORKFLOW_ROOT"))
			registry := strings.TrimSpace(os.Getenv("FORMAL_GATES_CANARY_REGISTRY"))
			recordID := strings.TrimSpace(os.Getenv("FORMAL_GATES_CANARY_RECORD"))
			if packageRoot == "" || workflowRoot == "" || registry == "" || recordID == "" {
				add("installed-binary-regression", fmt.Errorf("installed canary is missing its package/workflow/registry binding"))
			} else if installedCatalog, loadErr := prompts.LoadPromptCatalog(packageRoot); loadErr != nil {
				add("installed-binary-regression", loadErr)
			} else {
				add("installed-binary-regression", runQuickE2ECanaryAt(packageRoot, installedCatalog, workflowRoot, registry, recordID))
			}
		}
		return report, result
	}
	tempRoot, err := os.MkdirTemp("", "formal-gates-install-canary-")
	if err != nil {
		add("install", err)
	} else {
		defer os.RemoveAll(tempRoot)
		identities := addInstallChecks(root, tempRoot, func(name string, ok bool, detail string) {
			if ok {
				add(name, nil)
			} else {
				add(name, fmt.Errorf("%s", detail))
			}
		})
		report.Installed = append(report.Installed, identities...)
	}
	// 写阻断 canary：在有活动正式 run 的仓库根上，验证主线程阻断、development-worker
	// 放行、审查类代理阻断、登记文档豁免、无 run 放行的判定矩阵。
	writeBlockRoot, err := os.MkdirTemp("", "formal-gates-writeblock-canary-")
	if err != nil {
		add("write-block-hook", err)
	} else {
		defer os.RemoveAll(writeBlockRoot)
		add("write-block-hook", runWriteBlockCanary(writeBlockRoot))
	}
	return report, result
}

// runQuickE2ECanaryAt drives synthetic worker and user evidence through the
// installed engine. Native resources, candidate creation and promotion remain
// owned by the engine; QA observations execute the committed fixture artifacts.
func runQuickE2ECanaryAt(packageRoot string, catalog prompts.PromptCatalog, root, registry, recordID string) error {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(registry) == "" || strings.TrimSpace(recordID) == "" {
		return fmt.Errorf("portable workflow requires an installed root, registry and record")
	}
	admission, err := ResolveEngineAdmissionByRecord(recordID, registry)
	if err != nil {
		return err
	}
	bound, err := ResolveEngineAdmission(root, packageRoot, registry)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(admission, bound) {
		return fmt.Errorf("portable workflow installation does not match its root and package")
	}
	document := "# Canary requirement\n\n## 需求点\n\n### REQ-001：Confirmed behavior\n\n#### 要求\n\nThe installed workflow must complete its verified route.\n\n#### 验收条件\n\n- AC-001：The installed workflow reaches Seal through the public runtime.\n\n#### 来源\n\nPortable canary contract.\n"
	if err := os.WriteFile(filepath.Join(root, "requirement.md"), []byte(document), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module portablefixture\n\ngo 1.22\n"), 0600); err != nil {
		return err
	}
	if err := initializeCanaryGit(root); err != nil {
		return err
	}
	base, err := (vcs.ExecRunner{}).Run(root, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(document)))
	receipt := protocol.IntakeConfirmationReceipt{Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport, RequirementSource: "requirement.md", RequirementRevision: revision, Artifacts: []protocol.IntakeArtifact{{Path: "requirement.md", Revision: revision}}, SolutionRevision: revision, SolutionDigest: "sha256:" + revision}
	f, run, err := facade.Start(facade.StartOptions{RuntimeFiles: admission.RuntimeFiles, Root: root, ArtifactRoot: root, PackageRoot: packageRoot, Capacity: 8, Request: facade.StartRequest{RunID: "canary", VCS: "git", BaseSnapshot: strings.TrimSpace(base), Route: "regular", Provider: admission.Provider, DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: receipt}, Admission: &facade.Admission{RegistryPath: admission.RegistryPath, Target: admission.Target, Provider: admission.Provider, PackageDigest: admission.PackageDigest, InstalledTargetIdentity: admission.RecordID, Generation: admission.Generation, Lease: admission.Lease, Token: admission.Token}})
	if err != nil {
		return err
	}
	seq := 0
	eventID := func() protocol.EventID { seq++; return protocol.EventID(fmt.Sprintf("canary-event-%d", seq)) }
	submit := func(e protocol.Event) error {
		if e.ID == "" {
			e.ID = eventID()
		}
		accepted, next, err := f.Submit(e)
		if err != nil {
			return err
		}
		run = next
		if accepted.Status != "ACCEPTED" && accepted.Status != "STAGED" {
			return fmt.Errorf("portable evidence %s: %s (%+v; rejected=%+v)", e.Kind, accepted.Status, accepted, run.RejectedResults)
		}
		return nil
	}
	control := func(kind protocol.ControlKind, choice string, payload protocol.ReviewDecisionPayload) error {
		id := eventID()
		var e protocol.Event
		var err error
		if kind == protocol.ControlSelectRoute {
			e, err = protocol.NewRouteRequestEvent(id, payload.Route.Gates, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
		} else {
			e, err = protocol.NewRequestEvent(id, kind, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
		}
		if err != nil {
			return err
		}
		if err = submit(e); err != nil {
			return err
		}
		for _, ask := range run.PendingAsks {
			if ask.RequestID == string(id) {
				e, err = protocol.NewReviewDecideEvent(eventID(), protocol.RequestID(id), ask.Token, protocol.AskOptionID(choice), payload)
				if err != nil {
					return err
				}
				return submit(e)
			}
		}
		return fmt.Errorf("portable control ask missing: %s", kind)
	}
	gates := []string{"blackbox", "whitebox"}
	for _, gate := range catalog.Gates {
		if gate.ID != prompts.MergeGateID {
			gates = append(gates, gate.ID)
		}
	}
	topology, routeConfirmed := false, false
	checked := map[string]bool{}
	for turns := 0; turns < 100; turns++ {
		run, err = f.Drive()
		if err != nil {
			return err
		}
		if _, err = f.Next(); err != nil {
			return err
		}
		if run.Status == "COMPLETE" {
			for _, role := range []string{"engine.development.batch.v4", "design:BLACKBOX", "design:WHITEBOX", "review:BLACKBOX", "review:WHITEBOX", "execution:BLACKBOX", "execution:WHITEBOX"} {
				if !checked[role] {
					return fmt.Errorf("portable workflow omitted %s", role)
				}
			}
			for _, gate := range gates[2:] {
				if !checked["gate:"+gate] {
					return fmt.Errorf("portable workflow omitted gate %s", gate)
				}
			}
			return nil
		}
		if slices.ContainsFunc(run.Review.Actions, func(a facade.ReviewActionFace) bool {
			return a.Action == string(review.StartReadiness) && a.EffectiveStatus == "PASS"
		}) && !topology {
			if err = control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one bounded delivery fixture"}}); err != nil {
				return err
			}
			topology = true
			continue
		}
		if topology && !routeConfirmed {
			route := protocol.RouteDecision{Mode: "full", Gates: gates}
			preview, err := f.DevelopmentPlan(route)
			if err != nil {
				return err
			}
			route.DevelopmentPlan = &preview.Confirmation
			if err = control(protocol.ControlSelectRoute, "full", protocol.ReviewDecisionPayload{Route: &route}); err != nil {
				return err
			}
			routeConfirmed = true
			continue
		}
		ids := make([]string, 0, len(run.WorkerMaterials))
		for id := range run.WorkerMaterials {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if len(ids) == 0 {
			checking := false
			for _, intent := range run.AdapterIntents {
				checking = checking || intent.Adapter != nil && intent.Adapter.Operation == "op.check"
			}
			if checking && (run.Next.Kind == decision.KindWait || run.Next.Kind == decision.KindHostAction) {
				time.Sleep(50 * time.Millisecond)
				turns--
				continue
			}
			return fmt.Errorf("portable workflow stalled: next=%+v status=%s asks=%+v", run.Next, run.Status, run.PendingAsks)
		}
		for _, id := range ids {
			ref := run.WorkerMaterials[id]
			raw, err := os.ReadFile(ref.Path)
			if err != nil {
				return err
			}
			var material worker.Material
			if err = json.Unmarshal(raw, &material); err != nil {
				return err
			}
			var locationFacts []worker.LocationFact
			payload, err := portableWorkerResult(material.Input, checked, &locationFacts)
			if err != nil {
				return err
			}
			e, err := protocol.NewSpawnReceiptEvent(eventID(), id, admission.Provider, id, protocol.SpawnStatusSpawned)
			if err != nil {
				return err
			}
			if err = submit(e); err != nil {
				return err
			}
			hooks, err := lifecycle.HookDefinitions(admission.Provider)
			if err != nil {
				return err
			}
			lifecyclePayload, err := portableLifecyclePayload(root, material.Input)
			if err != nil {
				return err
			}
			for _, hook := range hooks {
				if _, err = lifecycle.Capture(root, admission.Provider, hook.Event, lifecyclePayload); err != nil {
					return err
				}
			}
			raw, err = json.Marshal(payload)
			if err != nil {
				return err
			}
			var paths []string
			if !material.ReadOnly && material.Input.Validation != nil && material.Input.Validation.Workspace != nil {
				resource := material.Input.Validation.Workspace
				paths, err = vcs.NewNative(root, material.Input.VCS, nil).At(resource.Path, resource.Handle).ChangedPaths()
				if err != nil {
					return err
				}
				if paths == nil {
					paths = []string{}
				}
			}
			event, err := worker.DecodeResultWithFacts(material.Input, eventID(), raw, fmt.Sprintf("sha256:%x", sha256.Sum256(raw)), paths, locationFacts)
			if err != nil {
				return err
			}
			if err = submit(event); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("portable workflow exceeded its bounded drive fixture")
}

// portableLifecyclePayload binds the local canary's host events to one issued
// action. ZCode Agent hooks identify that action by the native tool-call field.
func portableLifecyclePayload(root string, input protocol.WorkerInput) ([]byte, error) {
	id := input.Pending.ActionID
	payload := map[string]string{"agent_id": id, "parentConversationId": input.RunID, "generationId": id, "subagentType": input.Handler, "task": id, "cwd": root}
	if input.Provider == host.ZCode {
		payload["tool_name"], payload["tool_use_id"] = "Agent", id
	}
	return json.Marshal(payload)
}

func portableDevelopmentPlan() *worker.Granularity {
	return &worker.Granularity{Reason: "One data-artifact fixture with exact byte build/test checks and no inferred language.", VerificationTier: "FINAL", Batches: []worker.Batch{{Subtasks: []worker.Subtask{{Description: "Write the delivery marker"}}, Deliverable: "Committed delivery marker", PublicEntry: canaryBlackboxProcedure, NewArtifacts: []string{"delivery.txt"}, Interfaces: "delivery.txt contains delivery", StateBoundary: "Native candidate owns committed files", Risk: "Missing or incorrect marker", DoD: []worker.Criterion{{Description: "Exact delivery marker content"}}, Checks: []worker.Check{{Purpose: protocol.CheckFullBuild, Command: "cp delivery.txt built-delivery.txt"}, {Purpose: protocol.CheckFullTests, Command: "printf 'delivery\\n' | cmp - built-delivery.txt"}}, Rollback: "Discard the isolated workspace", Recovery: "Resume from the issued workspace", Handoff: "Recorded marker bytes", ContextCost: "One file and one acceptance condition"}}}
}

func portableWorkerResult(input protocol.WorkerInput, checked map[string]bool, facts *[]worker.LocationFact) (any, error) {
	var payload any
	if input.Review != nil {
		evidence := map[string]string{
			"binding": "The synthetic fixture uses the installed role and confirmed input.", "completeness": "The fixture requirement declares its delivery marker and full verified route.", "evidence": "This deterministic fixture does not claim model or semantic reviewer validation.", "locations": "The fixture identifies its public runtime and committed delivery artifacts.", "normal-entry": "The installed runtime and registered native workspace are used.", "requirement-match": "The plan delivers the declared marker and preserves route checks.", "scope": "The fixture remains within its confirmed requirement.", "severity": "No findings are claimed by this synthetic fixture.",
		}
		result := &worker.Review{Findings: []worker.Finding{}}
		for _, key := range review.RequiredVerificationChecks() {
			result.Verification = append(result.Verification, evidence[key])
		}
		if input.Review.Action == review.StartReadiness {
			result.GranularityReview = portableDevelopmentPlan()
		}
		return result, nil
	}
	if input.Validation == nil {
		return payload, fmt.Errorf("portable worker lacks issued typed input")
	}
	in := input.Validation
	workspace := ""
	if in.Workspace != nil {
		workspace = in.Workspace.Path
	}
	switch in.Handler {
	case "engine.development.batch.v4":
		if err := os.WriteFile(filepath.Join(workspace, "delivery.txt"), []byte(canaryDeliveryContents), 0600); err != nil {
			return payload, err
		}
		actual, err := os.ReadFile(filepath.Join(workspace, "delivery.txt"))
		if err != nil || string(actual) != canaryDeliveryContents {
			return payload, fmt.Errorf("delivery fixture bytes differ: %q (%v)", actual, err)
		}
		payload = &worker.Development{Completion: &worker.Completion{DoD: []string{"delivery.txt bytes equal delivery newline"}, Handoff: "delivery.txt is ready for engine candidate registration"}, Summary: "Wrote the exact portable delivery fixture"}
		checked[in.Handler] = true
	case "engine.qa.design.v3":
		mode := in.Manifest.Binding.ReviewKind
		c := worker.DesignedCase{Scenario: "SUCCESS", PublicEntry: canaryBlackboxProcedure, Preconditions: "Committed candidate", Steps: canaryBlackboxProcedure, Oracle: canaryBlackboxOracle, Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"git", "show", "HEAD:delivery.txt"}}}}
		missing := worker.DesignedCase{Scenario: "FAILURE", PublicEntry: "git show HEAD:absent-marker.txt", Preconditions: "Candidate has no absent-marker.txt", Steps: "git show HEAD:absent-marker.txt", Oracle: "git rejects the absent committed artifact", Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"git", "show", "HEAD:absent-marker.txt"}}}}
		if mode == coverage.ReviewWhitebox {
			c.TestFile, c.TestSymbol = 1, 1
			c.PublicEntry, c.Preconditions, c.Steps = "", "", ""
			c.Operations = nil
			c.Oracle = canaryWhiteboxOracle
			missing.TestFile, missing.TestSymbol = 1, 2
			missing.PublicEntry, missing.Preconditions, missing.Steps = "", "", ""
			missing.Operations = nil
			missing.Oracle = canaryWhiteboxOracle
			if err := os.WriteFile(filepath.Join(workspace, "whitebox_delivered_test.go"), []byte(strings.Replace(whiteboxDeliveredTestCode, `import "testing"`, "import (\n\"testing\"\n\"os\"\n)", 1)+"\nfunc TestWhiteboxMissingArtifact(t *testing.T) { if _, err := os.Stat(\"absent-marker.txt\"); !os.IsNotExist(err) { t.Fatalf(\"missing artifact accepted: %v\", err) } }\n"), 0600); err != nil {
				return payload, err
			}
			body, err := os.ReadFile(filepath.Join(workspace, "whitebox_delivered_test.go"))
			if err != nil {
				return payload, err
			}
			*facts = append(*facts, worker.LocationFact{Kind: "read", Paths: []string{"whitebox_delivered_test.go"}, Content: body})
			files := []worker.Location{{Read: len(*facts)}}
			c.Files, missing.Files = &files, &files
		}
		manifest := worker.DesignManifest{Cases: []worker.DesignedCase{c, missing}, Coverage: [][]int{}}
		for range in.Manifest.Points {
			manifest.Coverage = append(manifest.Coverage, []int{1, 2})
		}
		payload = &worker.Design{DesignManifest: manifest}
		checked["design:"+string(mode)] = true
	case "engine.qa.review.rationale.v2":
		m := in.Manifest
		r := worker.CaseReview{SourceDecisions: []*worker.Decision{}, PointDecisions: []*worker.Decision{}, CaseDecisions: []*worker.Decision{}}
		for range m.Sources {
			r.SourceDecisions = append(r.SourceDecisions, &worker.Decision{Status: coverage.StatusPass, Reason: "Synthetic fixture covers the declared installed delivery source."})
		}
		for range m.Points {
			r.PointDecisions = append(r.PointDecisions, &worker.Decision{Status: coverage.StatusPass, Reason: "Synthetic fixture relates committed/missing-artifact oracles to this condition."})
		}
		for range m.Cases {
			r.CaseDecisions = append(r.CaseDecisions, &worker.Decision{Status: coverage.StatusPass, Reason: "Synthetic fixture binds its approved operation or Go test and oracle."})
		}
		payload = &r
		checked["review:"+string(m.Binding.ReviewKind)] = true
	case "engine.qa.execution.v2":
		result := &worker.ExecutionJudgments{Execution: worker.Execution{Records: []*worker.Observation{}}}
		payload = result
		seen := []string{}
		for _, fact := range in.ExecutionFacts {
			if !slices.Contains(in.RequiredCases, fact.CaseID) || !slices.Contains(in.PendingCases, fact.CaseID) || slices.Contains(seen, fact.CaseID) {
				continue
			}
			seen = append(seen, fact.CaseID)
			index := slices.IndexFunc(in.Manifest.Cases, func(c coverage.AcceptanceCase) bool { return c.CaseID == fact.CaseID })
			if index < 0 {
				return payload, fmt.Errorf("portable execution lacks its approved case: %s", fact.CaseID)
			}
			observation, err := portableComparison(in.Manifest.Cases[index], fact)
			if err != nil {
				return payload, err
			}
			result.Records = append(result.Records, &observation)
		}

		checked["execution:"+string(in.Manifest.Binding.ReviewKind)] = true
	case "engine.gate.v2":
		payload = &worker.Gate{Findings: []worker.Finding{}}
		if in.Gate == nil {
			return payload, fmt.Errorf("portable gate lacks issued input")
		}
		checked["gate:"+in.Gate.Gate] = true
	default:
		return payload, fmt.Errorf("unsupported portable fixture worker %s", in.Handler)
	}
	return payload, nil
}

func portableComparison(c coverage.AcceptanceCase, fact protocol.ExecutionFact) (worker.Observation, error) {
	if len(fact.Operations) != 1 {
		return worker.Observation{}, fmt.Errorf("portable case requires its one approved operation: %s", c.CaseID)
	}
	evidence := fact.Operations[0].Evidence
	passed := false
	comparison := ""
	if c.Mode == coverage.CaseWhitebox {
		_, testName, _ := strings.Cut(c.TestRef, "::")
		passed = evidence.ExitCode == 0 && evidence.GoTestName == testName && evidence.GoResult == "pass"
		comparison = fmt.Sprintf("expected exact Go test %s to pass; actual test=%s result=%s exit=%d", testName, evidence.GoTestName, evidence.GoResult, evidence.ExitCode)
	} else if strings.Contains(c.Steps, "absent-marker.txt") {
		passed = evidence.ExitCode != 0 && evidence.StdoutText == "" && strings.Contains(evidence.StderrText, "absent-marker.txt")
		comparison = fmt.Sprintf("expected Git to reject absent-marker.txt with no stdout; actual exit=%d stdout=%q stderr=%q", evidence.ExitCode, evidence.StdoutText, evidence.StderrText)
	} else {
		passed = evidence.ExitCode == 0 && evidence.StdoutText == canaryDeliveryContents
		comparison = fmt.Sprintf("expected stdout=%q and exit=0; actual stdout=%q exit=%d; equal=%t", canaryDeliveryContents, evidence.StdoutText, evidence.ExitCode, passed)
	}
	result := coverage.ExecutedFail
	if passed {
		result = coverage.ExecutedPass
	}
	return worker.Observation{Result: result, Comparison: comparison}, nil
}

func initializeCanaryGit(root string) error {
	runner := vcs.ExecRunner{}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "formal-gates@example.invalid"}, {"config", "user.name", "Formal Gates Canary"}} {
		if _, err := runner.Run(root, "git", args...); err != nil {
			return err
		}
	}
	// Runtime evidence is excluded from the committed delivery candidate.
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".gates/\n"), 0o600); err != nil {
		return err
	}
	return commitCanaryGit(root, "base")
}

func commitCanaryGit(root, message string) error {
	runner := vcs.ExecRunner{}
	if _, err := runner.Run(root, "git", "add", "--all"); err != nil {
		return err
	}
	_, err := runner.Run(root, "git", "commit", "-m", message)
	return err
}

func addInstallChecks(root, tempRoot string, addCheck func(string, bool, string)) []CanaryInstallIdentity {
	var identities []CanaryInstallIdentity
	canaryHome := filepath.Join(tempRoot, "stable-home")
	canaryLocalAppData := filepath.Join(tempRoot, "stable-localappdata")
	registry := filepath.Join(canaryHome, ".formal-gates", "registry.json")
	launcher := filepath.Join(canaryHome, ".local", "bin", nativeBinaryName())
	if runtime.GOOS == "windows" {
		launcher = filepath.Join(canaryLocalAppData, "formal-gates", "bin", nativeBinaryName())
	}
	stableEnv := canaryEnvironment(canaryHome, canaryLocalAppData)
	for _, hostName := range host.InstallableNames() {
		tc := struct{ name, host string }{name: "install-" + hostName + "-native-runtime", host: hostName}
		project := filepath.Join(tempRoot, tc.name)
		if err := os.MkdirAll(project, 0o700); err != nil {
			addCheck(tc.name, false, err.Error())
			continue
		}
		releaseRoot := filepath.Join(project, "stable-release")
		report, err := Install(InstallOptions{Source: root, Host: tc.host, Scope: "project", Project: project, ReleaseRoot: releaseRoot, BinaryTarget: launcher, RegistryPath: registry, Force: true})
		if err != nil {
			addCheck(tc.name, false, err.Error())
			continue
		}
		for _, target := range report.Targets {
			identities = append(identities, CanaryInstallIdentity{Host: target.Host, Target: target.TargetPath, ReleaseRoot: target.ReleaseRoot, VCSIdentity: target.VCSIdentity, PackageDigest: target.PackageDigest, InstalledDigest: target.InstalledDigest, CanonicalPaths: target.CanonicalPaths, Disjoint: target.Disjoint, Registry: report.Registry, RegistryRecordID: target.RegistryRecordID, RegistryEpoch: report.RegistryEpoch})
			installedBinary := filepath.Join(target.TargetPath, "bin", nativeBinaryName())
			if output, smokeErr := exec.Command(installedBinary, "--version").CombinedOutput(); smokeErr != nil {
				addCheck(tc.name+"-installed-binary-smoke", false, fmt.Sprintf("%s: %v (%s)", installedBinary, smokeErr, strings.TrimSpace(string(output))))
				continue
			}
		}
		// The source checkout is only the install input.  Re-run the package
		// regression and portable canary through the executable copied to the
		// stable launcher path so a passing in-process source test cannot stand
		// in for candidate evidence.
		installedTarget := filepath.FromSlash(report.Targets[0].TargetPath)
		regressionCommand := exec.Command(launcher, "package", "validate", "--root", installedTarget)
		regressionCommand.Env = stableEnv
		if output, regressionErr := regressionCommand.CombinedOutput(); regressionErr != nil {
			addCheck(tc.name+"-installed-binary-regression", false, fmt.Sprintf("%v (%s)", regressionErr, strings.TrimSpace(string(output))))
			continue
		}
		addCheck(tc.name+"-installed-binary-regression", true, "stable launcher validated the installed target package")
		// The documented first-start boundary: a normal install commits registry
		// records without a bootstrap receipt, and workflow start rejects a
		// registry that was never bootstrapped. Bootstrap the same registry from
		// the same source before the installed-binary canary starts its run.
		if _, bootstrapErr := Install(InstallOptions{Source: root, Host: tc.host, Scope: "project", Project: project, ReleaseRoot: releaseRoot, BinaryTarget: launcher, RegistryPath: registry, Bootstrap: true, Force: true}); bootstrapErr != nil {
			addCheck(tc.name+"-bootstrap", false, bootstrapErr.Error())
			continue
		}
		if !isFile(registry + ".bootstrap.json") {
			addCheck(tc.name+"-bootstrap", false, "bootstrap did not persist the registry bootstrap receipt")
			continue
		}
		addCheck(tc.name+"-bootstrap", true, "registry bootstrap receipt committed for the installed target")
		registryDoc, registryErr := LoadRegistry(registry)
		if registryErr != nil {
			addCheck(tc.name+"-installed-binary-canary", false, registryErr.Error())
			continue
		}
		canaryRecord := ""
		if len(report.Targets) > 0 {
			for _, record := range registryDoc.Records {
				if canonicalRegistryPath(record.Target) == canonicalRegistryPath(report.Targets[0].TargetPath) {
					canaryRecord = record.ID
					break
				}
			}
		}
		if canaryRecord == "" {
			addCheck(tc.name+"-installed-binary-canary", false, "installed target has no exact registry record")
			continue
		}
		canaryCommand := exec.Command(launcher, "canary", "portable", "--root", installedTarget, "--format", "json")
		canaryCommand.Env = append(stableEnv, "FORMAL_GATES_INSTALLED_CANARY=1", "FORMAL_GATES_CANARY_PACKAGE_ROOT="+report.Targets[0].TargetPath, "FORMAL_GATES_CANARY_WORKFLOW_ROOT="+project, "FORMAL_GATES_CANARY_REGISTRY="+registry, "FORMAL_GATES_CANARY_RECORD="+canaryRecord)
		if output, canaryErr := canaryCommand.CombinedOutput(); canaryErr != nil {
			addCheck(tc.name+"-installed-binary-canary", false, fmt.Sprintf("%v (%s)", canaryErr, strings.TrimSpace(string(output))))
			continue
		}
		addCheck(tc.name+"-installed-binary-canary", true, "stable launcher completed the installed target canary")
		candidateExecutable, executableErr := os.Executable()
		candidateBase := filepath.Base(filepath.Clean(candidateExecutable))
		if executableErr == nil && candidateBase == nativeBinaryName() && canonicalRegistryPath(candidateExecutable) != canonicalRegistryPath(launcher) {
			candidateProject := filepath.Join(project, "candidate-must-not-install")
			candidateInstall := exec.Command(candidateExecutable, "install", "--source", releaseRoot, "--host", "claude", "--scope", "project", "--project", candidateProject, "--binary-target", candidateExecutable, "--force")
			candidateInstall.Env = stableEnv
			if output, candidateErr := candidateInstall.CombinedOutput(); candidateErr == nil {
				addCheck(tc.name+"-candidate-install-fenced", false, "candidate binary unexpectedly installed runtime")
				continue
			} else if exists(filepath.Join(candidateProject, ".claude", "skills", "formal-gates")) {
				addCheck(tc.name+"-candidate-install-fenced", false, fmt.Sprintf("candidate rejection left a target: %s", strings.TrimSpace(string(output))))
				continue
			}
			addCheck(tc.name+"-candidate-install-fenced", true, "candidate binary could not claim the stable launcher")

			candidateRunID := "candidate-must-not-start"
			requestBytes, readErr := os.ReadFile(filepath.Join(project, facade.EngineNamespace, "canary", "request.json"))
			var request facade.StartRequest
			if readErr != nil {
				addCheck(tc.name+"-candidate-workflow-fenced", false, readErr.Error())
				continue
			}
			if readErr = json.Unmarshal(requestBytes, &request); readErr != nil {
				addCheck(tc.name+"-candidate-workflow-fenced", false, readErr.Error())
				continue
			}
			confirmation, marshalErr := json.Marshal(request.IntakeConfirmationReceipt)
			if marshalErr != nil {
				addCheck(tc.name+"-candidate-workflow-fenced", false, marshalErr.Error())
				continue
			}
			confirmationPath := filepath.Join(tempRoot, tc.name+".intake.json")
			if writeErr := os.WriteFile(confirmationPath, confirmation, 0600); writeErr != nil {
				addCheck(tc.name+"-candidate-workflow-fenced", false, writeErr.Error())
				continue
			}
			candidateStart := exec.Command(candidateExecutable, "workflow", "start", "--root", project, "--package-root", report.Targets[0].TargetPath, "--run-id", candidateRunID, "--intake-receipt", confirmationPath, "--route", "lightweight")
			candidateStart.Env = stableEnv
			if output, candidateErr := candidateStart.CombinedOutput(); candidateErr == nil {
				addCheck(tc.name+"-candidate-workflow-fenced", false, "candidate binary unexpectedly created workflow state")
				continue
			} else if !strings.Contains(string(output), facade.UnregisteredInstall) {
				addCheck(tc.name+"-candidate-workflow-fenced", false, fmt.Sprintf("candidate failed outside installation admission: %s", strings.TrimSpace(string(output))))
				continue
			} else if exists(filepath.Join(project, facade.EngineNamespace, candidateRunID)) {
				addCheck(tc.name+"-candidate-workflow-fenced", false, fmt.Sprintf("candidate rejection left a run directory: %s", strings.TrimSpace(string(output))))
				continue
			}
			addCheck(tc.name+"-candidate-workflow-fenced", true, "candidate binary could not drive the registered target")
		}
		faultProject := filepath.Join(project, "fault-recovery")
		faultCommand := exec.Command(launcher, "install", "--source", releaseRoot, "--host", "claude", "--scope", "project", "--project", faultProject, "--binary-target", launcher, "--force")
		faultCommand.Env = append(stableEnv, "FORMAL_GATES_INSTALL_FAULT=post-switch-smoke")
		if output, faultErr := faultCommand.CombinedOutput(); faultErr == nil {
			addCheck(tc.name+"-installed-binary-fault-recovery", false, "post-switch smoke fault unexpectedly succeeded")
			continue
		} else if exists(filepath.Join(faultProject, ".claude", "skills", "formal-gates")) {
			addCheck(tc.name+"-installed-binary-fault-recovery", false, fmt.Sprintf("fault left a candidate target: %s", strings.TrimSpace(string(output))))
			continue
		}
		addCheck(tc.name+"-installed-binary-fault-recovery", true, "installed binary recovered the stable namespace")
		if report.Registry == "" || !isFile(filepath.FromSlash(report.Registry)) {
			addCheck(tc.name+"-registry-admission", false, "install did not persist a registry admission bridge receipt")
			continue
		}
		if detail := installedScriptRuntimeDetail(report); detail != "" {
			addCheck(tc.name, false, detail)
			continue
		}
		addCheck(tc.name, true, "installed runtime uses native commands")
		rule, err := LoadManagedRule(root)
		if err != nil {
			addCheck(tc.name+"-host-instructions", false, err.Error())
			continue
		}
		if detail := installedManagedRuleDetail(report, rule); detail != "" {
			addCheck(tc.name+"-host-instructions", false, detail)
			continue
		}
		uninstalled, err := Uninstall(UninstallOptions{Host: tc.host, Scope: "project", Project: project, RegistryPath: registry})
		if err != nil {
			addCheck(tc.name+"-uninstall", false, err.Error())
			continue
		}
		if detail := uninstalledInstallDetail(uninstalled); detail != "" {
			addCheck(tc.name+"-uninstall", false, detail)
			continue
		}
		addCheck(tc.name+"-uninstall", true, "runtime, hooks, and host instruction blocks cleaned")
	}
	return identities
}

func canaryEnvironment(home, localAppData string) []string {
	keys := map[string]bool{"HOME": true, "USERPROFILE": true, "LOCALAPPDATA": true}
	environment := make([]string, 0, len(os.Environ())+3)
	for _, item := range os.Environ() {
		name := item
		if index := strings.IndexByte(item, '='); index >= 0 {
			name = item[:index]
		}
		if !keys[name] {
			environment = append(environment, item)
		}
	}
	environment = append(environment, "HOME="+home, "USERPROFILE="+home, "LOCALAPPDATA="+localAppData)
	return environment
}

func installedScriptRuntimeDetail(report InstallReport) string {
	for _, target := range report.Targets {
		files, err := installPackageFiles(target.TargetPath, true)
		if err != nil {
			return err.Error()
		}
		declared := map[string]bool{}
		for _, file := range files {
			declared[file.Path] = true
		}
		var found []string
		err = filepath.WalkDir(target.TargetPath, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if isScriptRuntimeExtension(entry.Name()) {
				rel, err := filepath.Rel(target.TargetPath, path)
				if err != nil {
					return err
				}
				if !declared[filepath.ToSlash(rel)] {
					found = append(found, slash(path))
				}
			}
			return nil
		})
		if err != nil {
			return err.Error()
		}
		if len(found) > 0 {
			return "installed script runtime files: " + strings.Join(found, ", ")
		}
		if strings.TrimSpace(target.HookConfig) != "" {
			text, err := readText(target.HookConfig)
			if err != nil {
				return err.Error()
			}
			lower := strings.ToLower(text)
			for _, marker := range []string{".ps1", "powershell", "pwsh", "python", "node", "bash"} {
				if strings.Contains(lower, marker) {
					return "hook config contains script runtime marker " + marker
				}
			}
		}
	}
	return ""
}

func installedManagedRuleDetail(report InstallReport, latest string) string {
	for _, target := range report.Targets {
		if strings.TrimSpace(target.ManagedRulePath) == "" {
			continue
		}
		text, err := readText(target.ManagedRulePath)
		if err != nil {
			return err.Error()
		}
		if strings.Count(text, latest) != 1 {
			return fmt.Sprintf("host instruction rule count for %s is %d", target.ManagedRulePath, strings.Count(text, latest))
		}
		if strings.Count(text, hostInstructionsStartMarker) != 1 || strings.Count(text, hostInstructionsEndMarker) != 1 {
			return "host instruction markers did not converge in " + target.ManagedRulePath
		}
	}
	return ""
}

func uninstalledInstallDetail(report UninstallReport) string {
	for _, target := range report.Targets {
		if exists(target.TargetPath) {
			return "formal-gates runtime remains at " + target.TargetPath
		}
		if target.ManagedRulePath != "" && isFile(target.ManagedRulePath) {
			text, err := readText(target.ManagedRulePath)
			if err != nil {
				return err.Error()
			}
			if strings.Contains(text, hostInstructionsStartMarker) || strings.Contains(text, hostInstructionsEndMarker) {
				return "host instruction markers remain in " + target.ManagedRulePath
			}
		}
		if target.HookConfig != "" && isFile(target.HookConfig) {
			text, err := readText(target.HookConfig)
			if err != nil {
				return err.Error()
			}
			if strings.Contains(strings.ToLower(text), "formal-gates") {
				return "installer-owned hook remains in " + target.HookConfig
			}
		}
	}
	return ""
}
func resultSummary(result Result) string {
	messages := make([]string, 0, len(result.Failures))
	for _, failure := range result.Failures {
		messages = append(messages, failure.Path+": "+failure.Message)
	}
	return strings.Join(messages, "; ")
}

// runWriteBlockCanary exercises the host Hook using a current typed engine
// development fixture: owner/reviewer code writes are blocked, development/QA
// authors and confirmed document edits are allowed, and unrelated roots are allowed.
func runWriteBlockCanary(root string) error {
	// This isolated fixture represents an observed current engine development
	// dispatch. Persistence writes the complete envelope and content digest.
	runDir := filepath.Join(root, ".gates", "engine", "write-block-canary")
	view, err := decision.NewState(authoring.DefinitionVersion(definition.WorkflowDefinitionVersion), engineRuntime.PhaseDevelopmentParallel)
	if err != nil {
		return err
	}
	state := protocol.NewState(*view)
	state.RunID = "write-block-canary"
	state.Route = "regular"
	state.IntakeConfirmationReceipt = &protocol.IntakeConfirmationReceipt{RequirementRevision: "r1", Artifacts: []protocol.IntakeArtifact{{Path: "requirements.md", Revision: "r1"}, {Path: "design.md", Revision: "r2"}}}
	state.DispatchBindings["development"] = protocol.WorkBinding{RequirementRevision: "r1", PlanRequest: "confirmed-plan", BatchID: "delivery"}
	state.RequirementRevisions = []protocol.ConfirmedRevision{{Revision: "r1"}}
	store, err := persistence.NewStore(runDir, persistence.Config{PackageDigest: "sha256:portable-fixture", DefinitionSource: facade.DefaultDefinitionSource, InstalledTargetIdentity: "portable-fixture"})
	if err != nil {
		return err
	}
	if _, err = store.Save(persistence.Transaction{ExpectedFingerprint: "fixture", CollectFingerprint: func() (string, error) { return "fixture", nil }, Content: state}); err != nil {
		return err
	}
	cwdPayload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, root, filepath.Join(root, "internal", "code.go"))
	// 1. 主线程（无 agent 身份）写代码 → 阻断。
	if decision, err := Hook([]byte(cwdPayload)); err != nil || decision.PermissionDecision != "deny" {
		return fmt.Errorf("main-thread code write was not blocked: %#v %v", decision, err)
	}
	// 2. development-worker 写代码 → 放行。
	devPayload := `{"cwd":` + string(mustJSONString(root)) + `,"tool_name":"Write","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(root, "internal", "code.go")) + `"},"agent_type":"development-worker"}`
	if decision, err := Hook([]byte(devPayload)); err != nil || decision.PermissionDecision != "allow" {
		return fmt.Errorf("development-worker write was not allowed: %#v %v", decision, err)
	}
	// 3. qa-design 写测试代码 → 放行。
	qaDesignPayload := `{"cwd":` + string(mustJSONString(root)) + `,"tool_name":"Write","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(root, "internal", "code_test.go")) + `"},"agent_type":"qa-design"}`
	if decision, err := Hook([]byte(qaDesignPayload)); err != nil || decision.PermissionDecision != "allow" {
		return fmt.Errorf("qa-design write was not allowed: %#v %v", decision, err)
	}
	// 4. 审查类代理（product-review）写代码 → 阻断。
	reviewerPayload := `{"cwd":` + string(mustJSONString(root)) + `,"tool_name":"Write","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(root, "internal", "code.go")) + `"},"agent_type":"product-review"}`
	if decision, err := Hook([]byte(reviewerPayload)); err != nil || decision.PermissionDecision != "deny" {
		return fmt.Errorf("reviewer-class write was not blocked: %#v %v", decision, err)
	}
	// 5. 主线程编辑已登记需求文档 → 放行（需求更改流程）。
	docPayload := `{"cwd":` + string(mustJSONString(root)) + `,"tool_name":"Write","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(root, "requirements.md")) + `"}}`
	if decision, err := Hook([]byte(docPayload)); err != nil || decision.PermissionDecision != "allow" {
		return fmt.Errorf("main-agent registered-doc edit was not allowed: %#v %v", decision, err)
	}
	// 6. 无活动 run 的普通仓库：主线程写代码 → 放行（不干扰普通项目）。普通仓库必须放在
	// 活动 run 仓库根之外，否则向上查找会命中 root 下的活动 run。
	plainRoot, err := os.MkdirTemp("", "formal-gates-plain-repo-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(plainRoot)
	plainPayload := `{"cwd":` + string(mustJSONString(plainRoot)) + `,"tool_name":"Write","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(plainRoot, "main.go")) + `"}}`
	if decision, err := Hook([]byte(plainPayload)); err != nil || decision.PermissionDecision != "allow" {
		return fmt.Errorf("write without an active run was blocked: %#v %v", decision, err)
	}
	// 7. cwd 仍在活动 run 仓库内，但目标明确位于仓库外：当前 run 的写墙不得扩张成
	// 全局文件锁，另一个目录/窗口的写入应放行。
	outsidePath := filepath.Join(plainRoot, "outside.go")
	outsidePayload := `{"cwd":` + string(mustJSONString(root)) + `,"tool_name":"Write","tool_input":{"file_path":` + string(mustJSONString(outsidePath)) + `}}`
	if decision, err := Hook([]byte(outsidePayload)); err != nil || decision.PermissionDecision != "allow" {
		return fmt.Errorf("write outside the active run root was blocked: %#v %v", decision, err)
	}
	return nil
}

func mustJSONString(value string) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
