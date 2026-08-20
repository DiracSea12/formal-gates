package validate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"formal-gates/internal/lifecycle"
)

type StartOptions struct {
	Root, PackageRoot, RunID, Flow, RequirementSource, VCS, BaseSnapshot string
	// AdmissionRegistry and AdmissionRecordID explicitly select the stage-0
	// registry bridge. Empty values discover the shared user registry; only Go
	// test executables retain the in-process legacy start path.
	AdmissionRegistry string
	AdmissionRecordID string
	// CurrentSnapshot 显式指定当前快照停在某祖先：默认不传时取原生 HEAD；
	// 传入值必须是原生 HEAD 的祖先或相等，用于接手"开发已提交"的 run 时让 current 停在
	// 开发前、已有开发提交作为待登记快照。
	CurrentSnapshot                       string
	RequirementArtifacts                  []string
	RequirementConfirmed, RetainedOverall bool
	// Split 是 workflow start 的强制拆分声明（需求 4）：yes 或 no。yes 时本 run 必须是
	// 保留总任务实例（RetainedOverall）或切片实例（MasterRunID 引用主实例）；no 时禁止
	// 后续记录 split。缺失声明拒绝启动。轻量路线（Route == "lightweight"）不做拆分决定，
	// 免去本声明。
	Split string
	// MasterRunID 是切片实例启动时声明的保留总任务 master run id（--split yes --master）。
	MasterRunID string
	// Route 是 workflow start 的轻量路线声明：Route == "lightweight" 时本 run 从 start
	// 即走轻量路线——不拆分、不选 QA/门路线、不快照、不做任何验证，只留记录，三步直达
	// （start → 需求登记 → Seal）。轻量 start 免去强制 --split 声明（不接受 --split yes /
	// --retained-overall / --master）。留空走常规受理，full/custom 路线在拆分决定之后确认。
	Route string
}

type FindingInput struct {
	Severity  string
	Message   string
	Locations []string
}

// ReviewItemInput 是 record-action 对 product-review / start-readiness 逐项下发的
// 需求项判定（增量审查）。Key 是 prepare-action --scope 声明的需求项键；Status 为
// PASS | FAIL；Reason 是 FAIL 项必须携带的 finding。
type ReviewItemInput struct{ Key, Status, Reason string }

const (
	carryOriginIndependent  = "INDEPENDENT"
	carryOriginMainShortcut = "MAIN_SHORTCUT"
)

// carryAdoptKey is the reserved Carry map key recording an adopted external VCS
// change: Decision ADOPT with Origin ADOPT, SourceSnapshot the pre-adoption
// snapshot, TargetSnapshot the adopted native head, and Message the main-agent
// reason. Gate ids cannot collide with it because they match promptIDPattern.
const carryAdoptKey = "__adopt__"

const formalFlow = "formal"
const automaticReviewWaveLimit = 3

// QA execution scope source markers: PREPARE for the standalone
// workflow qa-execution-scope command, AUTHORIZE_REPAIR for a scope recorded inline
// by an authorize-repair call at the review-wave limit, and CARRY_FORWARD for a
// scope auto-carried at the limit from a prior user AFFECTED choice without asking
// again.
const (
	scopeSourcePrepare         = "PREPARE"
	scopeSourceAuthorizeRepair = "AUTHORIZE_REPAIR"
	scopeSourceCarryForward    = "CARRY_FORWARD"
)

const (
	developmentPending        = "PENDING"
	developmentPrepared       = "PREPARED"
	developmentRepairPrepared = "REPAIR_PREPARED"
	developmentComplete       = "PASS"
	developmentVerified       = "VERIFIED"
)

// routeModes 是 workflow route 与 start --route 可接受的路线模式集合。lightweight
// 是正式流程内的轻量路线（创建 run 但跳过全部验证、只留记录，三步直达 Seal），在 start
// 时显式声明，route 时经 SetRoute 也可选中零门。
var routeModes = map[string]bool{"lightweight": true, "full": true, "custom": true}

func Start(options StartOptions) (RunState, error) {
	root := lifecycle.CleanRoot(options.Root)
	ownerTranscript, ownerSession := consumeStartOwner(root)
	for name, value := range map[string]string{"flow": options.Flow, "requirement": options.RequirementSource, "VCS": options.VCS} {
		if strings.TrimSpace(value) == "" {
			return RunState{}, fmt.Errorf("%s is required", name)
		}
	}
	if strings.TrimSpace(options.Flow) != formalFlow {
		return RunState{}, fmt.Errorf("flow must be formal")
	}
	if options.RequirementConfirmed {
		return RunState{}, fmt.Errorf("a run cannot start with a pre-confirmed requirement; record Requirements Clarification first")
	}
	// 需求 4：启动时强制显式声明拆分意向，并把"保留总任务实例 vs 切片实例"的映射钉死在
	// 启动声明中，使"忘带 retained-overall"在启动时就暴露，而不是拖到拆分决定才被拒。
	// 轻量路线（--route lightweight）不做拆分决定，免去拆分声明：start 即声明轻量，之后
	// start → 需求登记 → Seal 三步直达，跳过拆分、路线确认、开发快照与全部验证。
	split := strings.ToLower(strings.TrimSpace(options.Split))
	master := strings.TrimSpace(options.MasterRunID)
	route := strings.ToLower(strings.TrimSpace(options.Route))
	if route != "" && route != "lightweight" {
		return RunState{}, fmt.Errorf("--route must be lightweight or empty")
	}
	isLightweight := route == "lightweight"
	if isLightweight {
		if split == "yes" {
			return RunState{}, fmt.Errorf("a lightweight route does not split; --split yes is not valid with --route lightweight")
		}
		if options.RetainedOverall || master != "" {
			return RunState{}, fmt.Errorf("a lightweight route does not retain overall or act as a slice; --retained-overall and --master are not valid with --route lightweight")
		}
	} else {
		switch split {
		case "yes":
			if options.RetainedOverall && master != "" {
				return RunState{}, fmt.Errorf("a run cannot be both a retained-overall instance and a slice instance; use --retained-overall or --master, not both")
			}
			if !options.RetainedOverall && master == "" {
				return RunState{}, fmt.Errorf("workflow start --split yes requires --retained-overall (保留总任务实例) or --master <run-id> (切片实例) to pin the split intent at start")
			}
		case "no":
			if options.RetainedOverall {
				return RunState{}, fmt.Errorf("a retained-overall run exists to integrate split slices, so it must declare --split yes")
			}
			if master != "" {
				return RunState{}, fmt.Errorf("--master is only valid with --split yes")
			}
		case "":
			return RunState{}, fmt.Errorf("workflow start requires an explicit --split yes|no declaration; a run refuses to start without a pinned split intent")
		default:
			return RunState{}, fmt.Errorf("--split must be yes or no")
		}
	}
	if master != "" {
		if !promptIDPattern.MatchString(master) {
			return RunState{}, fmt.Errorf("master run id must match [a-z0-9]+(?:-[a-z0-9]+)*")
		}
		masterState, err := LoadRunState(root, master)
		if err != nil {
			return RunState{}, fmt.Errorf("slice master run %q is not found: %v", master, err)
		}
		if err := requireRetainedSplitMaster(masterState); err != nil {
			return RunState{}, fmt.Errorf("slice master %q is invalid: %w", master, err)
		}
	}
	vcs := strings.ToLower(strings.TrimSpace(options.VCS))
	resolver, err := resolverForVCS(vcs, nil)
	if err != nil {
		return RunState{}, err
	}
	nativeHead, err := resolver.Resolve(root)
	if err != nil {
		return RunState{}, err
	}
	currentSnapshot := nativeHead
	// 显式指定当前快照停在某祖先（默认不传时仍取 HEAD）。传入值必须是原生 HEAD
	// 的祖先或相等；用于接手"开发已提交"的 run 时让 current 停在开发前、已有开发提交作为
	// 待登记快照。
	if supplied := strings.TrimSpace(options.CurrentSnapshot); supplied != "" {
		if err := resolver.Verify(root, supplied); err != nil {
			return RunState{}, err
		}
		if err := resolver.IsAncestorOrEqual(root, supplied, nativeHead); err != nil {
			return RunState{}, fmt.Errorf("current snapshot %s is not an ancestor or equal of the native head: %w", supplied, err)
		}
		currentSnapshot = strings.ToLower(supplied)
	}
	baseSnapshot := currentSnapshot
	if supplied := strings.TrimSpace(options.BaseSnapshot); supplied != "" {
		if err := resolver.Verify(root, supplied); err != nil {
			return RunState{}, err
		}
		if err := resolver.IsAncestorOrEqual(root, supplied, currentSnapshot); err != nil {
			return RunState{}, err
		}
		baseSnapshot = strings.ToLower(supplied)
	}
	catalog, err := LoadPromptCatalog(options.PackageRoot)
	if err != nil {
		return RunState{}, err
	}
	artifacts, err := requirementArtifactSet(root, options.RequirementSource, options.RequirementArtifacts)
	if err != nil {
		return RunState{}, err
	}
	revision := artifactRevision(artifacts, normalizeArtifactPath(root, options.RequirementSource))
	runID := strings.TrimSpace(options.RunID)
	if runID == "" {
		runID, err = newRunID()
		if err != nil {
			return RunState{}, err
		}
	}
	if !promptIDPattern.MatchString(runID) {
		return RunState{}, fmt.Errorf("run id must match [a-z0-9]+(?:-[a-z0-9]+)*")
	}
	if _, err := os.Stat(RunDir(root, runID)); err == nil {
		return RunState{}, fmt.Errorf("run %q already exists", runID)
	} else if !os.IsNotExist(err) {
		return RunState{}, err
	}
	if _, err := os.Stat(RunSummaryPath(root, runID)); err == nil {
		return RunState{}, fmt.Errorf("run %q already has a retained result", runID)
	} else if !os.IsNotExist(err) {
		return RunState{}, err
	}
	registryPath, registryRecordID, discoveryErr := discoverAdmissionBinding(root, options.PackageRoot, options.AdmissionRegistry, options.AdmissionRecordID)
	if discoveryErr != nil {
		return RunState{}, discoveryErr
	}
	var admissionDoc RegistryDocument
	var admissionRecord RegistryRecord
	if registryPath != "" {
		recordID := registryRecordID
		if recordID == "" {
			writeWorkflowAdmissionRejection(registryPath, recordID, root, options.PackageRoot, "admission record id is required when --registry is supplied")
			return RunState{}, fmt.Errorf("admission record id is required when --registry is supplied")
		}
		receipt, err := AdmitRegistry(registryPath, recordID)
		if err != nil {
			writeWorkflowAdmissionRejection(registryPath, recordID, root, options.PackageRoot, err.Error())
			return RunState{}, err
		}
		if !receipt.Accepted {
			writeWorkflowAdmissionReceipt(registryPath, receipt, root, options.PackageRoot, receipt.Reason)
			return RunState{}, fmt.Errorf("%s: workflow state write refused for registry record %q", receipt.Code, recordID)
		}
		if bindingErr := verifyRegistryBinding(registryPath, recordID, root, options.PackageRoot); bindingErr != nil {
			return RunState{}, bindingErr
		}
		admissionDoc, admissionRecord, err = registryAdmissionIdentity(registryPath, recordID)
		if err != nil {
			return RunState{}, err
		}
		if strings.EqualFold(admissionRecord.Scope, "global") && canonicalRegistryPath(admissionRecord.ProjectRoot) != canonicalRegistryPath(root) {
			// Global installation bytes are shared, but workflow state and
			// resources are project-local. Materialize this invocation binding
			// through the registry transaction owner before creating state.
			admissionRecord, admissionDoc, err = bindGlobalInvocationRoot(registryPath, admissionRecord, root)
			if err != nil {
				return RunState{}, err
			}
			registryRecordID = admissionRecord.ID
		}
	}
	if err := os.MkdirAll(filepath.Dir(RunDir(root, runID)), 0o700); err != nil {
		return RunState{}, err
	}
	if err := os.Mkdir(RunDir(root, runID), 0o700); err != nil {
		return RunState{}, fmt.Errorf("cannot create run %q: %w", runID, err)
	}
	if err := workflowLifecycle.Begin(root, runID); err != nil {
		_ = os.RemoveAll(RunDir(root, runID))
		return RunState{}, err
	}
	state := NewRunState(runID, strings.TrimSpace(options.Flow), normalizeArtifactPath(root, options.RequirementSource), revision, vcs, baseSnapshot, currentSnapshot, catalog.BaseRevision, catalog.CatalogRevision, options.RequirementConfirmed, catalog.GateIDs(), artifacts)
	state.PromptHashes = catalogPromptHashes(catalog)
	// Preserve the admission binding in the run envelope so every later
	// SaveRunState call re-admits the same target before writing state.
	state.AdmissionRegistry = registryPath
	state.AdmissionRecordID = registryRecordID
	state.AdmissionRoot = root
	state.AdmissionTarget = options.PackageRoot
	state.AdmissionEpoch = admissionDoc.Epoch
	state.AdmissionGeneration = admissionRecord.Generation
	state.AdmissionLease = admissionRecord.Lease
	state.AdmissionToken = admissionRecord.Token
	state.RetainedOverall = options.RetainedOverall
	state.SplitDeclaration = split
	state.SplitMasterRunID = master
	state.OwnerTranscript = ownerTranscript
	state.OwnerSession = ownerSession
	// 轻量路线在 start 即声明：routeMode 置 lightweight（免拆分决定、免路线确认、
	// 免开发快照直达 Seal），不记录拆分声明。
	if isLightweight {
		state.RouteMode = "lightweight"
		state.SplitDeclaration = ""
	}
	if err := SaveRunState(root, state); err != nil {
		_ = os.RemoveAll(RunDir(root, runID))
		return RunState{}, err
	}
	return state, nil
}

func bindGlobalInvocationRoot(path string, base RegistryRecord, root string) (RegistryRecord, RegistryDocument, error) {
	unlock, err := acquireRegistryLock(path)
	if err != nil {
		return RegistryRecord{}, RegistryDocument{}, err
	}
	defer unlock()
	doc, err := loadRegistryForCommit(path)
	if err != nil {
		return RegistryRecord{}, RegistryDocument{}, err
	}
	projectRoot := canonicalRegistryPath(root)
	derived := base
	identity := sha256.Sum256([]byte(base.ID + "\x00" + projectRoot))
	derived.ID = fmt.Sprintf("%s-project-%x", base.ID, identity[:6])
	derived.ProjectRoot = projectRoot
	derived.StateRoot = canonicalRegistryPath(filepath.Join(projectRoot, ".gates"))
	derived.ResourceRoot = canonicalRegistryPath(filepath.Join(projectRoot, ".formal-gates-resources"))
	derived.CanonicalPaths = map[string]string{
		"target": canonicalRegistryPath(derived.Target), "launcher": canonicalRegistryPath(derived.LauncherPath),
		"projectRoot": derived.ProjectRoot, "stateRoot": derived.StateRoot,
		"resourceRoot": derived.ResourceRoot, "runtimeSibling": canonicalRegistryPath(derived.RuntimeSibling),
	}
	if strings.TrimSpace(derived.HookConfig) != "" {
		derived.CanonicalPaths["hookConfig"] = canonicalRegistryPath(derived.HookConfig)
	}
	if strings.TrimSpace(derived.ReleaseRoot) != "" {
		derived.CanonicalPaths["releaseRoot"] = canonicalRegistryPath(derived.ReleaseRoot)
	}
	if err := os.MkdirAll(derived.ResourceRoot, 0o700); err != nil {
		return RegistryRecord{}, RegistryDocument{}, fmt.Errorf("resource root setup failed: %w", err)
	}
	for _, existing := range doc.Records {
		if existing.ID == derived.ID && strings.EqualFold(existing.Status, "active") && sameRegistryBinding(existing, derived) {
			return existing, doc, nil
		}
	}
	committed, err := commitRegistryRecordsUnlocked(path, doc, []RegistryRecord{derived})
	if err != nil {
		return RegistryRecord{}, RegistryDocument{}, err
	}
	return derived, committed, nil
}

// discoverAdmissionBinding resolves the shared user registry before Start
// creates .gates/tmp.  A present registry is an
// admission boundary: if it cannot account for the installed package, start
// fails with UNREGISTERED_INSTALL instead of silently falling back to a direct
// state writer. In-process test binaries retain the legacy phase-0 state path;
// a shipped binary must always be the stable launcher admitted by a registry.
func discoverAdmissionBinding(root, packageRoot, requestedPath, requestedID string) (string, string, error) {
	registryPath := strings.TrimSpace(requestedPath)
	registryRecordID := strings.TrimSpace(requestedID)
	if registryPath != "" {
		if registryRecordID == "" {
			writeWorkflowAdmissionRejection(registryPath, registryRecordID, root, packageRoot, "admission record id is required when --registry is supplied")
			return "", "", fmt.Errorf("admission record id is required when --registry is supplied")
		}
		return filepath.Clean(registryPath), registryRecordID, nil
	}
	// Unit and integration tests execute the workflow API in-process rather
	// than through an installed launcher.  Their temporary roots must remain
	// isolated from any real user registry that happens to exist on the host;
	// otherwise an unrelated global record turns every fixture into an
	// UNREGISTERED_INSTALL failure.  Admission-specific tests pass an explicit
	// registry/record above, so this exemption does not weaken the production
	// launcher boundary.
	if executable, executableErr := os.Executable(); executableErr == nil {
		base := filepath.Base(filepath.Clean(executable))
		if strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe") {
			return "", "", nil
		}
	}
	candidates := []string{}
	if home, err := installHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".formal-gates", "registry.json"))
	}
	for _, candidate := range candidates {
		if !isFile(candidate) {
			continue
		}
		record, err := findRegistryRecordForTarget(candidate, packageRoot)
		if err != nil {
			return "", "", fmt.Errorf("UNREGISTERED_INSTALL: registry admission bridge cannot bind %s: %w", packageRoot, err)
		}
		return filepath.Clean(candidate), record.ID, nil
	}
	for _, candidate := range candidates {
		writeWorkflowAdmissionRejection(candidate, registryRecordID, root, packageRoot, "stable launcher registry is missing")
	}
	executable, executableErr := os.Executable()
	if executableErr != nil {
		return "", "", fmt.Errorf("UNREGISTERED_INSTALL: cannot resolve invoking launcher: %w", executableErr)
	}
	base := filepath.Base(filepath.Clean(executable))
	if !strings.HasSuffix(base, ".test") && !strings.HasSuffix(base, ".test.exe") {
		return "", "", fmt.Errorf("UNREGISTERED_INSTALL: stable launcher has no registry record for package %s", packageRoot)
	}
	return "", "", nil
}

func findRegistryRecordForTarget(path, packageRoot string) (RegistryRecord, error) {
	doc, err := LoadRegistry(path)
	if err != nil {
		writeWorkflowAdmissionRejection(path, "", "", packageRoot, fmt.Sprintf("registry cannot be read: %v", err))
		return RegistryRecord{}, err
	}
	want, err := filepath.Abs(packageRoot)
	if err != nil {
		return RegistryRecord{}, err
	}
	for _, record := range doc.Records {
		if strings.EqualFold(record.Status, "active") && validAdmissionRegistryRecord(record) && canonicalRegistryPath(record.Target) == canonicalRegistryPath(want) && canonicalRegistryPath(record.CanonicalPaths["target"]) == canonicalRegistryPath(want) {
			if err := verifyRegisteredTargetIdentity(record); err != nil {
				return RegistryRecord{}, err
			}
			return record, nil
		}
	}
	writeWorkflowAdmissionRejection(path, "", "", packageRoot, fmt.Sprintf("no registry record matches package root %s", packageRoot))
	return RegistryRecord{}, fmt.Errorf("no registry record matches package root %s", packageRoot)
}

func writeWorkflowAdmissionRejection(path, recordID, root, packageRoot, reason string) {
	target := strings.TrimSpace(packageRoot)
	if target == "" {
		target = root
	}
	canonicalTarget := ""
	if target != "" {
		canonicalTarget = canonicalRegistryPath(target)
	}
	canonicalRoot := ""
	if strings.TrimSpace(root) != "" {
		canonicalRoot = canonicalRegistryPath(root)
	}
	paths := map[string]string{}
	if canonicalTarget != "" {
		paths["target"] = canonicalTarget
	}
	if canonicalRoot != "" {
		paths["projectRoot"] = canonicalRoot
	}
	_ = writeAdmissionReceipt(path, AdmissionReceipt{
		Code: "UNREGISTERED_INSTALL", Accepted: false, Status: "disabled",
		RecordID: recordID, Registry: filepath.Clean(path), Target: canonicalTarget,
		Scope: "unknown", CanonicalPaths: paths, Reason: reason, CreatedAt: nowReceiptTime(),
	})
}

func writeWorkflowAdmissionReceipt(path string, receipt AdmissionReceipt, root, packageRoot, reason string) {
	target := strings.TrimSpace(receipt.Target)
	if target == "" {
		target = strings.TrimSpace(packageRoot)
	}
	if target == "" {
		target = strings.TrimSpace(root)
	}
	if target != "" {
		receipt.Target = canonicalRegistryPath(target)
	}
	if strings.TrimSpace(receipt.Registry) == "" {
		receipt.Registry = filepath.Clean(path)
	}
	if strings.TrimSpace(receipt.Scope) == "" {
		receipt.Scope = "unknown"
	}
	if receipt.CanonicalPaths == nil {
		receipt.CanonicalPaths = map[string]string{}
	}
	if receipt.Target != "" {
		receipt.CanonicalPaths["target"] = canonicalRegistryPath(receipt.Target)
	}
	if strings.TrimSpace(root) != "" {
		receipt.CanonicalPaths["projectRoot"] = canonicalRegistryPath(root)
	}
	receipt.Code = "UNREGISTERED_INSTALL"
	receipt.Accepted = false
	receipt.Status = "disabled"
	receipt.Reason = reason
	receipt.CreatedAt = nowReceiptTime()
	_ = writeAdmissionReceipt(path, receipt)
}

func registryAdmissionIdentity(path, recordID string) (RegistryDocument, RegistryRecord, error) {
	doc, err := LoadRegistry(path)
	if err != nil {
		return RegistryDocument{}, RegistryRecord{}, err
	}
	for _, record := range doc.Records {
		if record.ID == recordID {
			if !strings.EqualFold(record.Status, "active") || !validAdmissionRegistryRecord(record) {
				return RegistryDocument{}, RegistryRecord{}, fmt.Errorf("UNREGISTERED_INSTALL: registry record %q is inactive or incomplete", recordID)
			}
			return doc, record, nil
		}
	}
	return RegistryDocument{}, RegistryRecord{}, fmt.Errorf("UNREGISTERED_INSTALL: registry record %q is missing", recordID)
}

func verifyRegistryBinding(registryPath, recordID, root, packageRoot string) error {
	doc, err := LoadRegistry(registryPath)
	if err != nil {
		return err
	}
	for _, record := range doc.Records {
		if record.ID != recordID {
			continue
		}
		if !strings.EqualFold(record.Status, "active") || !validAdmissionRegistryRecord(record) {
			return fmt.Errorf("UNREGISTERED_INSTALL: registry record is inactive or incomplete")
		}
		if err := verifyRegisteredTargetIdentity(record); err != nil {
			return err
		}
		canonicalRoot, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		canonicalPackage, err := filepath.Abs(packageRoot)
		if err != nil {
			return err
		}
		// A project-scope record is bound to one repository root.  A global
		// installation is intentionally reusable across projects, so its
		// projectRoot records the host-level installation namespace rather than
		// the arbitrary repository that is invoking the stable driver.
		if record.Scope == "project" {
			if expected := canonicalRegistryPath(record.CanonicalPaths["projectRoot"]); expected != "." && expected != canonicalRegistryPath(canonicalRoot) {
				return fmt.Errorf("UNREGISTERED_INSTALL: registry project root does not match workflow root")
			}
		}
		if canonicalRegistryPath(record.Target) != canonicalRegistryPath(canonicalPackage) || canonicalRegistryPath(record.CanonicalPaths["target"]) != canonicalRegistryPath(canonicalPackage) {
			return fmt.Errorf("UNREGISTERED_INSTALL: registry target does not match package root")
		}
		// Installed packages are only allowed to drive workflow writes through
		// the fixed launcher recorded by admission.  A direct invocation of the
		// candidate binary under the host target has the same package binding but
		// a different executable identity and is therefore rejected.  Repository
		// checkouts keep the legacy source-tree path and intentionally do not use
		// this installed-launcher check.
		executable, executableErr := os.Executable()
		if executableErr != nil {
			return fmt.Errorf("UNREGISTERED_INSTALL: cannot resolve invoking launcher: %w", executableErr)
		}
		base := filepath.Base(filepath.Clean(executable))
		if !strings.HasSuffix(base, ".test") && !strings.HasSuffix(base, ".test.exe") && canonicalRegistryPath(executable) != canonicalRegistryPath(record.LauncherPath) {
			return fmt.Errorf("UNREGISTERED_INSTALL: workflow must be driven by stable launcher %s", record.LauncherPath)
		}
		return nil
	}
	return fmt.Errorf("UNREGISTERED_INSTALL: registry record %q is missing", recordID)
}

func verifyRegisteredTargetIdentity(record RegistryRecord) error {
	if err := assertInstallSource(record.Target); err != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: registered target is not an installed artifact: %w", err)
	}
	if strings.TrimSpace(record.InstalledDigest) == "" {
		return nil
	}
	receipt, err := PackageReceipt(record.Target)
	if err != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: registered target identity cannot be read: %w", err)
	}
	if receipt.Digest != record.InstalledDigest {
		return fmt.Errorf("UNREGISTERED_INSTALL: registered target digest is stale")
	}
	return nil
}

// ResumeStatus is the recoverable classification reported when resuming an
// interrupted run: requirement edits need classification, catalog changes are
// reported per gate/action, a drifted native snapshot must be adopted, and a
// registered QA isolation worktree that no longer sits at the base snapshot must
// be confirmed or rebuilt by the user.
type ResumeStatus struct {
	ClassificationRequired bool     `json:"classificationRequired"`
	CatalogDelta           []string `json:"catalogDelta,omitempty"`
	NativeDrifted          bool     `json:"nativeDrifted"`
	IsolationDrifted       bool     `json:"isolationDrifted"`
}

// ResumeReport classifies everything the main agent must judge before the run
// can continue without hard failure.
func ResumeReport(root, packageRoot, runID string) (ResumeStatus, error) {
	if err := requireWorkflowAdmission(root, packageRoot); err != nil {
		return ResumeStatus{}, err
	}
	state, err := LoadRunState(root, runID)
	if err != nil {
		return ResumeStatus{}, err
	}
	if err := requireActive(state); err != nil {
		return ResumeStatus{}, err
	}
	catalog, err := LoadPromptCatalog(packageRoot)
	if err != nil {
		return ResumeStatus{}, err
	}
	changed, err := requirementArtifactsChanged(root, state.RequirementArtifacts)
	if err != nil {
		return ResumeStatus{}, err
	}
	native, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return ResumeStatus{}, err
	}
	isolationDrifted := false
	if strings.TrimSpace(state.QAWorktree) != "" {
		// 中断续跑时重校验隔离工作区原生标识 == 基线（工作区应停在基线，未漂移即正常；
		// 仅真实漂移才需用户确认/重建）。
		resolver, err := resolverForVCS(state.VCS, nil)
		if err != nil {
			return ResumeStatus{}, err
		}
		resolved, err := resolver.Resolve(cleanWorktree(state.QAWorktree))
		if err != nil {
			return ResumeStatus{}, fmt.Errorf("cannot re-verify QA isolation worktree: %w", err)
		}
		isolationDrifted = !strings.EqualFold(resolved, state.BaseSnapshot)
	}
	return ResumeStatus{ClassificationRequired: changed, CatalogDelta: catalogDelta(state, catalog), NativeDrifted: native != state.CurrentSnapshot, IsolationDrifted: isolationDrifted}, nil
}

// Resume is a workflow control operation even though it primarily reports
// state. A candidate executable must not use that read path to inspect or
// continue a stable run, so admission is checked before loading run bytes.
func requireWorkflowAdmission(root, packageRoot string) error {
	registry, recordID, err := discoverAdmissionBinding(root, packageRoot, "", "")
	if err != nil {
		return err
	}
	if registry == "" {
		return verifyLegacyStableLauncher()
	}
	receipt, err := AdmitRegistry(registry, recordID)
	if err != nil {
		writeWorkflowAdmissionRejection(registry, recordID, root, packageRoot, err.Error())
		return err
	}
	if !receipt.Accepted {
		writeWorkflowAdmissionReceipt(registry, receipt, root, packageRoot, receipt.Reason)
		return fmt.Errorf("%s: workflow resume refused for registry record %q", receipt.Code, recordID)
	}
	return verifyRegistryBinding(registry, recordID, root, packageRoot)
}

// AdoptExternalChange explicitly rebinds the current snapshot to the native
// head after the workspace drifted outside the run. When a development snapshot
// already exists, the previous snapshot becomes the pre-repair boundary so
// unaffected prior PASS results stay eligible for a Carry inheritance decision,
// and the review surface is reset. When the run has not yet produced a
// development snapshot (dev status is PENDING/PREPARED), there is nothing to
// inherit, so the adoption only rebinds the current snapshot and records the
// provenance under the reserved Carry key: no PreRepairSnapshot is set and the
// review surface is not reset. The main agent's reason is recorded as the
// adoption provenance under the reserved Carry key.
func AdoptExternalChange(root, packageRoot, runID, reason string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		current, err := resolveNativeSnapshot(root, state.VCS)
		if err != nil {
			return err
		}
		if current == state.CurrentSnapshot {
			return fmt.Errorf("native current snapshot already matches the run current snapshot")
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return fmt.Errorf("adopting an external change requires a reason")
		}
		oldSnapshot := state.CurrentSnapshot
		state.CurrentSnapshot = current
		// 采纳使派发源快照失效：既有 OPEN/CLAIMED 派发一律标 STALE。
		staleAllDispatches(state)
		if preDevelopment(*state) {
			// 尚无开发快照：不设置 PreRepairSnapshot、不重置审查面，后续开发不会
			// 被 "the current repair still requires verification" 挡住。
		} else {
			state.PreRepairSnapshot = oldSnapshot
			resetSnapshotReviewSurface(state, oldSnapshot, true, true)
		}
		state.Carry[carryAdoptKey] = CarryResult{Decision: "ADOPT", Origin: "ADOPT", SourceSnapshot: oldSnapshot, TargetSnapshot: current, Message: reason}
		return nil
	})
}

// preDevelopment reports whether the run has not yet produced a development
// snapshot: the development action status is PENDING or PREPARED. REPAIR_PREPARED
// sits behind an existing development snapshot and must not be treated as
// pre-development, so the predicate is not !hasDevelopmentSnapshot.
func preDevelopment(state RunState) bool {
	status := state.Actions["development-worker"].Status
	return status == developmentPending || status == developmentPrepared
}

func staleAllDispatches(state *RunState) {
	for id, dispatch := range state.Dispatches {
		if dispatch.Status == "OPEN" || dispatch.Status == "CLAIMED" {
			dispatch.Status = "STALE"
			state.Dispatches[id] = dispatch
		}
	}
}

// requireNoUnrecordedInFlightDispatch rejects a requirement rebinding while a
// review dispatch that records via record-action/record-gate has not recorded
// its result. Rebinding would invalidate those dispatches' source bindings and
// make their results impossible to record, forcing a re-run; the CLI forces
// recording the in-flight result first instead (需求 6 第 5 条）。判定只依赖既有
// 派发状态，不新增 returned 状态：
//   - 已准备（OPEN）或已认领（CLAIMED）的审查动作/门派发一律计入；
//     OPEN 且要求审查者的派发可在需求漂移后继续认领并记录旧 revision 的结果；
//   - 开发派发（快照记录）与 QA 派发（qa-* 命令记录）不经过 record-action/
//     record-gate，不计入。
func requireNoUnrecordedInFlightDispatch(state RunState) error {
	ids := make([]string, 0, len(state.Dispatches))
	for id, dispatch := range state.Dispatches {
		if !dispatchRecordsViaRecordCommand(dispatch) {
			continue
		}
		if dispatch.Status == "OPEN" || dispatch.Status == "CLAIMED" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return fmt.Errorf("cannot rebind the requirement while dispatch %s has not recorded its result; record the in-flight dispatch result before rebinding the requirement", strings.Join(ids, ", "))
}

// targetRecordsViaRecordCommand reports whether a record with the given target
// kind and id is written via record-action/record-gate: review actions
// (requirements-clarification, product-review, start-readiness) and gates.
// Development records via workflow snapshot, and QA records via its own
// qa-design / qa-review / qa-execution commands. Only the record-action /
// record-gate targets participate in the requirement-revision rebinding
// deadlock, so the same predicate scopes both the "record before rebind"
// ordering guard and the drift exemption for recording in-flight results.
func targetRecordsViaRecordCommand(targetKind, target string) bool {
	if targetKind == "gate" {
		return true
	}
	switch target {
	case "requirements-clarification", "product-review", "start-readiness":
		return true
	}
	return false
}

// dispatchRecordsViaRecordCommand reports whether a dispatch's result is
// recorded via record-action/record-gate (see targetRecordsViaRecordCommand).
func dispatchRecordsViaRecordCommand(dispatch PreparedDispatch) bool {
	return targetRecordsViaRecordCommand(dispatch.TargetKind, dispatch.Target)
}

// ResetResult reports what a workflow reset kept and what it reset. A reset is
// the user-gated escape hatch for a run whose flow state is broken (a snapshot
// cannot advance, an overall review was lost, a dispatch is stuck OPEN): it only
// touches the run's .gates flow data, never the developed content.
type ResetResult struct {
	RunID string   `json:"runId"`
	Kept  []string `json:"kept"`
	Reset []string `json:"reset"`
	State RunState `json:"state"`
}

// ResetRun resets a run's flow state back to a clean re-registrable state while
// preserving the developed content and recorded snapshots (需求 5）。Reset 是用户
// 权限门控命令：调用方（CLI）必须已获得用户显式授权，本函数假定授权已就绪。它：
//   - 重新登记需求：以当前需求文档内容重算修订并解除确认，使需求可重新登记
//     （注册修订与当前文档一致，需求修订漂移校验才不会挡住后续流程命令）；
//   - 重置流程状态：requirements-clarification / product-review / start-readiness
//     回到 PENDING（整体审可重做）、门与 QA 结果清空、carry 与派发清空（解开卡
//     OPEN 的派发）；
//   - 保留开发快照：base / current（开发快照）/ pre-repair 边界、拆分声明与
//     retained-overall / master 绑定、开发状态、路线决定原样保留，工作树与已提交
//     代码根本不被触碰（重置只写 run 的 .gates 状态文件）。
func ResetRun(root, packageRoot, runID string) (ResetResult, error) {
	var result ResetResult
	_, err := mutateRun(root, runID, func(state *RunState) error {
		result.RunID = state.RunID
		catalog, err := requireCurrentCatalog(*state, packageRoot)
		if err != nil {
			return err
		}
		// 1) 重新登记需求：沿用同一源与额外产物路径，以当前文档内容重算修订。
		source := state.RequirementSource
		var additional []string
		for _, artifact := range state.RequirementArtifacts {
			if artifact.Path != source {
				additional = append(additional, artifact.Path)
			}
		}
		artifacts, err := requirementArtifactSet(lifecycle.CleanRoot(root), source, additional)
		if err != nil {
			return err
		}
		revision := artifactRevision(artifacts, source)
		// 2) 重置流程状态。开发状态保留（开发快照保留）；其余动作回 PENDING、
		// 需求解除确认、结果类状态清空、卡住的派发清空。
		actions := pendingRequirementActions()
		if dev, ok := state.Actions["development-worker"]; ok {
			actions["development-worker"] = dev
		}
		state.Actions = actions
		state.RequirementConfirmed = false
		state.RequirementSource = source
		state.RequirementRevision = revision
		state.RequirementArtifacts = artifacts
		state.Gates = map[string]GateResult{}
		for _, id := range catalog.GateIDs() {
			state.Gates[id] = GateResult{Status: "PENDING"}
		}
		state.Carry = map[string]CarryResult{}
		state.Dispatches = map[string]PreparedDispatch{}
		state.QACasesByMode = map[string][]QACase{}
		state.QAExecutionByMode = map[string]QAExecutionResult{}
		state.PriorQAExecutionByMode = map[string]*QAExecutionResult{}
		state.QAReviewByMode = map[string]ActionResult{}
		state.QADesignByMode = map[string]ActionResult{}
		state.ExecutionScopes = map[string]QAExecutionScope{}
		state.SettledFindings = map[string][]SettledFinding{}
		state.ReviewItemsByAction = map[string]map[string]ReviewItem{}
		state.NeedsReReview = map[string]string{}
		state.ReReviewDispatch = map[string]string{}
		state.ReviewOverrides = map[string]string{}
		state.SnapshotOverride = nil
		state.BlackboxReviewFails = 0
		state.CompletedReviewWaves = 0
		state.ExtraReviewWaves = 0
		// 3) 输出说明保留了什么、重置了什么。
		result.Kept = []string{
			"working tree and committed code (untouched)",
			"requirement and solution documents on disk (untouched)",
			"base snapshot and current snapshot (recorded development snapshot)",
			"pre-repair snapshot boundary",
			"split declaration and retained-overall / master binding",
			"development action status",
			"route decision (route mode and selected gates)",
		}
		result.Reset = []string{
			"requirement registration (re-registered to the current document content, unconfirmed)",
			"requirements clarification",
			"product review",
			"start readiness",
			"gates and QA design / review / execution results",
			"carry results",
			"dispatches (stuck OPEN/CLAIMED cleared)",
			"settled findings, review items, re-review marks",
		}
		result.State = *state
		return nil
	})
	return result, err
}

func UpdateRequirement(root, packageRoot, runID, source string, confirmed bool, semanticEffect string, artifactPaths []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentCatalog(*state, packageRoot)
		if err != nil {
			return err
		}
		oldSource := state.RequirementSource
		if strings.TrimSpace(source) == "" {
			source = state.RequirementSource
		}
		source = normalizeArtifactPath(root, source)
		additional := artifactPaths
		if additional == nil {
			for _, artifact := range state.RequirementArtifacts {
				if artifact.Path != oldSource && artifact.Path != source {
					additional = append(additional, artifact.Path)
				}
			}
		}
		artifacts, err := requirementArtifactSet(lifecycle.CleanRoot(root), source, additional)
		if err != nil {
			return err
		}
		revision := artifactRevision(artifacts, source)
		changed := revision != state.RequirementRevision || source != state.RequirementSource || !sameArtifactSet(artifacts, state.RequirementArtifacts)
		semanticEffect = strings.ToLower(strings.TrimSpace(semanticEffect))
		if changed {
			if semanticEffect != "preserved" && semanticEffect != "changed" {
				return fmt.Errorf("changed requirement requires semantic effect preserved or changed")
			}
			// 需求 6 第 5 条：重绑前 CLI 强制校验本 run 是否存在未记录结果的在途派发
			// （已准备 OPEN 或已认领 CLAIMED、尚未记录结果）。重绑会作废这些派发的源绑定、
			// 使其结果无法记录而被迫重跑；存在时拒绝重绑、提示先记录该结果。按既有派发状态
			// 判定，不新增 returned 状态。
			if err := requireNoUnrecordedInFlightDispatch(*state); err != nil {
				return err
			}
			liveSnapshot, err := resolveNativeSnapshot(root, state.VCS)
			if err != nil {
				return err
			}
			if developmentStarted(*state) && semanticEffect == "preserved" && !confirmed {
				return fmt.Errorf("meaning-preserved requirement rebinding after development starts requires user confirmation")
			}
			state.RequirementSource, state.RequirementRevision, state.RequirementArtifacts = source, revision, artifacts
			if semanticEffect == "preserved" {
				if !state.RequirementConfirmed {
					return fmt.Errorf("meaning can be preserved only for a previously confirmed requirement")
				}
				rebindCurrentSnapshot(state, liveSnapshot)
				return nil
			}
			state.CurrentSnapshot = liveSnapshot
			invalidateRequirementResults(state, catalog.GateIDs())
			state.RequirementConfirmed = false
			if confirmed {
				return fmt.Errorf("a meaning-changing requirement must return to Requirements Clarification")
			}
			return nil
		}
		if semanticEffect != "" {
			return fmt.Errorf("semantic effect is accepted only when the requirement revision changed")
		}
		if confirmed && state.Actions["requirements-clarification"].Status != "PASS" {
			return fmt.Errorf("Requirements Clarification must pass before requirement confirmation")
		}
		state.RequirementConfirmed = confirmed
		return nil
	})
}

func RouteCandidates(root, packageRoot, runID string) ([]string, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return nil, err
	}
	catalog, err := requireCurrentDefinitions(root, state, packageRoot)
	if err != nil {
		return nil, err
	}
	if !state.RequirementConfirmed {
		return nil, fmt.Errorf("the current requirement is not confirmed")
	}
	return catalog.RouteCandidates(), nil
}

func SetRoute(root, packageRoot, runID, mode string, selected []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireTransition(*state, "route", ""); err != nil {
			return err
		}
		mode = strings.ToLower(strings.TrimSpace(mode))
		if !routeModes[mode] {
			return fmt.Errorf("route mode must be lightweight, full, or custom")
		}
		candidates := catalog.RouteCandidates()
		if mode == "full" {
			if len(selected) != 0 {
				return fmt.Errorf("full route selects the complete discovered list without --gate")
			}
			selected = candidates
		} else if mode == "lightweight" {
			// 轻量路线选中零门：不选 QA、不选门，跳过全部验证只留记录。
			if len(selected) != 0 {
				return fmt.Errorf("lightweight route selects zero gates without --gate")
			}
			selected = []string{}
		} else {
			var err error
			selected, err = normalizeSelected(selected, candidates)
			if err != nil {
				return err
			}
			if len(selected) == 0 || len(selected) == len(candidates) {
				return fmt.Errorf("custom route must select a non-empty proper subset; use full for the complete list")
			}
		}
		state.RouteMode = mode
		state.SelectedGates = append([]string{}, selected...)
		state.SkipAuthorizations = map[string]SkipAuthorization{}
		chosen := selectedSet(*state)
		for _, id := range candidates {
			if !chosen[id] {
				state.SkipAuthorizations[id] = SkipAuthorization{Origin: "ROUTE", Status: "UNSELECTED"}
			}
		}
		discardUnmatchedQADesign(state)
		return nil
	})
}

func AddRouteGates(root, packageRoot, runID string, additions []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireTransition(*state, "route-add", ""); err != nil {
			return err
		}
		if len(additions) == 0 {
			return fmt.Errorf("at least one gate addition is required")
		}
		candidates := catalog.RouteCandidates()
		normalized, err := normalizeSelected(additions, candidates)
		if err != nil {
			return err
		}
		chosen := selectedSet(*state)
		for _, id := range normalized {
			if chosen[id] {
				return fmt.Errorf("gate %q is already selected", id)
			}
			if isQAMode(id) && developmentStarted(*state) {
				return fmt.Errorf("QA cannot be added after development begins")
			}
			chosen[id] = true
			delete(state.SkipAuthorizations, id)
		}
		state.SelectedGates = orderedSelection(chosen, candidates)
		return nil
	})
}

// RecordSlicing records the run's formal split decision. The decision is binary
// (split or no-split), can only be recorded after Start Readiness passes, and
// once recorded is the binding point: it is not re-cut. A split requires at
// least two slices; when the run is retained overall, recording a split
// auto-attaches merge gate and merge QA as the run's only post-merge
// verification (the merge route), so it never goes through normal route
// selection. A no-split decision requires the mandatory "建议不拆（原因）" reason
// trace. The fast-path (non-high-confidence) decision note may note uncertainty.
func RecordSlicing(root, packageRoot, runID, decision string, splitCount int, slices []string, parallel, note, masterRunID string) (RunState, error) {
	return openRecord(root, packageRoot, runID, false, false, func(state *RunState, catalog PromptCatalog) error {
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		decision = strings.ToLower(strings.TrimSpace(decision))
		if decision != "split" && decision != "no-split" {
			return fmt.Errorf("slicing decision must be split or no-split")
		}
		if slicingRecorded(*state) {
			return fmt.Errorf("the slicing decision is already recorded and is the binding point; it is not re-cut")
		}
		// 需求 4：启动声明与拆分决定互相校验，杜绝"启动时说 no、拆分时说 split"或反向的
		// 不一致被静默放过。缺失启动声明的旧 run（本功能上线前启动）不在此约束，按旧语义
		// 处理（保留总任务实例仍须记录 split，其余 run 无额外限制）。
		switch state.SplitDeclaration {
		case "no":
			if decision == "split" {
				return fmt.Errorf("this run declared --split no at start, so a split decision is not allowed; restart with `workflow start --split yes ...` to split")
			}
		case "yes":
			if state.SplitMasterRunID != "" && decision == "no-split" {
				return fmt.Errorf("this run declared --split yes with --master %s at start, so a no-split decision is not allowed", state.SplitMasterRunID)
			}
		}
		if !state.RequirementConfirmed {
			return fmt.Errorf("the current requirement is not confirmed")
		}
		if state.RetainedOverall && decision == "no-split" {
			// 保留总任务实例专为拆分而启动，必须记录 split；记录 no-split 会让它
			// 无法进入开发（prepareDevelopmentAction 拒绝保留总任务实例），只能
			// abort+restart 恢复，是死端。
			return fmt.Errorf("a retained-overall run must record a split decision")
		}
		if decision == "split" && !state.RetainedOverall {
			// 切片实例：必须引用其保留总任务 master run，且该 master 的整体级产品
			// 审/技术审已 PASS，才继承整体级审查结果（记录继承来源与 master 引用），
			// 不再要求切片内重跑；development-worker 门对切片实例经继承满足。不自动
			// 附加合并验证，之后仍走逐切片路线确认与常规开发流程。
			if splitCount < 2 {
				return fmt.Errorf("a split requires at least two slices")
			}
			if strings.TrimSpace(masterRunID) == "" {
				return fmt.Errorf("a slice instance must reference its retained-overall master with --master")
			}
			if state.SplitMasterRunID != "" && strings.TrimSpace(masterRunID) != state.SplitMasterRunID {
				return fmt.Errorf("slice master %q does not match the master %q pinned in the start declaration (--split yes --master)", strings.TrimSpace(masterRunID), state.SplitMasterRunID)
			}
			master, err := LoadRunState(root, strings.TrimSpace(masterRunID))
			if err != nil {
				return fmt.Errorf("slice master run %q is not found: %v", strings.TrimSpace(masterRunID), err)
			}
			if err := requireRetainedSplitMaster(master); err != nil {
				return fmt.Errorf("slice master %q is invalid: %w", strings.TrimSpace(masterRunID), err)
			}
			if master.Slicing == nil || master.Slicing.Decision != "split" {
				return fmt.Errorf("slice master %q has not recorded its split decision", strings.TrimSpace(masterRunID))
			}
			if master.Actions["product-review"].Status != "PASS" {
				return fmt.Errorf("slice master %q has not passed Product Review", strings.TrimSpace(masterRunID))
			}
			if master.Actions["start-readiness"].Status != "PASS" {
				return fmt.Errorf("slice master %q has not passed Start Readiness", strings.TrimSpace(masterRunID))
			}
			if err := requireMatchingInheritedReviewInputs(*state, master); err != nil {
				return fmt.Errorf("slice review inputs do not match master %q: %w", strings.TrimSpace(masterRunID), err)
			}
			if state.VCS != master.VCS || state.BaseSnapshot != master.BaseSnapshot {
				return fmt.Errorf("slice VCS/base snapshot does not match master %q", strings.TrimSpace(masterRunID))
			}
			if splitCount != master.Slicing.SplitCount || !sameOrderedStrings(slices, master.Slicing.Slices) || strings.TrimSpace(parallel) != master.Slicing.Parallel {
				return fmt.Errorf("slice split topology does not match master %q", strings.TrimSpace(masterRunID))
			}
			state.Slicing = &Slicing{
				Decision:         master.Slicing.Decision,
				SplitCount:       master.Slicing.SplitCount,
				Slices:           append([]string{}, master.Slicing.Slices...),
				Parallel:         master.Slicing.Parallel,
				MasterRunID:      strings.TrimSpace(masterRunID),
				InheritedReviews: []string{"product-review", "start-readiness"},
			}
			return nil
		}
		if !actionPassedOrAbsent(*state, "product-review") {
			return fmt.Errorf("Product Review must pass before the slicing decision")
		}
		if !actionPassedOrAbsent(*state, "start-readiness") {
			return fmt.Errorf("Start Readiness must pass before the slicing decision")
		}
		if decision == "split" {
			if splitCount < 2 {
				return fmt.Errorf("a split requires at least two slices")
			}
			// 走到这里时 decision == "split" 且 state.RetainedOverall 必为 true（顶层
			// 已处理所有拆分的非保留总任务 run）。分片 >= 2 的保留总任务实例自动
			// 附加合并门与合并 QA：路线确定为合并路线，不涉常规路线选择，custom
			// 的省略不延伸到合并验证。先确认合并门在当前目录中已发现，否则后续
			// prepare-gate 会死端。
			mergeGateDiscovered := false
			for _, gate := range catalog.Gates {
				if gate.ID == mergeGateID {
					mergeGateDiscovered = true
					break
				}
			}
			if !mergeGateDiscovered {
				return fmt.Errorf("merge gate %q is not discovered in the package catalog", mergeGateID)
			}
			state.Slicing = &Slicing{Decision: decision, SplitCount: splitCount, Slices: slices, Parallel: strings.TrimSpace(parallel)}
			state.RouteMode = "merge"
			state.SelectedGates = []string{mergeQAID, mergeGateID}
			state.SkipAuthorizations = map[string]SkipAuthorization{}
			state.QAExecutionByMode = map[string]QAExecutionResult{}
			return nil
		}
		if strings.TrimSpace(note) == "" {
			return fmt.Errorf("a no-split decision requires the mandatory reason note (建议不拆原因)")
		}
		state.Slicing = &Slicing{Decision: decision, Note: strings.TrimSpace(note)}
		return nil
	})
}

func requireRetainedSplitMaster(master RunState) error {
	if master.Status != "ACTIVE" {
		return fmt.Errorf("run is %s, not ACTIVE", master.Status)
	}
	if !master.RetainedOverall || master.SplitDeclaration != "yes" || master.SplitMasterRunID != "" {
		return fmt.Errorf("run is not a retained-overall --split yes instance")
	}
	return nil
}

func requireMatchingInheritedReviewInputs(slice, master RunState) error {
	if slice.RequirementSource != master.RequirementSource || slice.RequirementRevision != master.RequirementRevision || !sameArtifactSet(slice.RequirementArtifacts, master.RequirementArtifacts) {
		return fmt.Errorf("requirement source or artifact set differs")
	}
	if slice.BasePromptRevision != master.BasePromptRevision || slice.CatalogRevision != master.CatalogRevision {
		return fmt.Errorf("review catalog differs")
	}
	for _, actionID := range []string{"product-review", "start-readiness"} {
		key := "action:" + actionID
		sliceHash, sliceOK := slice.PromptHashes[key]
		masterHash, masterOK := master.PromptHashes[key]
		if !sliceOK || !masterOK || sliceHash == "" || sliceHash != masterHash {
			return fmt.Errorf("%s prompt differs", actionID)
		}
	}
	return nil
}

func sameOrderedStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// RecordSettledFindings records the user's per-item disposition of findings from
// a product-review / start-readiness review. Confirm (确认问题：真问题、需修订) and
// dismiss (驳回问题：不是问题、作废) are both recorded and injected into the next
// dispatch so the reviewer does not re-raise them (reviewer-side enforcement of
// the double guarantee). Confirming a P0/P1 finding sets the "需重审" marker
// (NeedsReReview): the CLI then refuses to record PASS until a re-review round
// returns PASS. A dismissed P0/P1 is void and does not block. A meaning-changing
// requirement revision clears the settled list because the revised premise may
// legitimately re-raise an item.
func RecordSettledFindings(root, packageRoot, runID, actionID string, confirm, dismiss []string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if actionID != "product-review" && actionID != "start-readiness" {
			return fmt.Errorf("settled findings are recorded for product-review or start-readiness only")
		}
		if len(confirm) == 0 && len(dismiss) == 0 {
			return fmt.Errorf("at least one settled finding is required")
		}
		// 同一 finding 同时 confirm+dismiss 是自相矛盾的已拍板记录，拒绝。
		for _, message := range confirm {
			for _, dismissed := range dismiss {
				if strings.TrimSpace(message) != "" && strings.TrimSpace(message) == strings.TrimSpace(dismissed) {
					return fmt.Errorf("finding %q cannot be both confirmed and dismissed", strings.TrimSpace(message))
				}
			}
		}
		if state.NeedsReReview == nil {
			state.NeedsReReview = map[string]string{}
		}
		severityByMessage := map[string]string{}
		for _, finding := range state.Actions[actionID].Findings {
			if strings.TrimSpace(finding.Message) != "" {
				severityByMessage[strings.TrimSpace(finding.Message)] = finding.Severity
			}
		}
		settle := func(message, disposition string) error {
			message = strings.TrimSpace(message)
			if message == "" {
				return fmt.Errorf("settled finding message is required")
			}
			if severity, ok := severityByMessage[message]; !ok || severity == "" {
				return fmt.Errorf("finding %q is not in the recorded %s result", message, actionID)
			}
			if state.SettledFindings == nil {
				state.SettledFindings = map[string][]SettledFinding{}
			}
			state.SettledFindings[actionID] = append(state.SettledFindings[actionID], SettledFinding{Message: message, Disposition: disposition})
			// 确认的 P0/P1 置位"需重审"标记；驳回或确认的 P2/P3 不置位。
			if disposition == "confirm" && (severityByMessage[message] == "P0" || severityByMessage[message] == "P1") {
				state.NeedsReReview[actionID] = message
			}
			return nil
		}
		for _, message := range confirm {
			if err := settle(message, "confirm"); err != nil {
				return err
			}
		}
		for _, message := range dismiss {
			if err := settle(message, "dismiss"); err != nil {
				return err
			}
		}
		return nil
	})
}

// RegisterQAWorktree registers the QA isolation worktree for the run. The
// worktree is created from the base snapshot by the host (Git linked worktree
// branched from base; SVN workspace checked out at base; P4 client synced to the
// base changelist) and must resolve its native identity to the base snapshot.
// Registration also verifies the current requirement document / acceptance
// artifacts are injected into the worktree with the run's registered revisions
// (a worktree-state injection, not a drift: git commits / p4 changelists / svn
// BASE versions ignore it). This is the single home of that hash check
// (requirement 1's "登记**或**黑盒 prepare 时校验" guard is contracted here):
// later blackbox prepare/claim/record only re-resolve the native identity (==
// base) without re-reading or re-hashing the worktree files. Each blackbox
// design round recreates or reuses this worktree; it never contains development
// code.
func RegisterQAWorktree(root, packageRoot, runID, worktree string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		worktree = strings.TrimSpace(worktree)
		if worktree == "" {
			return fmt.Errorf("QA isolation worktree path is required")
		}
		resolver, err := resolverForVCS(state.VCS, nil)
		if err != nil {
			return err
		}
		resolved, err := resolver.Resolve(cleanWorktree(worktree))
		if err != nil {
			return fmt.Errorf("cannot resolve QA isolation worktree: %w", err)
		}
		if !strings.EqualFold(resolved, state.BaseSnapshot) {
			return fmt.Errorf("QA isolation worktree native identity %s does not match the base snapshot %s", resolved, state.BaseSnapshot)
		}
		// 校验隔离工作区内需求文档/验收产物哈希与 run 登记 revision 一致（防 host 遗忘
		// 刷新注入；需求 1 的"登记**或**黑盒 prepare 校验"单点落在登记处，prepare/claim/
		// record 不再重复哈希复查）。注入是工作树状态，Git=提交、P4=changelist、SVN=BASE
		// 版本级身份校验不受影响。
		if err := verifyIsolatedRequirementRevisions(*state, worktree); err != nil {
			return err
		}
		state.QAWorktree = worktree
		return nil
	})
}

func ClaimDispatch(root, packageRoot, runID, dispatchID, reviewerIdentity string) (RunState, error) {
	return ClaimDispatchWithProvider(root, packageRoot, runID, dispatchID, reviewerIdentity, "")
}

func ClaimDispatchWithProvider(root, packageRoot, runID, dispatchID, reviewerIdentity, provider string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		dispatchID, reviewerIdentity = strings.TrimSpace(dispatchID), strings.TrimSpace(reviewerIdentity)
		if dispatchID == "" {
			return fmt.Errorf("dispatch id is required")
		}
		dispatch, ok := state.Dispatches[dispatchID]
		if !ok {
			return fmt.Errorf("unknown dispatch %q", dispatchID)
		}
		requirementDrifted, err := requirementArtifactsChanged(root, state.RequirementArtifacts)
		if err != nil {
			return err
		}
		recordRecovery := dispatch.Status == "OPEN" && dispatchRecordsViaRecordCommand(dispatch) && requirementDrifted
		// A prepared record-action/record-gate dispatch remains bound to the old
		// requirement revision. Let it be claimed after document drift so its
		// result can be recorded before requirement rebinding; all other claims
		// retain the normal live-revision check.
		var catalog PromptCatalog
		if recordRecovery {
			catalog, err = requireCurrentCatalog(*state, packageRoot)
		} else {
			catalog, err = requireCurrentDefinitions(root, *state, packageRoot)
		}
		if err != nil {
			return err
		}
		// 硬闸：carry 处置派发的认领豁免，其余派发在存在待决继承判定时拒绝认领。
		if !(dispatch.TargetKind == "action" && dispatch.Target == "carry") {
			if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
				return err
			}
		}
		if dispatch.TargetKind == "action" && dispatch.Target == "development-worker" {
			// 开发/修复派发是 reviewer-required；worker 一旦提交，原生 HEAD 就前进到
			// 派发源快照之后。认领放宽：当前原生 HEAD 是派发源快照的后代（或相等）
			// 即允许认领（覆盖 worker 已提交的情形），否则 worker 提交后认领必失败，
			// "会提交的开发 worker"没有可行路径。该检查不验证 HEAD 是否由 worker 产生，
			// 开发期间无关外部提交落地会被静默吸收进开发快照（文档已注明）。其余派发
			// （审查、QA 等）仍要求原生 HEAD 精确等于当前快照。
			if err := requireDispatchSourceAncestorOfHead(root, *state, dispatch); err != nil {
				return err
			}
		} else if recordRecovery {
			// A committed requirement edit advances HEAD after prepare. The old
			// review remains reproducible from its immutable source snapshot, so
			// permit descendants while preserving the dispatch's old bindings.
			if err := requireDispatchSourceAncestorOfHead(root, *state, dispatch); err != nil {
				return err
			}
		} else if dispatch.Mode == "blackbox" && (dispatch.Target == "qa-design" || dispatch.Target == "qa-review") {
			// 黑盒 qa-design/qa-review 派发对 QA 隔离工作区解析原生标识（恒等于基线）。
			if err := requireIsolatedCurrent(root, *state); err != nil {
				return err
			}
		} else if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if !dispatch.ReviewerRequired {
			return fmt.Errorf("dispatch %q does not require a reviewer claim", dispatchID)
		}
		if dispatch.Status != "OPEN" {
			return fmt.Errorf("dispatch %q is %s and cannot be claimed", dispatchID, dispatch.Status)
		}
		// Resolve the effective claim identity: the preferred reviewer
		// identity wins when it matches a pending subagent start observation;
		// otherwise a unique pending start observation supplies its own
		// identity (common operator mistake compatibility), and an ambiguous
		// or empty resolution is rejected rather than binding the wrong
		// subagent or silently dropping lifecycle evidence.
		effective, err := resolveClaimIdentity(root, state.RunID, reviewerIdentity, provider)
		if err != nil {
			return err
		}
		for priorID, prior := range state.Dispatches {
			if prior.ReviewerIdentity == effective {
				return fmt.Errorf("reviewer identity is already reserved by dispatch %s", priorID)
			}
		}
		// 同功能去重：认领新派发时清掉同功能旧 OPEN 空票（无子代理、不挡认领）；已有
		// CLAIMED 同功能派发默认拒绝并行，除非前子代理已被主代理手动终结（生命周期已捕获其
		// stop 事件/中断原因 → 把前派发标 STALE、允许认领新派发）。
		if err := enforceSameFunctionDedup(root, state, dispatch); err != nil {
			return err
		}
		// 派发时（兜底）再次校验规范提示词文件内容与 prepare 记录一致：子代理只读该文件
		// 执行，文件被手写/篡改/凭记忆重拼时，认领即硬阻断、该派发判定为不可用，必须先
		// 重新 prepare（需求 6 第 4 条）。写入时的第一时间校验已挡住写钩子在写出那一刻的
		// 篡改，本校验兜底挡派发时文件已被改掉的情形。
		if err := verifyCanonicalPromptFile(*state, dispatch); err != nil {
			return err
		}
		if err := bindClaimDispatch(root, state.RunID, dispatchID, effective, provider); err != nil {
			return err
		}
		dispatch.ReviewerIdentity, dispatch.Status = effective, "CLAIMED"
		state.Dispatches[dispatchID] = dispatch
		return nil
	})
}

func resolveClaimIdentity(root, runID, preferred, provider string) (string, error) {
	if strings.TrimSpace(provider) != "" {
		if explicit, ok := workflowLifecycle.(interface {
			ResolveClaimIdentityWithProvider(string, string, string, string) (string, error)
		}); ok {
			return explicit.ResolveClaimIdentityWithProvider(root, runID, preferred, provider)
		}
		return "", fmt.Errorf("lifecycle implementation does not support explicit provider claim context")
	}
	return workflowLifecycle.ResolveClaimIdentity(root, runID, preferred)
}

func bindClaimDispatch(root, runID, dispatchID, identity, provider string) error {
	if strings.TrimSpace(provider) != "" {
		if explicit, ok := workflowLifecycle.(interface {
			BindWithProvider(string, string, string, string, string) error
		}); ok {
			return explicit.BindWithProvider(root, runID, dispatchID, identity, provider)
		}
		return fmt.Errorf("lifecycle implementation does not support explicit provider claim context")
	}
	return workflowLifecycle.Bind(root, runID, dispatchID, identity)
}

// openRecord opens a Record* state mutation and runs the shared entry prologue:
// load the current definitions and, when checkPending, reject any pending
// inheritance. skipDrift 豁免需求修订漂移硬阻断：只由 openDispatchRecord 为
// record-action/record-gate 目标的在途派发记录传 true——该派发按 run 注册的旧
// revision prepare、结果属于旧 revision，先记录在途结果再重绑的恢复路径必须可执行
// （否则漂移硬阻断与在途派发重绑守卫互锁成死锁）；QA 记录（qa-design / qa-review /
// qa-execution）不经 record-action/record-gate、不在重绑守卫计数内、本无死锁，与其余
// 记录命令一样保持漂移硬阻断。change receives the loaded catalog and performs the
// record write.
func openRecord(root, packageRoot, runID string, checkPending, skipDrift bool, change func(*RunState, PromptCatalog) error) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		var catalog PromptCatalog
		var err error
		if skipDrift {
			catalog, err = requireCurrentCatalog(*state, packageRoot)
		} else {
			catalog, err = requireCurrentDefinitions(root, *state, packageRoot)
		}
		if err != nil {
			return err
		}
		if checkPending {
			if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
				return err
			}
		}
		return change(state, catalog)
	})
}

// recordDispatchOptions carries the per-record entry checks for a dispatch-bound
// Record* write: the prepared dispatch binding, the transition rule, whether the
// native snapshot is verified on the main worktree or the QA isolation worktree,
// and an optional target/catalog check that runs after dispatch resolution.
type recordDispatchOptions struct {
	targetKind             string                        // dispatch target kind: "action" or "gate"
	target                 string                        // dispatch target id
	transitionOp           string                        // transition operation
	transitionTarget       func(PreparedDispatch) string // transition target (dispatch.Mode for QA)
	requireMainNative      bool                          // verify main-worktree native == current
	requireIsolationNative bool                          // verify QA isolation-worktree native == base
	requireLifecycle       bool                          // verify lifecycle pairing
	preCheck               func(*RunState, PromptCatalog) error
}

// openDispatchRecord opens a Record* state mutation for a dispatch-bound write
// and runs the shared entry prologue: load current definitions (exempting the
// requirement-revision drift hard block only for record-action/record-gate
// targets — 记录在途派发结果属于旧 revision，先记录再重绑的恢复路径必须可执行，见
// openRecord), reject pending inheritance, verify the native snapshot, resolve the
// prepared dispatch, run the transition rule, and verify lifecycle pairing.
// change receives the loaded catalog and resolved dispatch and performs the
// record-specific write.
func openDispatchRecord(root, packageRoot, runID, dispatchID string, opts recordDispatchOptions, change func(*RunState, PromptCatalog, PreparedDispatch) error) (RunState, error) {
	return openRecord(root, packageRoot, runID, true, targetRecordsViaRecordCommand(opts.targetKind, opts.target), func(state *RunState, catalog PromptCatalog) error {
		dispatch, err := requirePreparedDispatch(*state, dispatchID, opts.targetKind, opts.target)
		if err != nil {
			return err
		}
		if opts.requireMainNative {
			requirementDrifted, err := requirementArtifactsChanged(root, state.RequirementArtifacts)
			if err != nil {
				return err
			}
			if requirementDrifted && dispatchRecordsViaRecordCommand(dispatch) {
				if err := requireDispatchSourceAncestorOfHead(root, *state, dispatch); err != nil {
					return err
				}
			} else if _, err := requireNativeCurrent(root, *state); err != nil {
				return err
			}
		}
		if opts.preCheck != nil {
			if err := opts.preCheck(state, catalog); err != nil {
				return err
			}
		}
		if err := requireTransition(*state, opts.transitionOp, opts.transitionTarget(dispatch)); err != nil {
			return err
		}
		if opts.requireIsolationNative {
			if err := requireDispatchNativeCurrent(root, *state, dispatch); err != nil {
				return err
			}
		}
		if opts.requireLifecycle {
			if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
				return err
			}
		}
		return change(state, catalog, dispatch)
	})
}

// RecordAction records a product-review / start-readiness / requirements-
// clarification result. userRequested is the explicit user override signal for
// the review-rule enforcement (only the user can break the rule); its source is
// recorded in ReviewOverrides.
func RecordAction(root, packageRoot, runID, actionID, dispatchID, status, message string, findings []FindingInput, userRequested bool, userReason string, items ...ReviewItemInput) (RunState, error) {
	// record-action status 严格大小写校验：pass 之类的小写不宽容记录，必须精确
	// PASS / FAIL / RUNTIME_ERROR（trim 后）。
	if raw := strings.TrimSpace(status); raw != "PASS" && raw != "FAIL" && raw != "RUNTIME_ERROR" {
		return RunState{}, fmt.Errorf("record-action status must be exactly PASS, FAIL, or RUNTIME_ERROR (case-sensitive); got %q", status)
	}
	return openDispatchRecord(root, packageRoot, runID, dispatchID, recordDispatchOptions{
		targetKind:        "action",
		target:            actionID,
		transitionOp:      actionID,
		transitionTarget:  func(PreparedDispatch) string { return "" },
		requireMainNative: true,
		preCheck: func(state *RunState, catalog PromptCatalog) error {
			if _, ok := catalog.Action(actionID); !ok {
				return fmt.Errorf("unknown action prompt %q", actionID)
			}
			if actionID != "requirements-clarification" && actionID != "start-readiness" && actionID != "product-review" {
				return fmt.Errorf("action %q has a dedicated workflow command and cannot use record-action", actionID)
			}
			return nil
		},
	}, func(state *RunState, catalog PromptCatalog, dispatch PreparedDispatch) error {
		if actionID == "start-readiness" || actionID == "product-review" {
			if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
				return err
			}
			if err := enforceReviewRule(state, actionID, dispatch.ID, status, findings, userRequested, userReason); err != nil {
				return err
			}
			// 增量审查：record-action 下发逐项判定。所有 PENDING 项必须全判；对已 PASS
			// 项下发判定被拒；FAIL 项必须带 finding（reason）。
			if err := recordReviewItems(state, actionID, dispatch.ID, items); err != nil {
				return err
			}
		}
		backfillDispatchCost(root, state, dispatch)
		result, err := semanticActionResult(actionID, status, message, findings, state)
		if err != nil {
			return err
		}
		result.DispatchID = dispatch.ID
		state.Actions[actionID] = result
		completeDispatch(state, dispatch.ID)
		return nil
	})
}

// recordReviewItems 逐项登记 product-review / start-readiness 的增量审查判定。
// 语义与 QA 增量一致：prepare-action --scope 声明的 PENDING 项在此必须全部判定；对已 PASS
// 项下发判定被拒（除非主代理下次 prepare 显式重新声明变更）；FAIL 项必须携带 finding。
// 未声明 scope（该动作无逐项表）时不做逐项约束，回到全量审查路径。
func recordReviewItems(state *RunState, actionID, dispatchID string, items []ReviewItemInput) error {
	table := state.ReviewItemsByAction[actionID]
	if len(table) == 0 {
		return nil
	}
	seen := map[string]bool{}
	pending := map[string]bool{}
	for key, item := range table {
		if item.Status != "PASS" {
			pending[key] = true
		}
	}
	// 逐项表非空但已全部 PASS（无待定项）时，空判定集是合法形态（P2-1 死路修复）：
	// prepare 无 --scope 时逐项表全 PASS 会生成"无待定项可判"提示，审查者按提示不下发
	// 任何 --item，record 必须接受空集而非拒绝——否则无法记录该轮审查。仅当确实存在
	// 待定项时才强制"所有 PENDING 必须全判"。
	if len(pending) == 0 && len(items) == 0 {
		return nil
	}
	if len(items) == 0 {
		return fmt.Errorf("the %s incremental review requires one --item decision for every pending item", actionID)
	}
	for _, input := range items {
		key := strings.TrimSpace(input.Key)
		itemStatus := strings.ToUpper(strings.TrimSpace(input.Status))
		if key == "" {
			return fmt.Errorf("review item key is required")
		}
		if itemStatus != "PASS" && itemStatus != "FAIL" {
			return fmt.Errorf("review item %q status must be PASS or FAIL", key)
		}
		existing, ok := table[key]
		if !ok {
			return fmt.Errorf("review item %q is not in the declared review scope", key)
		}
		if existing.Status == "PASS" {
			return fmt.Errorf("review item %q already has an authoritative PASS result and cannot be re-judged; redeclare it in prepare-action --scope to re-review", key)
		}
		if seen[key] {
			return fmt.Errorf("duplicate review item decision for %q", key)
		}
		seen[key] = true
		if itemStatus == "FAIL" && strings.TrimSpace(input.Reason) == "" {
			return fmt.Errorf("review item %q FAIL requires a finding (reason)", key)
		}
		table[key] = ReviewItem{Status: itemStatus, DispatchID: dispatchID, Message: strings.TrimSpace(input.Reason)}
	}
	for key := range pending {
		if !seen[key] {
			return fmt.Errorf("review item %q is pending and must be judged (all pending items require a decision)", key)
		}
	}
	return nil
}

// enforceReviewRule implements the CLI-forced review rules for product-review
// and start-readiness (requirement 5):
//   - 仅 P2/P3 → 该轮记录 PASS，P2/P3 建议随 PASS 可见、不阻塞、不产生 FAIL；
//   - P0/P1 → 记录 FAIL；用户逐项确认或驳回。驳回的 P0/P1 作废不阻塞（PASS 可携带已
//     驳回的 P0/P1）；确认的 P0/P1 置位"需重审"标记，CLI 在重审前拒绝记录 PASS；
//   - 只有用户可破例：任一侧的破例都须 userRequested 显式授权，来源记录到
//     ReviewOverrides；主代理无破例权。
func enforceReviewRule(state *RunState, actionID, dispatchID, status string, findings []FindingInput, userRequested bool, userReason string) error {
	if state.NeedsReReview == nil {
		state.NeedsReReview = map[string]string{}
	}
	if state.ReReviewDispatch == nil {
		state.ReReviewDispatch = map[string]string{}
	}
	status = strings.ToUpper(strings.TrimSpace(status))
	switch status {
	case "PASS":
		dismissed := settledMessagesByDisposition(*state, actionID, "dismiss")
		for _, finding := range findings {
			severity := strings.ToUpper(strings.TrimSpace(finding.Severity))
			if (severity == "P0" || severity == "P1") && !dismissed[strings.TrimSpace(finding.Message)] {
				if userRequested {
					recordReviewOverride(state, actionID, userReason)
					return nil
				}
				return fmt.Errorf("P0/P1 finding %q is confirmed or undisposed; record FAIL and re-review after a requirement revision, or dismiss it explicitly", finding.Message)
			}
		}
		if state.NeedsReReview[actionID] != "" && state.ReReviewDispatch[actionID] != dispatchID {
			if userRequested {
				recordReviewOverride(state, actionID, userReason)
				return nil
			}
			return fmt.Errorf("confirmed P0/P1 finding %q awaits a re-review; record-action PASS is rejected before the re-review", state.NeedsReReview[actionID])
		}
		if state.NeedsReReview[actionID] != "" {
			delete(state.NeedsReReview, actionID)
			delete(state.ReReviewDispatch, actionID)
		}
	case "FAIL":
		confirmed := settledMessagesByDisposition(*state, actionID, "confirm")
		for _, finding := range findings {
			severity := strings.ToUpper(strings.TrimSpace(finding.Severity))
			if (severity == "P0" || severity == "P1") && confirmed[strings.TrimSpace(finding.Message)] {
				state.NeedsReReview[actionID] = strings.TrimSpace(finding.Message)
				delete(state.ReReviewDispatch, actionID)
			}
		}
	}
	return nil
}

func recordReviewOverride(state *RunState, actionID, userReason string) {
	if state.ReviewOverrides == nil {
		state.ReviewOverrides = map[string]string{}
	}
	reason := strings.TrimSpace(userReason)
	if reason == "" {
		reason = "user explicitly requested an override"
	}
	state.ReviewOverrides[actionID] = reason
}

// settledMessagesByDisposition lists the settled finding messages of the action
// that carry the named disposition (confirm or dismiss).
func settledMessagesByDisposition(state RunState, actionID, disposition string) map[string]bool {
	result := map[string]bool{}
	for _, item := range state.SettledFindings[actionID] {
		if item.Disposition == disposition {
			result[item.Message] = true
		}
	}
	return result
}

func RecordGate(root, packageRoot, runID, gateID, dispatchID, status, message, compared string, findings []FindingInput) (RunState, error) {
	return openDispatchRecord(root, packageRoot, runID, dispatchID, recordDispatchOptions{
		targetKind:        "gate",
		target:            gateID,
		transitionOp:      "gate",
		transitionTarget:  func(PreparedDispatch) string { return gateID },
		requireMainNative: true,
		requireLifecycle:  true,
		preCheck: func(state *RunState, catalog PromptCatalog) error {
			if _, ok := catalog.Gate(gateID); !ok {
				return fmt.Errorf("gate %q is not discovered", gateID)
			}
			return nil
		},
	}, func(state *RunState, catalog PromptCatalog, dispatch PreparedDispatch) error {
		backfillDispatchCost(root, state, dispatch)
		existing := state.Gates[gateID]
		if semanticResultRecorded(existing.Status, existing.Snapshot, state.CurrentSnapshot) && !gatePromptChanged(*state, catalog, gateID) {
			return fmt.Errorf("gate %q already has an authoritative %s result for the current snapshot; re-review requires a changed gate prompt or a repair snapshot", gateID, existing.Status)
		}
		if normalized := strings.ToUpper(strings.TrimSpace(status)); normalized != "RUNTIME_ERROR" {
			want := comparedRange(*state)
			if !strings.EqualFold(strings.TrimSpace(compared), want) {
				return fmt.Errorf("gate review reported compared %q but the requested base-to-current range is %q", compared, want)
			}
		}
		result, err := semanticGateResult(status, message, findings, state)
		if err != nil {
			return err
		}
		if err := rejectFrozenArtifactFindings(*state, result.Findings); err != nil {
			return err
		}
		result.Compared = strings.TrimSpace(compared)
		result.DispatchID = dispatch.ID
		state.Gates[gateID] = result
		// Settle the gate's recorded prompt hash only when the run already keeps a
		// full hash record. A run state loaded without hashes keeps its absent
		// semantics (nil) so an unmoved catalog still reports no delta instead of
		// mis-reporting every entry after a partial backfill.
		if gate, ok := catalog.Gate(gateID); ok && state.PromptHashes != nil {
			state.PromptHashes["gate:"+gateID] = composedGatePromptHash(catalog, gate.Content)
		}
		completeDispatch(state, dispatch.ID)
		completeReviewWaveIfReady(state)
		return nil
	})
}

// AdvanceSnapshot records the development or repair snapshot. The snapshot gate
// requires development complete 且 黑盒 qa-review PASS（两边都完成）；黑盒 review 未
// PASS 时快照被挡。userRequested 是用户显式的手动放行授权（类比 --user-requested），
// 记录授权来源到 SnapshotOverride，使黑盒门未通过时带风险继续；未获批准的黑盒用例
// 验证状态视为 PASS、qa-execution 只覆盖已批准用例。
func AdvanceSnapshot(root, packageRoot, runID, dispatchID string, userRequested bool, reason string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		currentSnapshot, err := resolveNativeSnapshot(root, state.VCS)
		if err != nil {
			return err
		}
		if err := verifySnapshotReady(root, state.VCS); err != nil {
			return err
		}
		developmentStatus := state.Actions["development-worker"].Status
		// 快照要求开发侧真正完成：产生开发提交（原生标识前进到派发源快照之后），而非仅
		// PREPARED 状态。dev worker 已派发但未提交时，快照不得直接把基线记为开发快照
		// （需求 2 验收"任一未完成时 snapshot 被挡"）。
		if currentSnapshot == state.CurrentSnapshot {
			return fmt.Errorf("a new current snapshot is required")
		}
		if err := verifyNativeSnapshot(root, state.VCS, state.CurrentSnapshot); err != nil {
			return err
		}
		if err := requireTransition(*state, "snapshot", ""); err != nil {
			return err
		}
		// 白盒测试代码推进路径（方案 A）：host 已提交白盒设计者交付的结构测试代码，
		// 把快照推进到包含该测试代码的新提交，供 qa-review / qa-execution 在其上派发、
		// Seal 后交付物包含测试代码。该路径不引用 development-worker 派发（白盒设计
		// 阶段无开发工作者派发），也不重置审查面、不改动开发状态（开发已由上一快照完成，
		// 白盒审查/执行/门审尚未开始）。
		if whiteboxTestCodeAdvancement(*state) {
			state.CurrentSnapshot = currentSnapshot
			return nil
		}
		var developmentDispatch PreparedDispatch
		if state.RetainedOverall {
			if strings.TrimSpace(dispatchID) != "" {
				return fmt.Errorf("a retained overall snapshot does not accept a development dispatch")
			}
		} else {
			developmentDispatch, err = requirePreparedDispatch(*state, dispatchID, "action", "development-worker")
			if err != nil {
				return err
			}
			if err := requireLifecycleVerification(root, *state, developmentDispatch); err != nil {
				return err
			}
			backfillDispatchCost(root, state, developmentDispatch)
		}
		// 快照黑盒门（等两边都完成）：黑盒 qa-review PASS 且 开发完成才可快照。黑盒
		// qa-review 未 PASS 且此前没有用户放行时，只有用户显式授权可手动放行并记录授权
		// 来源；已放行（SnapshotOverride 非空）后未批准的黑盒用例验证状态视为 PASS，
		// 后续修复快照不再重复被挡。黑盒 review 真正 PASS 时清除放行授权。
		blackboxSelected := isSelected(*state, blackboxQAID)
		if blackboxSelected && !blackboxReviewPassed(*state) && state.SnapshotOverride == nil {
			if !userRequested {
				return fmt.Errorf("blackbox QA Review must pass before a development snapshot; development and blackbox QA review both need to complete")
			}
			state.SnapshotOverride = &SnapshotOverride{Origin: "USER", Snapshot: currentSnapshot, Message: strings.TrimSpace(reason)}
		} else if blackboxSelected && blackboxReviewPassed(*state) {
			state.SnapshotOverride = nil
		}
		oldSnapshot := state.CurrentSnapshot
		isRepair := developmentStatus == developmentRepairPrepared ||
			(state.RetainedOverall && (developmentStatus == developmentComplete || developmentStatus == developmentVerified))
		state.CurrentSnapshot = currentSnapshot
		state.Actions["development-worker"] = ActionResult{Status: developmentComplete, DispatchID: developmentDispatch.ID}
		if developmentDispatch.ID != "" {
			completeDispatch(state, developmentDispatch.ID)
		}
		if isRepair {
			state.PreRepairSnapshot = oldSnapshot
		} else {
			state.PreRepairSnapshot = ""
		}
		resetSnapshotReviewSurface(state, oldSnapshot, isRepair, isRepair)
		return nil
	})
}

// resetSnapshotReviewSurface re-opens the post-snapshot review surface shared
// by a development snapshot and an adopted external change: QA Execution is
// kept only when it already passed at the previous snapshot and preserveQA is
// set, every recorded Carry judgment and seal-scoped skip authorization is
// dropped, non-PASS selected gates return to PENDING, and the Carry action is
// re-opened when reopenCarry is set and prior passing gates are eligible.
func resetSnapshotReviewSurface(state *RunState, oldSnapshot string, preserveQA, reopenCarry bool) {
	// 修复快照推进不得抹掉上一快照的权威结果（PASS/FAIL，含快照与 FAIL 用例集）。
	// 在把每个 mode 的 QAExecution 重置为 PENDING 前，若其为权威结果且已落在旧快照，先保留
	// 到该 mode 的 PriorQAExecutionByMode，供重跑识别与 AFFECTED 子集判定
	// 使用；RUNTIME_ERROR 不构成权威结果、直接重置不保留。按 mode 独立
	// 保留与重置，一个 mode 不触碰另一 mode。被新一轮权威结果取代时由 RecordQAExecution
	// 只清空该 mode 的 prior。
	for _, mode := range state.qaExecutionModes() {
		result := state.qaExecution(mode)
		if (result.Status == "PASS" || result.Status == "FAIL") && result.Snapshot != "" && result.Snapshot != state.CurrentSnapshot {
			state.setPriorQAExecution(mode, result)
		}
		if !preserveQA || result.Status != "PASS" || result.Snapshot != oldSnapshot {
			state.setQAExecution(mode, QAExecutionResult{Status: "PENDING"})
		}
	}
	state.Carry = map[string]CarryResult{}
	for id, authorization := range state.SkipAuthorizations {
		if isSealScopedAuthorization(authorization) {
			delete(state.SkipAuthorizations, id)
		}
	}
	for id, result := range state.Gates {
		if !isSelected(*state, id) {
			continue
		}
		if result.Status != "PASS" {
			state.Gates[id] = GateResult{Status: "PENDING"}
		}
	}
	if reopenCarry && len(eligibleCarryGates(*state)) != 0 {
		state.Actions["carry"] = ActionResult{Status: "PENDING"}
	} else {
		delete(state.Actions, "carry")
	}
}

func AuthorizeExtraRepair(root, packageRoot, runID string, cycles int, scopes []QAScopeInput) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
			return err
		}
		if cycles != 1 {
			return fmt.Errorf("each extra repair authorization must add exactly one review wave")
		}
		if state.CompletedReviewWaves < effectiveReviewWaveLimit(*state) {
			return fmt.Errorf("automatic review waves are not exhausted")
		}
		if !hasRepairableBlocker(*state) && !hasSuggestionRecommendation(*state) {
			return fmt.Errorf("no recorded result requires another repair")
		}
		// 轮次上限用尽后每一轮额外修复都须显式授权（carry-forward 不授予轮次，
		// 见 bundleRerunScopes）；QA 被选中且当前快照存在某 mode 的权威 FAIL 结果（该 mode
		// 将重跑）时，scope 决策在同一个交互中打包询问/记录（多 mode 各自一份、可不同）。
		if err := bundleRerunScopes(state, scopes); err != nil {
			return err
		}
		state.ExtraReviewWaves += cycles
		return nil
	})
}

func mutateRun(root, runID string, change func(*RunState) error) (RunState, error) {
	if err := requireRunID(runID); err != nil {
		return RunState{}, err
	}
	path := RunStatePath(root, runID)
	release, err := acquireStateLock(path)
	if err != nil {
		return RunState{}, err
	}
	defer release()
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RunState{}, err
	}
	if err := requireActive(state); err != nil {
		return RunState{}, err
	}
	if err := change(&state); err != nil {
		return RunState{}, err
	}
	if err := SaveRunState(root, state); err != nil {
		return RunState{}, err
	}
	return state, nil
}

// requireRunID rejects a missing run id up front so state-loading/locking paths
// report a friendly "run id is required" instead of a raw state.json.lock /
// state.json file error.
func requireRunID(runID string) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run id is required (--run-id)")
	}
	return nil
}

func acquireStateLock(statePath string) (func(), error) {
	lockPath := statePath + ".lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			file.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for another run-state update")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func requireActive(state RunState) error {
	if state.Status != "ACTIVE" {
		return fmt.Errorf("run %s is %s", state.RunID, state.Status)
	}
	return nil
}

func requireCurrentCatalog(state RunState, packageRoot string) (PromptCatalog, error) {
	catalog, err := LoadPromptCatalog(packageRoot)
	if err != nil {
		return PromptCatalog{}, err
	}
	// Catalog content changes are a recoverable classification, not a run
	// killer: the run continues with the live catalog, per-gate/action deltas
	// are reported, and unaffected results are inherited by a Carry judgment.
	return catalog, nil
}

func requireCurrentDefinitions(root string, state RunState, packageRoot string) (PromptCatalog, error) {
	catalog, err := requireCurrentCatalog(state, packageRoot)
	if err != nil {
		return PromptCatalog{}, err
	}
	changed, err := requirementArtifactsChanged(root, state.RequirementArtifacts)
	if err != nil {
		return PromptCatalog{}, err
	}
	if changed {
		// 需求 6 第 1 条：需求 revision 漂移第一时间硬阻断。任何依赖需求修订的流程命令
		// （prepare-action / prepare-gate / record-action / record-gate / snapshot / seal 等）
		// 在最早执行点即校验需求文档当前内容 hash == run 注册的修订；需求文件被改动导致
		// hash 漂移后，从改动后的第一个流程命令起就拒绝，提示先更新修订，不拖到记录时才
		// 发现。本命令（workflow requirement）不经过本函数，是更新修订的出口。仅 --confirmed
		// 无法更新已变更的 revision，指引须带 --meaning preserved|changed；开发开始后的
		// meaning-preserved 重绑另需 --confirmed。
		return PromptCatalog{}, fmt.Errorf("需求文档已改动，先 `workflow requirement --meaning preserved|changed` 更新修订")
	}
	return catalog, nil
}

func requireNativeCurrent(root string, state RunState) (string, error) {
	current, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return "", err
	}
	if current != state.CurrentSnapshot {
		return "", fmt.Errorf("native VCS identity does not match the current snapshot")
	}
	return current, nil
}

// requireDispatchSourceAncestorOfHead is the relaxed native identity check for
// a dispatch whose documented recovery path permits HEAD to advance after
// prepare. The dispatch source must remain an ancestor (or equal), so unrelated
// history cannot replace its immutable comparison basis.
func requireDispatchSourceAncestorOfHead(root string, state RunState, dispatch PreparedDispatch) error {
	current, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return err
	}
	resolver, err := resolverForVCS(state.VCS, nil)
	if err != nil {
		return err
	}
	if err := resolver.IsAncestorOrEqual(root, dispatch.SourceSnapshot, current); err != nil {
		return fmt.Errorf("native VCS identity does not match the current snapshot")
	}
	return nil
}

func routeForState(root string, state RunState) PromptRoute {
	return PromptRoute{RequirementSource: state.RequirementSource, RequirementRevision: state.RequirementRevision, CatalogRevision: state.CatalogRevision, Worktree: absPath(lifecycle.CleanRoot(root)), VCS: state.VCS, BaseSnapshot: state.BaseSnapshot, CurrentSnapshot: state.CurrentSnapshot, PreRepairSnapshot: state.PreRepairSnapshot, RequirementArtifacts: append([]RequirementArtifact{}, state.RequirementArtifacts...)}
}

func requirePreparedDispatch(state RunState, dispatchID, targetKind, target string) (PreparedDispatch, error) {
	dispatchID = strings.TrimSpace(dispatchID)
	if dispatchID == "" {
		return PreparedDispatch{}, fmt.Errorf("dispatch id is required")
	}
	dispatch, ok := state.Dispatches[dispatchID]
	if !ok {
		return PreparedDispatch{}, fmt.Errorf("unknown dispatch %q", dispatchID)
	}
	if dispatch.TargetKind != targetKind || dispatch.Target != target {
		return PreparedDispatch{}, fmt.Errorf("dispatch %q does not belong to %s %q", dispatchID, targetKind, target)
	}
	recovery := false
	switch dispatch.Status {
	case "CLAIMED":
		if !dispatch.ReviewerRequired || strings.TrimSpace(dispatch.ReviewerIdentity) == "" {
			return PreparedDispatch{}, fmt.Errorf("dispatch %q has no claimed reviewer identity", dispatchID)
		}
	case "OPEN":
		if dispatch.ReviewerRequired {
			return PreparedDispatch{}, fmt.Errorf("dispatch %q is %s and cannot record a result", dispatchID, dispatch.Status)
		}
	case "STALE":
		// 恢复路径：STALE 但审查者已认领（ReviewerIdentity 已绑定）且已产出结果的
		// 派发仍可记录（校验身份与结果内容后接受，不重审）。审查阶段快照未变时 source
		// 绑定匹配、恢复记录落当前快照；非常规快照已变情形由下方 source 绑定校验保守拒绝。
		if !dispatch.ReviewerRequired || strings.TrimSpace(dispatch.ReviewerIdentity) == "" {
			return PreparedDispatch{}, fmt.Errorf("dispatch %q is %s and cannot record a result", dispatchID, dispatch.Status)
		}
		recovery = true
	default:
		return PreparedDispatch{}, fmt.Errorf("dispatch %q is %s and cannot record a result", dispatchID, dispatch.Status)
	}
	// 派发源绑定的陈旧校验按 mode 分叉：黑盒 qa-design/qa-review 绑基线（隔离工作区），
	// 其余派发绑当前快照（主工作区）。
	wantedSource := state.CurrentSnapshot
	if isBlackboxQADispatch(dispatch) {
		wantedSource = state.BaseSnapshot
	}
	if dispatch.RequirementRevision != state.RequirementRevision || dispatch.CatalogRevision != state.CatalogRevision || dispatch.SourceSnapshot != wantedSource {
		return PreparedDispatch{}, fmt.Errorf("dispatch %q has stale source bindings", dispatchID)
	}
	if recovery {
		// 防 STALE 记录与替换派发并行记录双记：同功能替换派发在途（OPEN 空票或 CLAIMED
		// 子代理）时拒绝 STALE 记录——host 应以替换派发为准，避免同一功能两个记录落盘。
		for id, candidate := range state.Dispatches {
			if id == dispatchID {
				continue
			}
			if candidate.TargetKind != targetKind || candidate.Target != target || candidate.Mode != dispatch.Mode {
				continue
			}
			if candidate.Status == "OPEN" || candidate.Status == "CLAIMED" {
				return PreparedDispatch{}, fmt.Errorf("dispatch %q is STALE and has a same-function replacement dispatch %s in flight; record %s instead", dispatchID, id, id)
			}
		}
	}
	return dispatch, nil
}

// requireDispatchNativeCurrent resolves the native identity for a dispatch: a
// blackbox qa-design/qa-review dispatch against the QA isolation worktree
// (== base), every other dispatch against the main worktree (== current).
func requireDispatchNativeCurrent(root string, state RunState, dispatch PreparedDispatch) error {
	if isBlackboxQADispatch(dispatch) {
		return requireIsolatedCurrent(root, state)
	}
	_, err := requireNativeCurrent(root, state)
	return err
}

func completeDispatch(state *RunState, dispatchID string) {
	dispatch := state.Dispatches[dispatchID]
	dispatch.Status = "COMPLETED"
	state.Dispatches[dispatchID] = dispatch
}

// staleOpenDispatches supersedes the prior OPEN empty tickets of the same target
// when a fresh dispatch is claimed. An OPEN dispatch was prepared but
// never claimed, so no subagent was ever dispatched for it (no start event), and
// it must not block or shadow the live claim. Staling is mode-scoped across every
// target: a dispatch whose mode differs from the fresh dispatch's mode
// is a different function (blackbox vs whitebox qa-review / qa-execution, etc.)
// and stays untouched — fixing "whitebox review prepare staled the blackbox
// review". CLAIMED same-function dedup and the manual-termination exception are
// enforced separately at claim time (see ClaimDispatch); prepare no longer calls
// this at all.
func staleOpenDispatches(state *RunState, targetKind, target, mode string) {
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind != targetKind || dispatch.Target != target || dispatch.Status != "OPEN" {
			continue
		}
		if dispatch.Mode != "" && dispatch.Mode != mode {
			continue
		}
		dispatch.Status = "STALE"
		state.Dispatches[id] = dispatch
	}
}

// enforceSameFunctionDedup implements claim-time parallel-dispatch
// guard, the default and only guard against two subagents of the same function
// (same target kind, target, and mode) running at once. Claiming a dispatch is
// rejected when a same-function dispatch is already CLAIMED and its subagent has
// not been terminated (no recorded stop event / interruption reason). A claimed
// dispatch whose subagent was manually terminated (stop event captured) is marked
// STALE so the fresh claim proceeds. OPEN same-function empty tickets are staled
// by staleOpenDispatches and never block a claim (deadlock elimination). The
// staling is transactional: it only persists when the whole claim succeeds.
func enforceSameFunctionDedup(root string, state *RunState, dispatch PreparedDispatch) error {
	staleOpenDispatches(state, dispatch.TargetKind, dispatch.Target, dispatch.Mode)
	for id, prior := range state.Dispatches {
		if id == dispatch.ID {
			continue
		}
		if prior.TargetKind != dispatch.TargetKind || prior.Target != dispatch.Target || prior.Mode != dispatch.Mode {
			continue
		}
		if prior.Status != "CLAIMED" {
			continue
		}
		// 手动终止例外（不新增 CLI 命令）：主代理直接终结前一个同功能子代理后，
		// 生命周期已捕获其 stop 事件并记录中断原因；读得中断原因即把前派发标 STALE、允许
		// 认领该新派发。读不到原因（子代理仍在途）时默认拒绝两个同功能子代理并行。
		reason, err := workflowLifecycle.InterruptionReason(root, state.RunID, id)
		if err != nil {
			return err
		}
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("a claimed %s %q dispatch %s is already in flight for the same function; resume the original agent or terminate its subagent (recording the interruption reason) before claiming a fresh dispatch", dispatch.TargetKind, dispatch.Target, id)
		}
		prior.Status = "STALE"
		state.Dispatches[id] = prior
	}
	return nil
}

func nextDispatchAttempt(state RunState, targetKind, target string, wave int) int {
	attempt := 1
	for _, dispatch := range state.Dispatches {
		if dispatch.TargetKind == targetKind && dispatch.Target == target && dispatch.ReviewWave == wave && dispatch.Attempt >= attempt {
			attempt = dispatch.Attempt + 1
		}
	}
	return attempt
}

func currentGateReviewWave(state RunState) int {
	if state.CompletedReviewWaves > 0 && state.Actions["development-worker"].Status == developmentVerified {
		return state.CompletedReviewWaves
	}
	return state.CompletedReviewWaves + 1
}

func newDispatchID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "dispatch-" + hex.EncodeToString(value[:]), nil
}

func semanticActionResult(actionID, status, message string, findings []FindingInput, state *RunState) (ActionResult, error) {
	normalized, converted, err := validateSemanticResult(actionID, status, message, findings, false)
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Status: normalized, Message: strings.TrimSpace(message), Findings: converted}, nil
}

func semanticGateResult(status, message string, findings []FindingInput, state *RunState) (GateResult, error) {
	normalized, converted, err := validateSemanticResult("", status, message, findings, true)
	if err != nil {
		return GateResult{}, err
	}
	return GateResult{Status: normalized, Message: strings.TrimSpace(message), Snapshot: state.CurrentSnapshot, Findings: converted}, nil
}

func validateSemanticResult(actionID, status, message string, findings []FindingInput, gateResult bool) (string, []Finding, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "PASS" && status != "FAIL" && status != "RUNTIME_ERROR" {
		return "", nil, fmt.Errorf("status must be PASS, FAIL, or RUNTIME_ERROR")
	}
	reviewAction := actionID == "product-review" || actionID == "start-readiness"
	if status == "PASS" && len(findings) != 0 && !gateResult && !reviewAction {
		return "", nil, fmt.Errorf("PASS cannot include findings")
	}
	if status == "FAIL" && len(findings) == 0 {
		return "", nil, fmt.Errorf("FAIL requires at least one finding")
	}
	if status == "RUNTIME_ERROR" {
		if len(findings) != 0 {
			return "", nil, fmt.Errorf("RUNTIME_ERROR cannot include reviewer findings")
		}
		if strings.TrimSpace(message) == "" {
			return "", nil, fmt.Errorf("RUNTIME_ERROR requires a message")
		}
	}
	converted := make([]Finding, 0, len(findings))
	hasBlocking := false
	for _, input := range findings {
		if strings.TrimSpace(input.Message) == "" {
			return "", nil, fmt.Errorf("finding message is required")
		}
		locations := make([]string, 0, len(input.Locations))
		for _, location := range input.Locations {
			if err := validateFindingLocation(location); err != nil {
				return "", nil, err
			}
			locations = append(locations, strings.TrimSpace(location))
		}
		severity := strings.ToUpper(strings.TrimSpace(input.Severity))
		if gateResult || reviewAction {
			// 门与 product-review / start-readiness 的发现项必须分级 P0/P1/P2/P3（非空）；
			// 仅 P2/P3 的审查记录 PASS 且 P2/P3 建议可见，存在 P0/P1 时记录 FAIL（驳回的
			// P0/P1 由 enforceReviewRule 放行，确认的 P0/P1 置位需重审标记）。
			if severity != "P0" && severity != "P1" && severity != "P2" && severity != "P3" {
				if gateResult {
					return "", nil, fmt.Errorf("gate finding severity must be P0, P1, P2, or P3")
				}
				return "", nil, fmt.Errorf("review finding severity must be P0, P1, P2, or P3")
			}
			if severity == "P0" || severity == "P1" {
				hasBlocking = true
			}
		} else if severity != "" {
			return "", nil, fmt.Errorf("severity is accepted only for discovered-gate findings or product/start-readiness review findings")
		}
		converted = append(converted, Finding{Severity: severity, Message: strings.TrimSpace(input.Message), Locations: locations})
	}
	// 门的 PASS 只允许 P2/P3；product-review/start-readiness 的 PASS 允许携带发现项（仅
	// P2/P3 或已驳回的 P0/P1，由 enforceReviewRule 逐项判定），FAIL 两者都必须含 P0/P1。
	if gateResult && status == "PASS" && hasBlocking {
		return "", nil, fmt.Errorf("PASS can include only P2/P3 findings")
	}
	if (gateResult || reviewAction) && status == "FAIL" && !hasBlocking {
		return "", nil, fmt.Errorf("FAIL requires at least one P0 or P1 finding")
	}
	return status, converted, nil
}

func validateFindingLocation(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("finding location is empty")
	}
	if strings.Contains(value, `\`) || strings.Contains(value, "://") || strings.HasPrefix(value, "/") || (len(value) > 1 && value[1] == ':') {
		return fmt.Errorf("finding location must be repository-relative: %s", value)
	}
	path := value
	for count := 0; count < 2; count++ {
		index := strings.LastIndex(path, ":")
		if index <= 0 {
			break
		}
		if !suffixIsDigits(path[index+1:]) {
			break
		}
		path = path[:index]
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("finding location must be repository-relative: %s", value)
	}
	return nil
}

func rejectFrozenArtifactFindings(state RunState, findings []Finding) error {
	excluded := map[string]bool{}
	for _, artifact := range state.RequirementArtifacts {
		excluded[artifact.Path] = true
	}
	for _, finding := range findings {
		for _, location := range finding.Locations {
			path := location
			for count := 0; count < 2; count++ {
				index := strings.LastIndex(path, ":")
				if index <= 0 || !suffixIsDigits(path[index+1:]) {
					break
				}
				path = path[:index]
			}
			if excluded[filepath.ToSlash(filepath.Clean(path))] {
				return fmt.Errorf("finding location %s is a frozen acceptance artifact and not a review target", location)
			}
		}
	}
	return nil
}

func suffixIsDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func invalidateRequirementResults(state *RunState, gateIDs []string) {
	routeMode := state.RouteMode
	selected := append([]string{}, state.SelectedGates...)
	routeSkips := map[string]SkipAuthorization{}
	for id, authorization := range state.SkipAuthorizations {
		if authorization.Origin == "ROUTE" {
			routeSkips[id] = authorization
		}
	}
	state.Actions = pendingRequirementActions()
	// 语义变更作废全部结果时，per-mode review/design 权威结果一并作废——qa-review /
	// qa-design 已移出 Actions（按 mode 存储），失效路径只重置 Actions 会让 per-mode
	// 旧 PASS 残留，快照黑盒门读到旧 PASS 仍放行。这里清空两个按 mode 的权威结果 map，并把
	// 各 mode 用例的 ReviewStatus 置回 PENDING，使门读到旧 PASS 不放行、须对新需求重新设计/重审。
	state.QAReviewByMode = map[string]ActionResult{}
	state.QADesignByMode = map[string]ActionResult{}
	// 需求作废重置：一并清空按 mode 的增量变更留痕（本轮新增/修改/删除列表随结果作废）。
	state.QADesignChangesByMode = map[string]QADesignChange{}
	if len(state.QACasesByMode) != 0 {
		reset := map[string][]QACase{}
		for mode, cases := range state.QACasesByMode {
			updated := make([]QACase, 0, len(cases))
			for _, testCase := range cases {
				testCase.ReviewStatus = "PENDING"
				updated = append(updated, testCase)
			}
			reset[mode] = updated
		}
		state.QACasesByMode = reset
	}
	state.QAExecutionByMode = map[string]QAExecutionResult{}
	state.Carry = map[string]CarryResult{}
	// 需求作废重置：一并清空 scope 决策与上一轮执行结果（P2-丙确认：两者同属"上一轮/
	// 历史执行上下文"，随重置对称清除，防止残留决策污染新一轮重跑）。
	state.ExecutionScopes = map[string]QAExecutionScope{}
	state.PriorQAExecutionByMode = map[string]*QAExecutionResult{}
	state.PreRepairSnapshot = ""
	// 语义已变的需求修订改变了原决定的前提，已拍板发现项清单随之清空，允许重新提出。
	state.SettledFindings = map[string][]SettledFinding{}
	// 需求作废重置：一并清空黑盒隔离工作区、快照放行授权、连续 FAIL 计数与"需重审"标记。
	state.QAWorktree = ""
	state.SnapshotOverride = nil
	state.BlackboxReviewFails = 0
	state.NeedsReReview = map[string]string{}
	state.ReReviewDispatch = map[string]string{}
	state.ReviewOverrides = map[string]string{}
	// meaning-changed 语义变更清空逐项审查表，全量重审；meaning-preserved 重绑
	// 不清表（rebindCurrentSnapshot 不触碰本表）。
	state.ReviewItemsByAction = map[string]map[string]ReviewItem{}
	state.Gates = map[string]GateResult{}
	for _, id := range gateIDs {
		state.Gates[id] = GateResult{Status: "PENDING"}
	}
	state.RouteMode = routeMode
	state.SelectedGates = selected
	state.SkipAuthorizations = routeSkips
	state.CompletedReviewWaves = 0
	state.ExtraReviewWaves = 0
	staleAllDispatches(state)
}

func rebindCurrentSnapshot(state *RunState, snapshot string) {
	previous := state.CurrentSnapshot
	state.CurrentSnapshot = snapshot
	if previous == snapshot {
		return
	}
	staleAllDispatches(state)
	// 每个 mode 的执行结果快照各自重绑（合并空 mode 一并处理）。
	for _, mode := range state.qaExecutionModes() {
		result := state.qaExecution(mode)
		if result.Snapshot == previous {
			result.Snapshot = snapshot
			state.setQAExecution(mode, result)
		}
	}
	for id, result := range state.Gates {
		if result.Snapshot == previous {
			result.Snapshot = snapshot
			state.Gates[id] = result
		}
	}
	for id, authorization := range state.SkipAuthorizations {
		if isSealScopedAuthorization(authorization) && authorization.Snapshot == previous {
			authorization.Snapshot = snapshot
			state.SkipAuthorizations[id] = authorization
		}
	}
	for id, result := range state.Carry {
		if result.TargetSnapshot == previous {
			result.TargetSnapshot = snapshot
			state.Carry[id] = result
		}
	}
}

// runHasAction reports whether the run's state carries the named action. A run
// seeds its actions from the catalog at start; a run started before an action
// was added to the catalog carries no entry for it.
func runHasAction(state RunState, actionID string) bool {
	_, ok := state.Actions[actionID]
	return ok
}

// actionPassedOrAbsent reports whether the named pre-development action does
// not gate this run: it is absent from the run (predating run), has recorded
// PASS, or the run is a slice instance that inherited the overall-level review.
// Unselected action changes must not block an existing run.
func actionPassedOrAbsent(state RunState, actionID string) bool {
	if !runHasAction(state, actionID) || state.Actions[actionID].Status == "PASS" {
		return true
	}
	return inheritedReview(state, actionID)
}

// inheritedReview reports whether the run's split record names the action as an
// overall-level review inherited by a slice instance (product-review /
// start-readiness from the retained overall run).
func inheritedReview(state RunState, actionID string) bool {
	if state.Slicing == nil {
		return false
	}
	for _, id := range state.Slicing.InheritedReviews {
		if id == actionID {
			return true
		}
	}
	return false
}

// reviewerActionIDs are the reviewer actions whose prompt changes with a
// recorded result create an inheritance judgment ("动作提示词" scope,
// mirroring contamination-check coverage). The non-reviewer actions
// (development worker, qa executor) and the carry disposition command are
// excluded: their changes are not zero-context review results that a Carry
// judgment re-decides, and carrying the carry action itself would deadlock (a
// recorded carry cannot be re-judged by carry --main-agent).
var reviewerActionIDs = map[string]bool{
	"requirements-clarification": true,
	"product-review":             true,
	"start-readiness":            true,
	"qa-design":                  true,
	"qa-review":                  true,
}

func normalizeSelected(values, candidates []string) ([]string, error) {
	allowed := map[string]bool{}
	for _, id := range candidates {
		allowed[id] = true
	}
	chosen := map[string]bool{}
	for _, value := range values {
		id := strings.TrimSpace(value)
		if !allowed[id] {
			return nil, fmt.Errorf("gate %q is not in the current route candidates", id)
		}
		if chosen[id] {
			return nil, fmt.Errorf("duplicate selected gate %q", id)
		}
		chosen[id] = true
	}
	return orderedSelection(chosen, candidates), nil
}

func orderedSelection(chosen map[string]bool, candidates []string) []string {
	selected := []string{}
	for _, id := range candidates {
		if chosen[id] {
			selected = append(selected, id)
		}
	}
	return selected
}

func selectedSet(state RunState) map[string]bool {
	selected := map[string]bool{}
	for _, id := range state.SelectedGates {
		selected[id] = true
	}
	return selected
}

func isSelected(state RunState, id string) bool { return selectedSet(state)[id] }

func reviewWaveRecorded(state RunState) bool {
	// 波次完成前按选中 mode 校验各 mode 均已记录执行——黑盒/
	// 白盒各自独立派发、各自记录到自己的执行结果；若某选中 mode 从未派发记录，其用例与
	// 发现项被静默跳过，波次不得视为已记录。
	if !selectedQAModesRecorded(state) {
		return false
	}
	for id := range selectedSet(state) {
		if isQAMode(id) {
			continue
		}
		result := state.Gates[id]
		if result.Snapshot != state.CurrentSnapshot || result.Status == "PENDING" || result.Status == "" {
			return false
		}
	}
	return true
}

// selectedQAModesRecorded reports whether every selected QA mode has recorded
// execution at the current snapshot (per-mode storage): blackbox and
// whitebox each record their own result, and a wave may only complete (and Seal)
// once every selected mode has recorded execution, so one mode cannot be silently
// skipped while the other passes. A mode with zero cases has nothing to execute
// (the qa-review set-level decision judged the empty set), so it is trivially
// recorded. Merge QA uses the merged (empty-mode) result. A RUNTIME_ERROR result
// is a recorded outcome and blocks through the existing
// skip-authorization path, so it is not treated as a missing mode here.
func selectedQAModesRecorded(state RunState) bool {
	if !isSelectedQA(state) {
		return true
	}
	for id := range selectedSet(state) {
		if !isQAMode(id) {
			continue
		}
		if !qaModeRecordedAtCurrent(state, qaDispatchMode(id)) {
			return false
		}
	}
	return true
}

func hasRepairableBlocker(state RunState) bool {
	// 任一选中 QA mode 当前快照有权威 FAIL 结果即阻塞（黑盒/白盒各自独立，合并
	// 单派发结果按 mode 生效）。
	if isSelectedQA(state) {
		for id := range selectedSet(state) {
			if !isQAMode(id) {
				continue
			}
			result := qaModeResult(state, qaDispatchMode(id))
			if result.Status == "FAIL" && result.Snapshot == state.CurrentSnapshot {
				return true
			}
		}
	}
	for id := range selectedSet(state) {
		if isQAMode(id) {
			continue
		}
		if state.Gates[id].Status == "FAIL" && state.Gates[id].Snapshot == state.CurrentSnapshot {
			return true
		}
	}
	return false
}

func hasSuggestionRecommendation(state RunState) bool {
	for id := range selectedSet(state) {
		if isQAMode(id) {
			continue
		}
		result := state.Gates[id]
		if result.Snapshot != state.CurrentSnapshot {
			continue
		}
		for _, finding := range result.Findings {
			if finding.Severity == "P2" || finding.Severity == "P3" {
				return true
			}
		}
	}
	return false
}

func hasSelectedRuntimeError(state RunState) bool {
	for id := range selectedSet(state) {
		if selectedResultStatus(state, id) == "RUNTIME_ERROR" {
			return true
		}
	}
	return false
}

func runtimeErrorsAuthorizedForRepair(state RunState) bool {
	foundRuntime := false
	for id := range selectedSet(state) {
		if selectedResultStatus(state, id) != "RUNTIME_ERROR" {
			continue
		}
		foundRuntime = true
		authorization, ok := state.SkipAuthorizations[id]
		if !ok || authorization.Origin != "SEAL" || authorization.Status != "RUNTIME_ERROR" || authorization.Snapshot != state.CurrentSnapshot {
			return false
		}
	}
	return foundRuntime
}

func repairInput(state RunState) string {
	lines := []string{"Repair the complete recorded wave below. P2/P3 recommendations are included whenever this wave has a blocker or the user explicitly requested their repair."}
	// 收集每个 FAIL 的选中 QA mode 的发现项（黑盒/白盒各自独立，合并单派发按
	// mode 生效）。
	if isSelectedQA(state) {
		for _, id := range state.SelectedGates {
			if !isQAMode(id) {
				continue
			}
			result := qaModeResult(state, qaDispatchMode(id))
			if result.Status != "FAIL" {
				continue
			}
			for _, finding := range result.Findings {
				lines = append(lines, "QA FAIL: "+finding.Message)
			}
		}
	}
	for _, id := range state.SelectedGates {
		if isQAMode(id) {
			continue
		}
		for _, finding := range state.Gates[id].Findings {
			lines = append(lines, fmt.Sprintf("%s %s: %s", id, finding.Severity, finding.Message))
		}
	}
	return strings.Join(lines, "\n")
}

func effectiveReviewWaveLimit(state RunState) int {
	return automaticReviewWaveLimit + state.ExtraReviewWaves
}

func completeReviewWaveIfReady(state *RunState) {
	if len(state.SelectedGates) == 0 || state.Actions["development-worker"].Status != developmentComplete || !reviewWaveRecorded(*state) {
		clearResolvedCarryBoundary(state)
		return
	}
	// 任一选中 QA mode 为 RUNTIME_ERROR 即不自动完成波次（走既有 skip 授权路径）。
	if isSelectedQA(*state) {
		for id := range selectedSet(*state) {
			if !isQAMode(id) {
				continue
			}
			if qaModeResult(*state, qaDispatchMode(id)).Status == "RUNTIME_ERROR" {
				return
			}
		}
	}
	for id := range selectedSet(*state) {
		if isQAMode(id) {
			continue
		}
		if state.Gates[id].Status == "RUNTIME_ERROR" {
			return
		}
	}
	if len(eligibleCarryGates(*state)) != 0 || state.Actions["carry"].Status == "RUNTIME_ERROR" {
		return
	}
	state.CompletedReviewWaves++
	state.Actions["development-worker"] = ActionResult{Status: developmentVerified}
	state.PreRepairSnapshot = ""
}

func resolveFromRoot(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, filepath.FromSlash(path))
}

func absPath(path string) string {
	full, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(full)
}

func newRunID() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return strings.ToLower(time.Now().UTC().Format("20060102t150405000z")) + "-" + hex.EncodeToString(suffix[:]), nil
}

func requirementArtifactSet(root, primary string, additional []string) ([]RequirementArtifact, error) {
	root = cleanWorktree(root)
	paths := append([]string{primary}, additional...)
	seen := map[string]bool{}
	artifacts := make([]RequirementArtifact, 0, len(paths))
	for _, raw := range paths {
		path, err := validatedArtifactPath(root, raw)
		if err != nil {
			return nil, err
		}
		if seen[path] {
			return nil, fmt.Errorf("duplicate requirement artifact %q", path)
		}
		seen[path] = true
		revision, err := RequirementRevision(resolveFromRoot(root, path))
		if err != nil {
			return nil, fmt.Errorf("requirement artifact %s: %w", path, err)
		}
		artifacts = append(artifacts, RequirementArtifact{Path: path, Revision: revision})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func validatedArtifactPath(root, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("requirement artifact path is required")
	}
	full := resolveFromRoot(root, strings.TrimSpace(raw))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("requirement artifact must be a file under the repository root: %s", raw)
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("requirement artifact %s: %w", filepath.ToSlash(rel), err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("requirement artifact %s is not a regular file", filepath.ToSlash(rel))
	}
	return filepath.ToSlash(filepath.Clean(rel)), nil
}

func normalizeArtifactPath(root, raw string) string {
	full := resolveFromRoot(cleanWorktree(root), strings.TrimSpace(raw))
	rel, err := filepath.Rel(cleanWorktree(root), full)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(raw))
	}
	return filepath.ToSlash(filepath.Clean(rel))
}

func artifactRevision(artifacts []RequirementArtifact, path string) string {
	for _, artifact := range artifacts {
		if artifact.Path == path {
			return artifact.Revision
		}
	}
	return ""
}

func requirementArtifactsChanged(root string, artifacts []RequirementArtifact) (bool, error) {
	if len(artifacts) == 0 {
		return false, fmt.Errorf("requirement artifact set is empty")
	}
	for _, artifact := range artifacts {
		revision, err := RequirementRevision(resolveFromRoot(cleanWorktree(root), artifact.Path))
		if err != nil {
			return false, fmt.Errorf("requirement artifact %s: %w", artifact.Path, err)
		}
		if revision != artifact.Revision {
			return true, nil
		}
	}
	return false, nil
}

func sameArtifactSet(left, right []RequirementArtifact) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
