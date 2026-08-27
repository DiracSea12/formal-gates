package validate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"formal-gates/internal/lifecycle"
)

type InstallOptions struct {
	Source  string
	Host    string
	Scope   string
	Project string
	// ReleaseRoot and BinaryTarget are used by the bootstrap scripts. When
	// provided, the native installer owns release copy, backup, and executable
	// replacement in the same transaction as host installation.
	ReleaseRoot  string
	BinaryTarget string
	RegistryPath string
	Bootstrap    bool
	Force        bool
	SkipHooks    bool
}

type UninstallOptions struct {
	Host         string
	Scope        string
	Project      string
	RegistryPath string
}

type InstallReport struct {
	Targets              []InstallTargetReport `json:"targets"`
	GeneratedAt          string                `json:"generatedAt,omitempty"`
	Registry             string                `json:"registry,omitempty"`
	RegistryEpoch        uint64                `json:"registryEpoch,omitempty"`
	ReceiptPath          string                `json:"receiptPath,omitempty"`
	BootstrapReceiptPath string                `json:"bootstrapReceiptPath,omitempty"`
	VCSIdentity          string                `json:"vcsIdentity,omitempty"`
	PackageDigest        string                `json:"packageDigest,omitempty"`
}

type InstallTargetReport struct {
	Host              string            `json:"host"`
	TargetPath        string            `json:"targetPath"`
	LauncherPath      string            `json:"launcherPath,omitempty"`
	HookConfig        string            `json:"hookConfig,omitempty"`
	ManagedRulePath   string            `json:"managedRulePath,omitempty"`
	SourceRoot        string            `json:"sourceRoot,omitempty"`
	SourceDigest      string            `json:"sourceDigest,omitempty"`
	InstalledDigest   string            `json:"installedDigest,omitempty"`
	HookDigest        string            `json:"hookDigest,omitempty"`
	ManagedRuleDigest string            `json:"managedRuleDigest,omitempty"`
	ReleaseRoot       string            `json:"releaseRoot,omitempty"`
	RegistryRecordID  string            `json:"registryRecordId,omitempty"`
	RegistryEpoch     uint64            `json:"registryEpoch,omitempty"`
	VCSIdentity       string            `json:"vcsIdentity,omitempty"`
	PackageDigest     string            `json:"packageDigest,omitempty"`
	SourceLstat       PathLstat         `json:"sourceLstat"`
	InstalledLstat    PathLstat         `json:"installedLstat"`
	Disjoint          map[string]string `json:"disjoint"`
	DisjointProof     map[string]string `json:"disjointProof"`
	HookAction        string            `json:"hookAction,omitempty"`
	ManagedRuleAction string            `json:"managedRuleAction,omitempty"`
	Manifest          []PackageEntry    `json:"manifest,omitempty"`
	CanonicalPaths    map[string]string `json:"canonicalPaths,omitempty"`
	Smoke             string            `json:"smoke,omitempty"`
}

type BootstrapReceipt struct {
	Operation     string           `json:"operation"`
	Code          string           `json:"code,omitempty"`
	Accepted      bool             `json:"accepted"`
	Status        string           `json:"status,omitempty"`
	Registry      string           `json:"registry"`
	Epoch         uint64           `json:"epoch"`
	PackageDigest string           `json:"packageDigest"`
	VCSIdentity   string           `json:"vcsIdentity"`
	SourceRoot    string           `json:"sourceRoot"`
	SourceLstat   PathLstat        `json:"sourceLstat"`
	Records       []RegistryRecord `json:"records"`
	StateCreated  bool             `json:"stateCreated"`
	Reason        string           `json:"reason,omitempty"`
	ObservedAt    string           `json:"observedAt"`
}

// PathLstat is the immutable filesystem identity recorded in install receipts.
// It deliberately uses Lstat-derived metadata and a canonical real path so a
// receipt cannot silently describe a symlink or a different installed object.
type PathLstat struct {
	Path     string `json:"path"`
	RealPath string `json:"realPath"`
	Mode     uint32 `json:"mode"`
	Size     int64  `json:"size"`
	Kind     string `json:"kind"`
	Digest   string `json:"digest,omitempty"`
}

type UninstallReport struct {
	Targets []InstallTargetReport `json:"targets"`
}

type installTarget struct {
	host       string
	targetPath string
	// launcherPath is the fixed executable that host hooks use.  It is never the
	// replaceable binary inside targetPath.
	launcherPath    string
	hookConfig      string
	managedRulePath string
}

var installRuntimeEntries = []string{
	"SKILL.md",
	"README.md",
	"README_EN.md",
	"formal-gates.manifest.json",
	"bin",
	"agents",
	"prompts",
	"gates",
	"references",
	"definitions",
}

// Complete packages keep the source files required by the installed-binary
// package validator. Runtime-only test fixtures may omit these entries.
var installPackageEntries = []string{
	"go.mod",
	".github/workflows/portable-validation.yml",
	"cmd",
	"internal",
}

func Install(options InstallOptions) (InstallReport, error) {
	if strings.TrimSpace(options.Source) == "" {
		return InstallReport{}, fmt.Errorf("formal-gates source is required (--source); it must point at the package directory to install")
	}
	source := lifecycle.CleanRoot(options.Source)
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return InstallReport{}, err
	}
	sourceAbs = filepath.Clean(sourceAbs)
	if err := assertInstallSource(sourceAbs); err != nil {
		return InstallReport{}, err
	}
	targets, err := resolveInstallTargets(options.Host, options.Scope, options.Project)
	if err != nil {
		return InstallReport{}, err
	}
	// A stable launcher is a public pointer, separate from every replaceable
	// host payload.  Release bootstrap callers can choose the documented path;
	// direct installs use one fixed launcher namespace for their scope.
	for index := range targets {
		if strings.TrimSpace(options.BinaryTarget) != "" {
			targets[index].launcherPath = stableLauncherPath(options.BinaryTarget)
		} else {
			targets[index].launcherPath = defaultStableLauncherPath(options)
		}
	}
	for _, target := range targets {
		// A frozen installed artifact is allowed to bootstrap its own admission
		// record. Normal installs still require a disjoint source and target;
		// bootstrap is read-only and must not be rejected merely because the
		// stable artifact is the target being admitted.
		if !options.Bootstrap && pathOverlaps(canonicalRegistryPath(sourceAbs), canonicalRegistryPath(target.targetPath)) {
			return InstallReport{}, fmt.Errorf("install source %s overlaps target %s", sourceAbs, target.targetPath)
		}
		if !options.Bootstrap && pathOverlaps(canonicalRegistryPath(sourceAbs), canonicalRegistryPath(target.launcherPath)) {
			return InstallReport{}, fmt.Errorf("install source %s overlaps stable launcher %s", sourceAbs, target.launcherPath)
		}
	}
	if strings.TrimSpace(options.ReleaseRoot) != "" {
		// Resolve the documented relative option against the process working
		// directory before comparing it with the source.  Comparing an absolute
		// source with a raw relative release path lets `releaseRoot=./release`
		// slip through and makes the copy walk recurse into its own temp tree.
		releaseAbs, absErr := filepath.Abs(options.ReleaseRoot)
		if absErr != nil {
			return InstallReport{}, absErr
		}
		if !options.Bootstrap && pathOverlaps(canonicalRegistryPath(sourceAbs), canonicalRegistryPath(releaseAbs)) {
			return InstallReport{}, fmt.Errorf("install source %s overlaps release root %s", sourceAbs, filepath.Clean(releaseAbs))
		}
		if strings.TrimSpace(options.BinaryTarget) != "" {
			binaryAbs, binaryErr := filepath.Abs(options.BinaryTarget)
			if binaryErr != nil {
				return InstallReport{}, binaryErr
			}
			if pathOverlaps(canonicalRegistryPath(releaseAbs), canonicalRegistryPath(binaryAbs)) {
				return InstallReport{}, fmt.Errorf("release root %s overlaps binary target %s", filepath.Clean(releaseAbs), filepath.Clean(binaryAbs))
			}
		}
	}
	sourcePackage, err := PackageReceipt(sourceAbs)
	if err != nil {
		return InstallReport{}, fmt.Errorf("formal-gates source failed immutable package validation: %w", err)
	}
	registryPath := installRegistryPath(options)
	for _, target := range targets {
		record := installRegistryRecord(target, options)
		namespaces := map[string]string{
			"sourceRoot": canonicalRegistryPath(sourceAbs),
		}
		for name, path := range record.CanonicalPaths {
			namespaces[name] = path
		}
		if registryPath != "" {
			namespaces["registry"] = canonicalRegistryPath(registryPath)
		}
		if strings.TrimSpace(options.ReleaseRoot) != "" {
			namespaces["releaseRoot"] = canonicalRegistryPath(options.ReleaseRoot)
		}
		if !options.Bootstrap {
			if err := validateCanonicalNamespaceRelations(namespaces, "install"); err != nil {
				return InstallReport{}, err
			}
		}
	}
	// A source checkout/release must pass the complete package contract.  The
	// documented runtime-only install fixture intentionally omits development
	// sources, so it receives the manifest checks below without being rejected
	// for files that are not part of the copied runtime subset.
	completePackage := isFile(filepath.Join(sourceAbs, "internal", "validate", "runner.go"))
	if completePackage {
		if packageResult := Package(sourceAbs); !packageResult.OK() {
			return InstallReport{}, fmt.Errorf("formal-gates source failed complete package validation: %s", resultSummary(packageResult))
		}
	}
	manifestData, readErr := os.ReadFile(filepath.Join(sourceAbs, "formal-gates.manifest.json"))
	if readErr != nil {
		return InstallReport{}, fmt.Errorf("formal-gates manifest cannot be read: %w", readErr)
	}
	var manifestShape manifest
	if unmarshalErr := json.Unmarshal(manifestData, &manifestShape); unmarshalErr != nil {
		return InstallReport{}, fmt.Errorf("formal-gates manifest JSON is invalid: %w", unmarshalErr)
	}
	if manifestShape.Name != "formal-gates" {
		return InstallReport{}, fmt.Errorf("formal-gates manifest name must be formal-gates")
	}
	if unknown := unknownManifestHosts(manifestShape.Hosts); len(unknown) > 0 {
		return InstallReport{}, fmt.Errorf("formal-gates manifest lists unsupported host target %q", unknown[0])
	}
	if unknown := unknownManifestParts(manifestShape.Parts); len(unknown) > 0 {
		return InstallReport{}, fmt.Errorf("formal-gates manifest lists unsupported package target %q", unknown[0])
	}
	// An install target is only valid when this payload's manifest registers
	// the requested host as an installable host target. A manifest that omits
	// the host or downgrades it to explanation-level support describes a
	// payload that must not install there; accepting it would create an
	// unregistered target with exit 0 before any state exists to notice.
	if unregistered := unregisteredManifestInstallHosts(manifestShape.Hosts, targets); len(unregistered) > 0 {
		return InstallReport{}, fmt.Errorf("formal-gates manifest does not register %q as an installable host target (support \"host-target\"); refusing the unknown install target", unregistered[0])
	}
	if options.Bootstrap {
		return bootstrapInstall(options, targets, sourcePackage, registryPath)
	}
	rule, err := LoadManagedRule(sourceAbs)
	if err != nil {
		return InstallReport{}, err
	}
	vcsIdentity := sourceVCSIdentity(sourceAbs, sourcePackage.Digest)
	report := InstallReport{GeneratedAt: "sha256:" + sourcePackage.Digest, VCSIdentity: vcsIdentity, PackageDigest: sourcePackage.Digest}
	report.Registry = filepath.ToSlash(registryPath)
	if registryPath != "" {
		report.ReceiptPath = filepath.ToSlash(registryPath + ".install.json")
	}
	var installUnlock func()
	var registryUnlock func()
	if registryPath != "" {
		installUnlock, err = acquireInstallLock(registryPath)
		if err != nil {
			return InstallReport{}, err
		}
		defer installUnlock()
		registryUnlock, err = acquireRegistryLock(registryPath)
		if err != nil {
			return InstallReport{}, err
		}
		defer registryUnlock()
		// Recover an interrupted multi-target transaction before validating the
		// bridge or touching a new target.  The outer journal owns all runtime,
		// host-config and registry snapshots, so host-both cannot resume in a
		// mixed state after a process crash.
		if err := reconcileOuterInstallJournal(registryPath); err != nil {
			return InstallReport{}, err
		}
		// Validate the existing bridge before changing any host target.  A
		// malformed registry is an unregistered install, not a reason to leave
		// a partially installed runtime behind.
		if _, loadErr := LoadRegistry(registryPath); loadErr != nil && !os.IsNotExist(loadErr) {
			return InstallReport{}, fmt.Errorf("registry admission bridge is unavailable: %w", loadErr)
		}
	}

	transactionParent := filepath.Dir(registryPath)
	if transactionParent == "." || transactionParent == "" {
		transactionParent = filepath.Dir(targets[0].targetPath)
	}
	for !exists(transactionParent) {
		parent := filepath.Dir(transactionParent)
		if parent == transactionParent {
			break
		}
		transactionParent = parent
	}
	transactionRoot, err := os.MkdirTemp(transactionParent, ".formal-gates-transaction-")
	if err != nil {
		return InstallReport{}, err
	}
	defer os.RemoveAll(transactionRoot)
	outerPath := installOuterJournalPath(registryPath)
	outer := outerInstallJournal{Operation: "install", RegistryPath: registryPath, TransactionRoot: transactionRoot, Phase: "intent", CreatedAt: nowReceiptTime()}
	outer.VCSIdentity = vcsIdentity
	outer.PackageDigest = sourcePackage.Digest
	if len(targets) > 0 {
		outer.InstalledTarget = canonicalRegistryPath(targets[0].targetPath)
	}
	if registryPath != "" {
		if existing, loadErr := loadRegistryForCommit(registryPath); loadErr == nil {
			outer.Generation = existing.Epoch + 1
		}
	}
	if outer.Generation == 0 {
		outer.Generation = 1
	}
	outer.Lease = fmt.Sprintf("lease-%d", outer.Generation)
	outer.Token = fmt.Sprintf("token-%d", time.Now().UnixNano())
	outer.Registry, err = snapshotOuterFile(registryPath, filepath.Join(transactionRoot, "registry.before"))
	if err != nil {
		return InstallReport{}, err
	}
	outer.Receipt, err = snapshotOuterFile(filepath.FromSlash(report.ReceiptPath), filepath.Join(transactionRoot, "install-receipt.before"))
	if err != nil {
		return InstallReport{}, err
	}
	for index, target := range targets {
		backupRoot := filepath.Join(transactionRoot, fmt.Sprintf("target-%d", index))
		tree, backupErr := snapshotInstallTree(target.targetPath, filepath.Join(backupRoot, "runtime"))
		if backupErr != nil {
			return InstallReport{}, backupErr
		}
		hook, backupErr := snapshotOuterFile(target.hookConfig, filepath.Join(backupRoot, "hook.before"))
		if backupErr != nil {
			return InstallReport{}, backupErr
		}
		rule, backupErr := snapshotOuterFile(target.managedRulePath, filepath.Join(backupRoot, "rule.before"))
		if backupErr != nil {
			return InstallReport{}, backupErr
		}
		resourceRoot := installRegistryRecord(target, options).ResourceRoot
		outer.Targets = append(outer.Targets, outerTargetSnapshot{TargetPath: target.targetPath, HookPath: target.hookConfig, RulePath: target.managedRulePath, ResourcePath: resourceRoot, ResourceExisted: exists(resourceRoot), Tree: outerTreeFromBackup(tree), Hook: hook, Rule: rule})
	}
	var releaseBackup installTreeBackup
	if strings.TrimSpace(options.ReleaseRoot) != "" {
		releaseBackup, err = snapshotInstallTree(options.ReleaseRoot, filepath.Join(transactionRoot, "release"))
		if err != nil {
			return InstallReport{}, err
		}
		outer.Release = outerTreeFromBackup(releaseBackup)
	}
	outer.Binary, err = snapshotOuterFile(targets[0].launcherPath, filepath.Join(transactionRoot, "binary.before"))
	if err != nil {
		return InstallReport{}, err
	}
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, err
	}
	createdResourceRoots := []string{}
	rollbackAll := func(cause error) error {
		markOuterCopyEvidence(&outer)
		if restoreErr := restoreOuterJournal(outerPath, outer, false); restoreErr != nil {
			cause = fmt.Errorf("%w (rollback failed: %v)", cause, restoreErr)
		}
		for _, path := range createdResourceRoots {
			removeEmptyDirectory(path)
		}
		return outerJournalFailure(outerPath, outer, cause)
	}
	if err := installFault("journal-boundary"); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	if err := installFault("intent"); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	if err := installFault("registry"); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	staged := make([]stagedInstallTree, 0, len(targets)+1)
	defer func() {
		for _, candidate := range staged {
			_ = os.RemoveAll(candidate.Temp)
		}
	}()
	// A release copy has a real copy phase. Mark the journal prepared before
	// entering it so component faults produce evidence that the transaction
	// reached the copy boundary rather than stopping at intent validation.
	if strings.TrimSpace(options.ReleaseRoot) != "" {
		outer.Phase = "prepared"
		if err := persistOuterJournal(outerPath, outer); err != nil {
			return InstallReport{}, rollbackAll(err)
		}
	}
	if strings.TrimSpace(options.ReleaseRoot) != "" {
		candidate := newStagedInstallTree(sourceAbs, options.ReleaseRoot, false)
		outer.Staged = append(outer.Staged, candidate.Temp)
		if err := persistOuterJournal(outerPath, outer); err != nil {
			return InstallReport{}, rollbackAll(err)
		}
		candidate, prepareErr := prepareInstallTree(candidate)
		if prepareErr != nil {
			return InstallReport{}, rollbackAll(prepareErr)
		}
		staged = append(staged, candidate)
	}
	for _, target := range targets {
		candidate := newStagedInstallTree(sourceAbs, target.targetPath, true)
		outer.Staged = append(outer.Staged, candidate.Temp)
		if err := persistOuterJournal(outerPath, outer); err != nil {
			return InstallReport{}, rollbackAll(err)
		}
		candidate, prepareErr := prepareInstallTree(candidate)
		if prepareErr != nil {
			return InstallReport{}, rollbackAll(prepareErr)
		}
		staged = append(staged, candidate)
	}
	outer.Phase = "prepared"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	if err := installFault("prepared"); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	for index := range staged {
		if switchErr := switchPreparedInstallTree(&staged[index], options.Force); switchErr != nil {
			return InstallReport{}, rollbackAll(switchErr)
		}
	}
	outer.Phase = "switched"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	if err := installFault("switched"); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	for _, candidate := range staged {
		strict := !candidate.RuntimeOnly || isFile(filepath.Join(sourceAbs, "internal", "validate", "runner.go"))
		if smokeErr := verifySwitchedInstallTree(candidate, !strict); smokeErr != nil {
			return InstallReport{}, rollbackAll(smokeErr)
		}
	}
	if err := installFault("post-switch-smoke"); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	outer.Phase = "smoke-passed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	launcherSource := filepath.Join(targets[0].targetPath, "bin", nativeBinaryName())
	if strings.TrimSpace(options.ReleaseRoot) != "" {
		launcherSource = filepath.Join(canonicalRegistryPath(options.ReleaseRoot), "bin", nativeBinaryName())
	}
	strictLauncher := isFile(filepath.Join(sourceAbs, "internal", "validate", "runner.go"))
	managedRuleChanges := map[string]bool{}
	hookChanges := map[string]bool{}
	for _, target := range targets {
		if !options.SkipHooks {
			if err := installFault("hook"); err != nil {
				return InstallReport{}, rollbackAll(err)
			}
			changed, err := configureInstallHook(target)
			if err != nil {
				return InstallReport{}, rollbackAll(err)
			}
			hookChanges[target.hookConfig] = changed
		}
		if target.managedRulePath != "" && !options.SkipHooks {
			if err := installFault("managed-rule"); err != nil {
				return InstallReport{}, rollbackAll(err)
			}
			changed, err := manageManagedRuleFile(target.managedRulePath, rule)
			if err != nil {
				return InstallReport{}, rollbackAll(err)
			}
			managedRuleChanges[target.managedRulePath] = changed
		}
	}
	// Publish the stable launcher only after installed-path smoke and all hook
	// and managed-rule writes have succeeded. Any later registry or receipt
	// failure still rolls every changed namespace back through the journal.
	if err := installFault("pointer"); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	if !samePath(launcherSource, targets[0].launcherPath) {
		if err := atomicCopyFile(launcherSource, targets[0].launcherPath); err != nil {
			return InstallReport{}, rollbackAll(err)
		}
	}
	if err := runInstalledBinarySmokeWithPolicy(targets[0].launcherPath, !strictLauncher); err != nil {
		return InstallReport{}, rollbackAll(fmt.Errorf("stable launcher smoke failed: %w", err))
	}
	records := make([]RegistryRecord, 0, len(targets))
	for _, target := range targets {
		installedReceipt, receiptErr := PackageReceipt(target.targetPath, sourceAbs)
		if receiptErr != nil {
			return InstallReport{}, rollbackAll(fmt.Errorf("installed target receipt failed: %w", receiptErr))
		}
		record := installRegistryRecord(target, options)
		record.VCSIdentity = vcsIdentity
		record.PackageDigest = sourcePackage.Digest
		record.InstalledDigest = installedReceipt.Digest
		if !exists(record.ResourceRoot) {
			if err := os.MkdirAll(record.ResourceRoot, 0o700); err != nil {
				return InstallReport{}, rollbackAll(fmt.Errorf("resource root setup failed: %w", err))
			}
			createdResourceRoots = append(createdResourceRoots, record.ResourceRoot)
		}
		releaseRoot := ""
		if strings.TrimSpace(options.ReleaseRoot) != "" {
			releaseRoot = filepath.ToSlash(canonicalRegistryPath(options.ReleaseRoot))
		}
		canonicalNamespaces := map[string]string{"sourceRoot": canonicalRegistryPath(sourceAbs), "target": canonicalRegistryPath(target.targetPath), "launcher": canonicalRegistryPath(target.launcherPath), "projectRoot": canonicalRegistryPath(record.ProjectRoot), "stateRoot": canonicalRegistryPath(record.StateRoot), "resourceRoot": canonicalRegistryPath(record.ResourceRoot), "runtimeSibling": canonicalRegistryPath(record.RuntimeSibling)}
		if registryPath != "" {
			canonicalNamespaces["registry"] = canonicalRegistryPath(registryPath)
		}
		if strings.TrimSpace(options.ReleaseRoot) != "" {
			canonicalNamespaces["releaseRoot"] = canonicalRegistryPath(options.ReleaseRoot)
		}
		targetReport := InstallTargetReport{
			Host:            target.host,
			TargetPath:      filepath.ToSlash(target.targetPath),
			LauncherPath:    filepath.ToSlash(target.launcherPath),
			ManagedRulePath: filepath.ToSlash(target.managedRulePath),
			SourceRoot:      filepath.ToSlash(sourceAbs),
			SourceDigest:    sourcePackage.Digest,
			InstalledDigest: installedReceipt.Digest,
			Manifest:        sourcePackage.Entries,
			Disjoint:        installedReceipt.Disjoint,
			CanonicalPaths:  canonicalNamespaces,
			Smoke:           "PASS",
			VCSIdentity:     vcsIdentity,
			PackageDigest:   sourcePackage.Digest,
			ReleaseRoot:     releaseRoot,
			SourceLstat:     pathLstat(sourceAbs),
			InstalledLstat:  pathLstat(target.targetPath),
			DisjointProof: map[string]string{
				"source-target":         "PASS",
				"source-launcher":       "PASS",
				"source-project":        "PASS",
				"target-state-resource": "PASS",
			},
		}
		if !options.SkipHooks && target.hookConfig != "" {
			targetReport.HookConfig = filepath.ToSlash(target.hookConfig)
			targetReport.HookDigest, _ = fileDigest(target.hookConfig)
			targetReport.CanonicalPaths["hookConfig"] = canonicalRegistryPath(target.hookConfig)
			if hookChanges[target.hookConfig] {
				targetReport.HookAction = "CONFIGURED"
			} else {
				targetReport.HookAction = "SKIPPED_UNCHANGED"
			}
		} else if options.SkipHooks {
			targetReport.HookAction = "SKIPPED"
		} else {
			targetReport.HookAction = "NOT_APPLICABLE"
		}
		targetReport.ManagedRuleDigest, _ = fileDigest(target.managedRulePath)
		if target.managedRulePath != "" {
			targetReport.CanonicalPaths["managedRule"] = canonicalRegistryPath(target.managedRulePath)
			if options.SkipHooks {
				targetReport.ManagedRuleAction = "SKIPPED"
			} else if managedRuleChanges[target.managedRulePath] {
				targetReport.ManagedRuleAction = "APPLIED"
			} else {
				targetReport.ManagedRuleAction = "SKIPPED_UNCHANGED"
			}
		} else {
			targetReport.ManagedRuleAction = "NOT_APPLICABLE"
		}
		report.Targets = append(report.Targets, targetReport)
		if registryPath != "" {
			records = append(records, record)
		}
	}
	if registryPath != "" {
		if faultErr := installFault("registry-commit"); faultErr != nil {
			return InstallReport{}, rollbackAll(faultErr)
		}
		registryDocument, loadErr := loadRegistryForCommit(registryPath)
		if loadErr != nil {
			return InstallReport{}, rollbackAll(fmt.Errorf("installation registry admission bridge load failed: %w", loadErr))
		}
		records = append(records, refreshedGlobalInvocationRecords(registryDocument.Records, targets, options, vcsIdentity, sourcePackage.Digest, records)...)
		committed, err := commitRegistryRecordsUnlocked(registryPath, registryDocument, records)
		if err != nil {
			return InstallReport{}, rollbackAll(fmt.Errorf("installation registry admission bridge commit failed: %w", err))
		}
		report.RegistryEpoch = committed.Epoch
		for index := range report.Targets {
			report.Targets[index].RegistryRecordID = records[index].ID
			report.Targets[index].RegistryEpoch = committed.Epoch
		}
	}
	outer.Phase = "registry-committed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	if report.ReceiptPath != "" {
		if writeErr := writeJSONAtomically(filepath.FromSlash(report.ReceiptPath), report); writeErr != nil {
			return InstallReport{}, rollbackAll(fmt.Errorf("failed to persist install receipt: %w", writeErr))
		}
	}
	outer.Phase = "committed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, rollbackAll(err)
	}
	_ = os.RemoveAll(outer.TransactionRoot)
	_ = os.Remove(outerPath)
	return report, nil
}

func Uninstall(options UninstallOptions) (UninstallReport, error) {
	targets, err := resolveInstallTargets(options.Host, options.Scope, options.Project)
	if err != nil {
		return UninstallReport{}, err
	}
	registryPath := installRegistryPath(InstallOptions{Host: options.Host, Scope: options.Scope, Project: options.Project, RegistryPath: options.RegistryPath})
	var unlock func()
	if registryPath != "" {
		unlock, err = acquireInstallLock(registryPath)
		if err != nil {
			return UninstallReport{}, err
		}
		defer unlock()
		unlock, err = acquireRegistryLock(registryPath)
		if err != nil {
			return UninstallReport{}, err
		}
		defer unlock()
		if err := reconcileOuterInstallJournal(registryPath); err != nil {
			return UninstallReport{}, err
		}
		// Resolve the launcher only after taking both locks.  Otherwise a
		// concurrent install can change the registry between this lookup and the
		// uninstall transaction, leaving hooks and the registry describing
		// different generations.
		if doc, loadErr := LoadRegistry(registryPath); loadErr == nil {
			if fenceErr := rejectActiveWorkflowRuns("uninstall", targets, InstallOptions{Host: options.Host, Scope: options.Scope, Project: options.Project, RegistryPath: options.RegistryPath}, doc.Records); fenceErr != nil {
				return UninstallReport{}, fenceErr
			}
			for index := range targets {
				for _, record := range doc.Records {
					if filepath.Clean(record.Target) == filepath.Clean(targets[index].targetPath) && strings.TrimSpace(record.LauncherPath) != "" {
						targets[index].launcherPath = record.LauncherPath
					}
				}
			}
		} else if !os.IsNotExist(loadErr) {
			return UninstallReport{}, fmt.Errorf("registry admission bridge is unavailable: %w", loadErr)
		}
	} else if fenceErr := rejectActiveWorkflowRuns("uninstall", targets, InstallOptions{Host: options.Host, Scope: options.Scope, Project: options.Project, RegistryPath: options.RegistryPath}, nil); fenceErr != nil {
		return UninstallReport{}, fenceErr
	}
	transactionParent := filepath.Dir(registryPath)
	if transactionParent == "." || transactionParent == "" {
		transactionParent = filepath.Dir(targets[0].targetPath)
	}
	for !exists(transactionParent) {
		parent := filepath.Dir(transactionParent)
		if parent == transactionParent {
			break
		}
		transactionParent = parent
	}
	transactionRoot, err := os.MkdirTemp(transactionParent, ".formal-gates-uninstall-")
	if err != nil {
		return UninstallReport{}, err
	}
	defer os.RemoveAll(transactionRoot)
	outerPath := installOuterJournalPath(registryPath)
	outer := outerInstallJournal{Operation: "uninstall", RegistryPath: registryPath, TransactionRoot: transactionRoot, Phase: "intent", CreatedAt: nowReceiptTime()}
	if len(targets) > 0 {
		outer.InstalledTarget = canonicalRegistryPath(targets[0].targetPath)
	}
	if doc, loadErr := LoadRegistry(registryPath); loadErr == nil {
		outer.Generation = doc.Epoch
		for _, record := range doc.Records {
			if canonicalRegistryPath(record.Target) == outer.InstalledTarget {
				outer.Generation, outer.Lease, outer.Token = record.Generation, record.Lease, record.Token
				break
			}
		}
	}
	if outer.Generation == 0 {
		outer.Generation = 1
	}
	outer.VCSIdentity = "registry:" + filepath.Base(registryPath)
	outer.PackageDigest = "registry-state"
	outer.Registry, err = snapshotOuterFile(registryPath, filepath.Join(transactionRoot, "registry.before"))
	if err != nil {
		return UninstallReport{}, err
	}
	for index, target := range targets {
		backupRoot := filepath.Join(transactionRoot, fmt.Sprintf("target-%d", index))
		tree, backupErr := snapshotInstallTree(target.targetPath, filepath.Join(backupRoot, "runtime"))
		if backupErr != nil {
			return UninstallReport{}, backupErr
		}
		hook, backupErr := snapshotOuterFile(target.hookConfig, filepath.Join(backupRoot, "hook.before"))
		if backupErr != nil {
			return UninstallReport{}, backupErr
		}
		rule, backupErr := snapshotOuterFile(target.managedRulePath, filepath.Join(backupRoot, "rule.before"))
		if backupErr != nil {
			return UninstallReport{}, backupErr
		}
		outer.Targets = append(outer.Targets, outerTargetSnapshot{TargetPath: target.targetPath, HookPath: target.hookConfig, RulePath: target.managedRulePath, Tree: outerTreeFromBackup(tree), Hook: hook, Rule: rule})
	}
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return UninstallReport{}, err
	}
	rollback := func(cause error) error {
		if restoreErr := restoreOuterJournal(outerPath, outer, false); restoreErr != nil {
			cause = fmt.Errorf("%w (rollback failed: %v)", cause, restoreErr)
		}
		return outerJournalFailure(outerPath, outer, cause)
	}
	if err := installFault("intent"); err != nil {
		return UninstallReport{}, rollback(err)
	}
	report := UninstallReport{}
	for _, target := range targets {
		if err := os.RemoveAll(target.targetPath); err != nil {
			return UninstallReport{}, rollback(err)
		}
		report.Targets = append(report.Targets, installTargetReport(target))
	}
	outer.Phase = "switched"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return UninstallReport{}, rollback(err)
	}
	if err := installFault("switched"); err != nil {
		return UninstallReport{}, rollback(err)
	}
	if err := installFault("post-switch-smoke"); err != nil {
		return UninstallReport{}, rollback(err)
	}
	for _, target := range targets {
		if target.managedRulePath != "" {
			if err := installFault("managed-rule"); err != nil {
				return UninstallReport{}, rollback(err)
			}
			if err := removeManagedRuleFile(target.managedRulePath, target.host == "cursor"); err != nil {
				return UninstallReport{}, rollback(err)
			}
		}
		if err := installFault("hook"); err != nil {
			return UninstallReport{}, rollback(err)
		}
		if err := removeInstallHooks(target); err != nil {
			return UninstallReport{}, rollback(err)
		}
	}
	if registryPath != "" {
		if doc, loadErr := LoadRegistry(registryPath); loadErr == nil {
			updated := append([]RegistryRecord(nil), doc.Records...)
			for index := range updated {
				for _, target := range targets {
					if filepath.Clean(updated[index].Target) == filepath.Clean(target.targetPath) {
						updated[index].Status = "disabled"
						break
					}
				}
			}
			if _, err := commitRegistryRecordsUnlocked(registryPath, doc, updated); err != nil {
				return UninstallReport{}, rollback(fmt.Errorf("uninstall registry bridge update failed: %w", err))
			}
		} else if !os.IsNotExist(loadErr) {
			return UninstallReport{}, rollback(fmt.Errorf("uninstall registry bridge unavailable: %w", loadErr))
		}
	}
	outer.Phase = "registry-committed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return UninstallReport{}, rollback(err)
	}
	outer.Phase = "committed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return UninstallReport{}, rollback(err)
	}
	_ = os.RemoveAll(outer.TransactionRoot)
	_ = os.Remove(outerPath)
	return report, nil
}

// installTreeBackup is the outer install transaction's undo record.  The
// outer coordinator protects every target; this record lets it undo already
// switched trees when a later host, launcher, config, registry, or receipt
// commit fails.
type installTreeBackup struct {
	path    string
	backup  string
	existed bool
}

func snapshotInstallTree(path, backup string) (installTreeBackup, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return installTreeBackup{}, err
	}
	state := installTreeBackup{path: filepath.Clean(path), backup: filepath.Clean(backup)}
	info, err := os.Lstat(state.path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return state, fmt.Errorf("cannot back up non-directory install target: %s", state.path)
	}
	state.existed = true
	if err := copyTreeForBackup(state.path, state.backup); err != nil {
		return installTreeBackup{}, err
	}
	return state, nil
}

func installRegistryRecord(target installTarget, options InstallOptions) RegistryRecord {
	projectRoot := options.Project
	if strings.EqualFold(options.Scope, "global") {
		projectRoot, _ = installHomeDir()
	}
	projectRoot = absPath(projectRoot)
	stateRoot := filepath.Join(projectRoot, ".gates")
	resourceRoot := filepath.Join(projectRoot, ".formal-gates-resources")
	releaseRoot := ""
	if strings.TrimSpace(options.ReleaseRoot) != "" {
		releaseRoot = canonicalRegistryPath(options.ReleaseRoot)
	}
	canonical := map[string]string{
		"target":         canonicalRegistryPath(target.targetPath),
		"launcher":       canonicalRegistryPath(target.launcherPath),
		"projectRoot":    canonicalRegistryPath(projectRoot),
		"stateRoot":      canonicalRegistryPath(stateRoot),
		"resourceRoot":   canonicalRegistryPath(resourceRoot),
		"runtimeSibling": canonicalRegistryPath(filepath.Dir(target.targetPath)),
	}
	if target.hookConfig != "" {
		canonical["hookConfig"] = canonicalRegistryPath(target.hookConfig)
	}
	if releaseRoot != "" {
		canonical["releaseRoot"] = releaseRoot
	}
	identity := sha256.Sum256([]byte(canonicalRegistryPath(target.targetPath)))
	return RegistryRecord{
		ID:             fmt.Sprintf("%s-%s-%x", target.host, strings.ToLower(options.Scope), identity[:6]),
		Target:         target.targetPath,
		LauncherPath:   target.launcherPath,
		Scope:          strings.ToLower(options.Scope),
		Host:           target.host,
		HookConfig:     target.hookConfig,
		ProjectRoot:    projectRoot,
		StateRoot:      stateRoot,
		ResourceRoot:   resourceRoot,
		RuntimeSibling: filepath.Dir(target.targetPath),
		ReleaseRoot:    releaseRoot,
		CanonicalPaths: canonical,
		Status:         "active",
	}
}

func installRegistryPath(options InstallOptions) string {
	if path := strings.TrimSpace(options.RegistryPath); path != "" {
		return absPath(path)
	}
	if home, err := installHomeDir(); err == nil {
		return filepath.Join(home, ".formal-gates", "registry.json")
	}
	return ""
}

// rejectActiveWorkflowRuns inventories the state roots owned by the affected
// registry records before an install or uninstall can replace runtime bytes or
// advance admission identity. An active run keeps the current target and its
// launcher authoritative until the run reaches a terminal state.
func rejectActiveWorkflowRuns(operation string, targets []installTarget, options InstallOptions, records []RegistryRecord) error {
	targetPaths := map[string]bool{}
	stateRoots := map[string]bool{}
	for _, target := range targets {
		targetPaths[canonicalRegistryPath(target.targetPath)] = true
		desired := installRegistryRecord(target, options)
		if desired.StateRoot != "" {
			stateRoots[canonicalRegistryPath(desired.StateRoot)] = true
		}
	}
	for _, record := range records {
		if !targetPaths[canonicalRegistryPath(record.Target)] {
			continue
		}
		if record.StateRoot != "" {
			stateRoots[canonicalRegistryPath(record.StateRoot)] = true
		}
	}
	for stateRoot := range stateRoots {
		matches, err := filepath.Glob(filepath.Join(stateRoot, "tmp", "*", "state.json"))
		if err != nil {
			return fmt.Errorf("%s active-run inventory failed for %s: %w", operation, stateRoot, err)
		}
		for _, statePath := range matches {
			data, readErr := os.ReadFile(statePath)
			if readErr != nil {
				return fmt.Errorf("%s active-run inventory failed for %s: %w", operation, statePath, readErr)
			}
			var probe struct {
				RunID  string `json:"runId"`
				Status string `json:"status"`
			}
			if json.Unmarshal(data, &probe) != nil || !strings.EqualFold(probe.Status, "ACTIVE") {
				continue
			}
			return fmt.Errorf("active workflow run %q at %s fences %s", probe.RunID, statePath, operation)
		}
	}
	return nil
}

func defaultStableLauncherPath(options InstallOptions) string {
	if runtime.GOOS == "windows" {
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			return stableLauncherPath(filepath.Join(local, "formal-gates", "bin", nativeBinaryName()))
		}
	}
	home, err := installHomeDir()
	if err != nil {
		return stableLauncherPath(filepath.Join(".local", "bin", nativeBinaryName()))
	}
	return stableLauncherPath(filepath.Join(home, ".local", "bin", nativeBinaryName()))
}

// Keep the public launcher path lexical. Resolving an existing symlink here
// would make an upgrade overwrite the old release instead of replacing the
// stable pointer itself.
func stableLauncherPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	abs = filepath.Clean(abs)
	// Resolve aliases in the existing parent only. The final component is the
	// public stable pointer and must not be resolved when it is already a
	// symlink to an older release.
	current := filepath.Dir(abs)
	for {
		if resolved, resolveErr := filepath.EvalSymlinks(current); resolveErr == nil {
			rel, relErr := filepath.Rel(current, abs)
			if relErr == nil {
				return filepath.Clean(filepath.Join(resolved, rel))
			}
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return abs
}

func launcherInvocationMatches(expected string) bool {
	expected = stableLauncherPath(expected)
	if argument := strings.TrimSpace(os.Args[0]); argument != "" && stableLauncherPath(argument) == expected {
		return true
	}
	executable, err := os.Executable()
	return err == nil && stableLauncherPath(executable) == expected
}

// RequireInstallLauncher fences the public mutation command. The downloaded
// archive binary must first be staged at the fixed launcher path by the
// checksum-verifying bootstrap script; invoking source/bin directly is a
// candidate path and cannot write the shared registry or host configuration.
func RequireInstallLauncher(options InstallOptions) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// Go's in-process unit test binaries do not expose the shipped executable
	// name.  Production artifacts always use nativeBinaryName, so this branch is
	// not a public compatibility mode.
	base := filepath.Base(filepath.Clean(executable))
	if strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe") {
		return nil
	}
	expected := defaultStableLauncherPath(options)
	if strings.TrimSpace(options.BinaryTarget) != "" {
		if stableLauncherPath(options.BinaryTarget) != expected {
			return fmt.Errorf("UNREGISTERED_INSTALL: --binary-target must be the fixed stable launcher %s", expected)
		}
	}
	if !launcherInvocationMatches(expected) {
		return fmt.Errorf("UNREGISTERED_INSTALL: install maintenance must run through stable launcher %s", expected)
	}
	return nil
}

func RequireUninstallLauncher(options UninstallOptions) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	base := filepath.Base(filepath.Clean(executable))
	if strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe") {
		return nil
	}
	registry := installRegistryPath(InstallOptions{Host: options.Host, Scope: options.Scope, Project: options.Project, RegistryPath: options.RegistryPath})
	doc, err := LoadRegistry(registry)
	if err != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: uninstall requires the shared registry: %w", err)
	}
	targets, err := resolveInstallTargets(options.Host, options.Scope, options.Project)
	if err != nil {
		return err
	}
	for _, target := range targets {
		for _, record := range doc.Records {
			if canonicalRegistryPath(record.Target) == canonicalRegistryPath(target.targetPath) && launcherInvocationMatches(record.LauncherPath) {
				return nil
			}
		}
	}
	return fmt.Errorf("UNREGISTERED_INSTALL: uninstall maintenance must run through the registered stable launcher")
}

func bootstrapInstall(options InstallOptions, targets []installTarget, source PackageReceiptReport, registryPath string) (InstallReport, error) {
	if registryPath == "" {
		return InstallReport{}, fmt.Errorf("bootstrap requires a registry path for the selected scope")
	}
	installUnlock, err := acquireInstallLock(registryPath)
	if err != nil {
		return InstallReport{}, err
	}
	defer installUnlock()
	unlock, err := acquireRegistryLock(registryPath)
	if err != nil {
		return InstallReport{}, err
	}
	defer unlock()
	if err := reconcileOuterInstallJournal(registryPath); err != nil {
		return InstallReport{}, err
	}
	receiptPath := registryPath + ".bootstrap.json"
	transactionRoot, err := os.MkdirTemp(filepath.Dir(registryPath), ".formal-gates-bootstrap-")
	if err != nil {
		return InstallReport{}, err
	}
	defer os.RemoveAll(transactionRoot)
	outerPath := installOuterJournalPath(registryPath)
	outer := outerInstallJournal{Operation: "bootstrap", RegistryPath: registryPath, TransactionRoot: transactionRoot, Phase: "intent", CreatedAt: nowReceiptTime()}
	outer.VCSIdentity = sourceVCSIdentity(source.Root, source.Digest)
	outer.PackageDigest = source.Digest
	existingRegistry, existingRegistryErr := LoadRegistry(registryPath)
	registryWasPresent := existingRegistryErr == nil
	if existingRegistryErr != nil && !os.IsNotExist(existingRegistryErr) {
		return InstallReport{}, fmt.Errorf("registry bootstrap cannot read existing registry: %w", existingRegistryErr)
	}
	if registryWasPresent {
		for _, target := range targets {
			for _, record := range existingRegistry.Records {
				if canonicalRegistryPath(record.Target) == canonicalRegistryPath(target.targetPath) && record.PackageDigest == source.Digest && strings.TrimSpace(record.VCSIdentity) != "" {
					// The first install may have been sourced from a Git checkout,
					// while bootstrap necessarily reads the copied release tree.
					// Keep the authoritative identity already committed for this
					// target when the immutable package digest is unchanged.
					outer.VCSIdentity = record.VCSIdentity
					break
				}
			}
		}
	}
	if len(targets) > 0 {
		outer.InstalledTarget = canonicalRegistryPath(targets[0].targetPath)
	}
	if registryWasPresent {
		outer.Generation = existingRegistry.Epoch + 1
	}
	if outer.Generation == 0 {
		outer.Generation = 1
	}
	outer.Lease = fmt.Sprintf("lease-%d", outer.Generation)
	outer.Token = fmt.Sprintf("token-%d", time.Now().UnixNano())
	outer.Registry, err = snapshotOuterFile(registryPath, filepath.Join(transactionRoot, "registry.before"))
	if err != nil {
		return InstallReport{}, err
	}
	outer.Receipt, err = snapshotOuterFile(receiptPath, filepath.Join(transactionRoot, "bootstrap-receipt.before"))
	if err != nil {
		return InstallReport{}, err
	}
	for _, target := range targets {
		record := installRegistryRecord(target, options)
		outer.Targets = append(outer.Targets, outerTargetSnapshot{TargetPath: target.targetPath, ResourcePath: record.ResourceRoot, ResourceExisted: exists(record.ResourceRoot)})
	}
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, err
	}
	rollback := func(cause error) error {
		_ = restoreOuterJournal(outerPath, outer, false)
		return outerJournalFailure(outerPath, outer, cause)
	}
	if registryWasPresent && len(existingRegistry.Records) == 0 {
		writeBootstrapAdmissionRejection(registryPath, targets[0], options, "", "an existing empty registry is not a fresh bootstrap boundary")
		return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: existing registry has no admission record for bootstrap"))
	}
	if registryWasPresent {
		for _, record := range existingRegistry.Records {
			if !validAdmissionRegistryRecord(record) {
				writeBootstrapAdmissionRejection(registryPath, targets[0], options, record.ID, "an existing registry record cannot be reconciled")
				return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: existing registry record %q cannot be reconciled", record.ID))
			}
			if strings.EqualFold(record.Status, "active") {
				if targetErr := assertInstallSource(record.Target); targetErr != nil {
					writeBootstrapAdmissionRejection(registryPath, targets[0], options, record.ID, fmt.Sprintf("an existing active target is not an installed artifact: %v", targetErr))
					return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: existing registry target %q is not an installed artifact", record.Target))
				}
			}
		}
	}
	strictBinary := isFile(filepath.Join(source.Root, "internal", "validate", "runner.go"))
	targetDigests := map[string]string{}
	for _, target := range targets {
		if targetErr := assertInstallSource(target.targetPath); targetErr != nil {
			writeBootstrapAdmissionRejection(registryPath, target, options, "", fmt.Sprintf("bootstrap target is not an installed artifact: %v", targetErr))
			return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: bootstrap target %s is not an installed artifact: %w", target.targetPath, targetErr))
		}
		disjoint := []string{source.Root}
		if samePath(target.targetPath, source.Root) {
			disjoint = nil
		}
		targetReceipt, targetErr := PackageReceipt(target.targetPath, disjoint...)
		if targetErr != nil {
			writeBootstrapAdmissionRejection(registryPath, target, options, "", fmt.Sprintf("bootstrap target receipt failed: %v", targetErr))
			return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: bootstrap target receipt failed: %w", targetErr))
		}
		targetDigests[canonicalRegistryPath(target.targetPath)] = targetReceipt.Digest
		if targetErr := runInstalledBinarySmokeWithPolicy(filepath.Join(target.targetPath, "bin", nativeBinaryName()), !strictBinary); targetErr != nil {
			writeBootstrapAdmissionRejection(registryPath, target, options, "", fmt.Sprintf("bootstrap target smoke failed: %v", targetErr))
			return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: bootstrap target smoke failed: %w", targetErr))
		}
	}
	if targetErr := runInstalledBinarySmokeWithPolicy(targets[0].launcherPath, !strictBinary); targetErr != nil {
		writeBootstrapAdmissionRejection(registryPath, targets[0], options, "", fmt.Sprintf("bootstrap stable launcher smoke failed: %v", targetErr))
		return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: bootstrap stable launcher smoke failed: %w", targetErr))
	}
	if faultErr := installFault("registry"); faultErr != nil {
		return InstallReport{}, rollback(faultErr)
	}
	existingByID := map[string]RegistryRecord{}
	if existing, loadErr := LoadRegistry(registryPath); loadErr == nil {
		for _, record := range existing.Records {
			existingByID[record.ID] = record
			for _, target := range targets {
				desired := installRegistryRecord(target, options)
				sameTarget := canonicalRegistryPath(record.Target) == canonicalRegistryPath(target.targetPath)
				if sameTarget && record.ID != desired.ID {
					if isGlobalInvocationRecord(record, desired) {
						continue
					}
					writeBootstrapAdmissionRejection(registryPath, target, options, desired.ID, "bootstrap target already exists with a different registry record identity")
					return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: bootstrap target %s has an unaccounted registry identity", target.targetPath))
				}
				if record.ID == desired.ID && !sameTarget {
					writeBootstrapAdmissionRejection(registryPath, target, options, record.ID, "bootstrap target conflicts with an existing registry record")
					return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: bootstrap target conflicts with registry record %q", record.ID))
				}
				if sameTarget {
					expected := desired
					expected.VCSIdentity = outer.VCSIdentity
					expected.PackageDigest = source.Digest
					expected.InstalledDigest = targetDigests[canonicalRegistryPath(target.targetPath)]
					valid := validRegistryRecord(record)
					binding := sameRegistryBinding(record, expected)
					identity := sameRegistryIdentity(record, expected)
					if !strings.EqualFold(record.Status, "active") || !valid || !binding || !identity {
						reason := fmt.Sprintf("bootstrap target has a stale or incomplete registry identity (status=%s valid=%t binding=%t identity=%t)", record.Status, valid, binding, identity)
						writeBootstrapAdmissionRejection(registryPath, target, options, record.ID, reason)
						return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: bootstrap target %s cannot reconcile its existing registry identity: %s", target.targetPath, reason))
					}
				}
			}
		}
	} else if !os.IsNotExist(loadErr) {
		return InstallReport{}, fmt.Errorf("registry bootstrap cannot read existing registry: %w", loadErr)
	}
	if registryWasPresent {
		for _, target := range targets {
			desired := installRegistryRecord(target, options)
			if _, found := existingByID[desired.ID]; found {
				continue
			}
			if bootstrapHasSiblingAdmission(desired, existingRegistry.Records, source.Digest) {
				continue
			}
			writeBootstrapAdmissionRejection(registryPath, target, options, desired.ID, "an existing registry has no admission record for the bootstrap target")
			return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: bootstrap target %s is missing from the existing registry", target.targetPath))
		}
	}
	records := make([]RegistryRecord, 0, len(targets))
	mutatesRegistry := false
	createdResourceRoots := []string{}
	for _, target := range targets {
		desired := installRegistryRecord(target, options)
		desired.VCSIdentity = outer.VCSIdentity
		desired.PackageDigest = source.Digest
		desired.InstalledDigest = targetDigests[canonicalRegistryPath(target.targetPath)]
		if !exists(desired.ResourceRoot) {
			if err := os.MkdirAll(desired.ResourceRoot, 0o700); err != nil {
				return InstallReport{}, rollback(fmt.Errorf("resource root setup failed: %w", err))
			}
			createdResourceRoots = append(createdResourceRoots, desired.ResourceRoot)
		}
		if existing, ok := existingByID[desired.ID]; ok && strings.EqualFold(existing.Status, "active") && validRegistryRecord(existing) && sameRegistryBinding(existing, desired) && sameRegistryIdentity(existing, desired) {
			records = append(records, existing)
			continue
		}
		mutatesRegistry = true
		records = append(records, desired)
	}
	registryDocument, loadErr := loadRegistryForCommit(registryPath)
	if loadErr != nil {
		for _, path := range createdResourceRoots {
			removeEmptyDirectory(path)
		}
		return InstallReport{}, rollback(loadErr)
	}
	siblingRefresh := refreshedGlobalInvocationRecords(registryDocument.Records, targets, options, outer.VCSIdentity, source.Digest, records)
	if len(siblingRefresh) != 0 {
		records = append(records, siblingRefresh...)
		mutatesRegistry = true
	}
	rollbackWithResources := func(cause error) error {
		for _, path := range createdResourceRoots {
			removeEmptyDirectory(path)
		}
		return rollback(cause)
	}
	if mutatesRegistry {
		if _, err := commitRegistryRecordsUnlocked(registryPath, registryDocument, records); err != nil {
			return InstallReport{}, rollbackWithResources(err)
		}
	}
	committedRegistry, err := LoadRegistry(registryPath)
	if err != nil {
		return InstallReport{}, rollbackWithResources(err)
	}
	outer.Phase = "registry-committed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, rollbackWithResources(err)
	}
	receipt := BootstrapReceipt{
		Operation: "bootstrap", Accepted: true, Registry: filepath.Clean(registryPath),
		Epoch: committedRegistry.Epoch, PackageDigest: source.Digest,
		VCSIdentity: outer.VCSIdentity, SourceRoot: filepath.ToSlash(canonicalRegistryPath(source.Root)),
		SourceLstat: pathLstat(source.Root), Records: committedRegistry.Records,
		StateCreated: false, ObservedAt: nowReceiptTime(),
	}
	if err := writeJSONAtomically(receiptPath, receipt); err != nil {
		return InstallReport{}, rollback(err)
	}
	outer.Phase = "committed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, rollback(err)
	}
	_ = os.RemoveAll(outer.TransactionRoot)
	_ = os.Remove(outerPath)
	return InstallReport{GeneratedAt: "sha256:" + source.Digest, Registry: filepath.ToSlash(registryPath), RegistryEpoch: committedRegistry.Epoch, ReceiptPath: filepath.ToSlash(receiptPath), BootstrapReceiptPath: filepath.ToSlash(receiptPath), VCSIdentity: outer.VCSIdentity, PackageDigest: source.Digest}, nil
}

func bootstrapHasSiblingAdmission(desired RegistryRecord, records []RegistryRecord, packageDigest string) bool {
	for _, record := range records {
		if !strings.EqualFold(record.Status, "active") || !validAdmissionRegistryRecord(record) {
			continue
		}
		if record.PackageDigest == packageDigest &&
			record.Scope == desired.Scope &&
			canonicalRegistryPath(record.ProjectRoot) == canonicalRegistryPath(desired.ProjectRoot) &&
			canonicalRegistryPath(record.LauncherPath) == canonicalRegistryPath(desired.LauncherPath) {
			return true
		}
	}
	return false
}

// Global installs have one canonical host record, but workflow state and
// resources remain project-local. bindGlobalInvocationRoot therefore creates
// project-derived sibling records that share the same installed target. They
// are not a competing installation identity and must be refreshed whenever the
// canonical global target is upgraded or bootstrapped.
func isGlobalInvocationRecord(record, desired RegistryRecord) bool {
	return desired.Scope == "global" && record.Scope == "global" &&
		record.ID != desired.ID && strings.HasPrefix(record.ID, desired.ID+"-project-") &&
		canonicalRegistryPath(record.Target) == canonicalRegistryPath(desired.Target) &&
		canonicalRegistryPath(record.LauncherPath) == canonicalRegistryPath(desired.LauncherPath) &&
		record.Host == desired.Host && record.HookConfig == desired.HookConfig &&
		record.RuntimeSibling == desired.RuntimeSibling && record.ReleaseRoot == desired.ReleaseRoot
}

func refreshedGlobalInvocationRecords(existing []RegistryRecord, targets []installTarget, options InstallOptions, vcsIdentity, packageDigest string, already []RegistryRecord) []RegistryRecord {
	refreshed := []RegistryRecord{}
	for _, record := range existing {
		for index, target := range targets {
			desired := installRegistryRecord(target, options)
			if !isGlobalInvocationRecord(record, desired) {
				continue
			}
			updated := record
			updated.VCSIdentity = vcsIdentity
			updated.PackageDigest = packageDigest
			if index < len(already) {
				updated.InstalledDigest = already[index].InstalledDigest
			}
			if sameRegistryIdentity(record, updated) {
				continue
			}
			refreshed = append(refreshed, updated)
		}
	}
	return refreshed
}

func bootstrapUnregisteredReceipt(registryPath string, target installTarget, options InstallOptions, reason string) AdmissionReceipt {
	record := installRegistryRecord(target, options)
	return AdmissionReceipt{
		Code: "UNREGISTERED_INSTALL", Status: "disabled", Accepted: false,
		RecordID: record.ID, Registry: registryPath, Target: record.Target,
		Scope: record.Scope, Host: record.Host, CanonicalPaths: record.CanonicalPaths,
		Reason: reason, CreatedAt: nowReceiptTime(),
	}
}

func writeBootstrapAdmissionRejection(registryPath string, target installTarget, options InstallOptions, recordID, reason string) {
	receipt := bootstrapUnregisteredReceipt(registryPath, target, options, reason)
	if strings.TrimSpace(recordID) != "" {
		receipt.RecordID = recordID
	}
	_ = writeAdmissionReceipt(registryPath, receipt)
}

func sameRegistryBinding(left, right RegistryRecord) bool {
	leftFields := []string{left.Target, left.LauncherPath, left.Scope, left.Host, left.HookConfig, left.ProjectRoot, left.StateRoot, left.ResourceRoot, left.RuntimeSibling, left.ReleaseRoot}
	rightFields := []string{right.Target, right.LauncherPath, right.Scope, right.Host, right.HookConfig, right.ProjectRoot, right.StateRoot, right.ResourceRoot, right.RuntimeSibling, right.ReleaseRoot}
	for index := range leftFields {
		if filepath.Clean(leftFields[index]) != filepath.Clean(rightFields[index]) {
			return false
		}
	}
	if len(left.CanonicalPaths) != len(right.CanonicalPaths) {
		return false
	}
	for key, value := range right.CanonicalPaths {
		if canonicalRegistryPath(left.CanonicalPaths[key]) != canonicalRegistryPath(value) {
			return false
		}
	}
	return true
}

func sameRegistryIdentity(left, right RegistryRecord) bool {
	return left.VCSIdentity == right.VCSIdentity &&
		left.PackageDigest == right.PackageDigest &&
		left.InstalledDigest == right.InstalledDigest
}

func nowReceiptTime() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func sourceVCSIdentity(root, packageDigest string) string {
	rootAbs, absErr := filepath.Abs(root)
	if absErr != nil {
		rootAbs = filepath.Clean(root)
	}
	rootAbs = filepath.Clean(rootAbs)
	// Git searches parent directories for a repository. A release package inside
	// a test project must not inherit that project's HEAD as its own identity.
	if output, err := exec.Command("git", "-C", rootAbs, "rev-parse", "--show-toplevel").Output(); err == nil && canonicalRegistryPath(strings.TrimSpace(string(output))) == canonicalRegistryPath(rootAbs) {
		if output, err := exec.Command("git", "-C", rootAbs, "rev-parse", "HEAD").Output(); err == nil {
			if identity := strings.TrimSpace(string(output)); identity != "" {
				return "git:" + identity
			}
		}
	}
	if strings.TrimSpace(packageDigest) != "" {
		return "package:" + strings.TrimPrefix(strings.TrimSpace(packageDigest), "sha256:")
	}
	return "unknown"
}

func pathLstat(path string) PathLstat {
	path = filepath.Clean(path)
	identity := PathLstat{Path: filepath.ToSlash(path), RealPath: filepath.ToSlash(canonicalRegistryPath(path)), Kind: "missing"}
	info, err := os.Lstat(path)
	if err != nil {
		return identity
	}
	identity.Mode = uint32(info.Mode().Perm())
	identity.Size = info.Size()
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		identity.Kind = "symlink"
	case info.IsDir():
		identity.Kind = "directory"
	case info.Mode().IsRegular():
		identity.Kind = "regular"
	default:
		identity.Kind = "nonregular"
	}
	return identity
}

func pathLstatIdentity(path string) (PathLstat, error) {
	identity := pathLstat(path)
	if identity.Kind == "missing" {
		return PathLstat{}, fmt.Errorf("path does not exist: %s", path)
	}
	if identity.Kind == "symlink" || identity.Kind == "nonregular" {
		return PathLstat{}, fmt.Errorf("path is not an immutable regular file or directory: %s", path)
	}
	identity.Digest = pathIdentityDigest(path, identity.Kind)
	if identity.Digest == "" {
		return PathLstat{}, fmt.Errorf("path digest is unavailable: %s", path)
	}
	return identity, nil
}

func pathIdentityDigest(path, kind string) string {
	var digest string
	if kind == "regular" {
		digest, _ = fileDigest(path)
	} else if kind == "directory" {
		if receipt, err := PackageReceipt(path); err == nil {
			digest = receipt.Digest
		}
	}
	return digest
}

func removeEmptyDirectory(path string) {
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		return
	}
	_ = os.Remove(path)
}

func normalizeInstallHost(host string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "claude":
		return "claude", nil
	case "codex":
		return "codex", nil
	case "cursor":
		return "cursor", nil
	case "dsh", "deepseek", "deepseek-harness":
		return "dsh", nil
	case "both":
		return "both", nil
	default:
		return "", fmt.Errorf("unsupported --host %q (want claude, codex, cursor, dsh, or both)", host)
	}
}

func normalizeInstallScope(scope string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "global":
		return "global", nil
	case "project":
		return "project", nil
	default:
		return "", fmt.Errorf("unsupported --scope %q (want global or project)", scope)
	}
}

func assertInstallSource(source string) error {
	for _, entry := range installRuntimeEntries {
		if !exists(filepath.Join(source, filepath.FromSlash(entry))) {
			if entry == "definitions" {
				// Older runtime-only fixtures do not expose the future engine
				// definition. Complete release packages are checked by Package;
				// this source admission check only validates the copied runtime set.
				continue
			}
			return fmt.Errorf("formal-gates source is incomplete; missing %s under %s", entry, source)
		}
	}
	binaryRel := filepath.Join("bin", nativeBinaryName())
	if !isFile(filepath.Join(source, binaryRel)) {
		return fmt.Errorf("formal-gates native binary is missing at %s; build it first with: go build -o %s ./cmd/formal-gates", filepath.Join(source, binaryRel), filepath.Join("bin", nativeBinaryName()))
	}
	return nil
}

func installTargets(host, scope, project string) ([]installTarget, error) {
	hosts := []string{host}
	if host == "both" {
		hosts = []string{"claude", "codex"}
	}
	home := ""
	if scope == "global" {
		var err error
		home, err = installHomeDir()
		if err != nil {
			return nil, err
		}
	}
	targets := make([]installTarget, 0, len(hosts))
	for _, h := range hosts {
		var base string
		var hookConfig string
		var managedRulePath string
		if scope == "global" {
			switch h {
			case "claude":
				base = filepath.Join(home, ".claude", "skills")
				hookConfig = filepath.Join(home, ".claude", "settings.json")
				managedRulePath = filepath.Join(home, ".claude", "CLAUDE.md")
			case "codex":
				base = filepath.Join(home, ".codex", "skills")
				hookConfig = filepath.Join(home, ".codex", "hooks.json")
				managedRulePath = filepath.Join(home, ".codex", "AGENTS.md")
			case "cursor":
				base = filepath.Join(home, ".cursor")
				hookConfig = filepath.Join(home, ".cursor", "hooks.json")
			case "dsh":
				var err error
				base, hookConfig, managedRulePath, err = dshInstallTargetPaths(home, "", scope)
				if err != nil {
					return nil, err
				}
			}
		} else {
			switch h {
			case "claude":
				base = filepath.Join(project, ".claude", "skills")
				hookConfig = filepath.Join(project, ".claude", "settings.json")
				managedRulePath = filepath.Join(project, "CLAUDE.md")
			case "codex":
				base = filepath.Join(project, ".codex", "skills")
				hookConfig = filepath.Join(project, ".codex", "hooks.json")
				managedRulePath = filepath.Join(project, "AGENTS.md")
			case "cursor":
				base = filepath.Join(project, ".cursor")
				hookConfig = filepath.Join(project, ".cursor", "hooks.json")
				managedRulePath = filepath.Join(project, ".cursor", "rules", "formal-gates.mdc")
			case "dsh":
				var err error
				base, hookConfig, managedRulePath, err = dshInstallTargetPaths("", project, scope)
				if err != nil {
					return nil, err
				}
			}
		}
		managedRule := ""
		if managedRulePath != "" {
			managedRule = filepath.Clean(managedRulePath)
		}
		hookConfigPath := ""
		if hookConfig != "" {
			hookConfigPath = filepath.Clean(hookConfig)
		}
		targets = append(targets, installTarget{
			host:            h,
			targetPath:      filepath.Clean(filepath.Join(base, "formal-gates")),
			hookConfig:      hookConfigPath,
			managedRulePath: managedRule,
		})
	}
	return targets, nil
}

func resolveInstallTargets(host, scope, project string) ([]installTarget, error) {
	normalizedHost, err := normalizeInstallHost(host)
	if err != nil {
		return nil, err
	}
	normalizedScope, err := normalizeInstallScope(scope)
	if err != nil {
		return nil, err
	}
	projectAbs := ""
	if normalizedScope == "project" || strings.TrimSpace(project) != "" {
		if strings.TrimSpace(project) == "" {
			return nil, fmt.Errorf("--project is required when --scope project is used")
		}
		projectAbs, err = filepath.Abs(project)
		if err != nil {
			return nil, err
		}
		projectAbs = filepath.Clean(projectAbs)
	}
	return installTargets(normalizedHost, normalizedScope, projectAbs)
}

func installTargetReport(target installTarget) InstallTargetReport {
	return InstallTargetReport{
		Host:            target.host,
		TargetPath:      filepath.ToSlash(target.targetPath),
		LauncherPath:    filepath.ToSlash(target.launcherPath),
		HookConfig:      filepath.ToSlash(target.hookConfig),
		ManagedRulePath: filepath.ToSlash(target.managedRulePath),
	}
}

func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func installHomeDir() (string, error) {
	for _, name := range []string{"HOME", "USERPROFILE"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			abs, err := filepath.Abs(value)
			if err != nil {
				return "", err
			}
			return filepath.Clean(abs), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve home directory: %w", err)
	}
	return filepath.Clean(home), nil
}

func copyInstallRuntime(source, target string, force bool) error {
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	if samePath(source, target) {
		return nil
	}
	if exists(target) {
		if !force {
			return fmt.Errorf("target already exists: %s; re-run with --force to replace it", target)
		}
		if err := removeExistingInstallTarget(target); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	runtimeFaultChecked := false
	entries := append(append([]string{}, installRuntimeEntries...), installPackageEntries...)
	for _, entry := range entries {
		from := filepath.Join(source, filepath.FromSlash(entry))
		to := filepath.Join(target, filepath.FromSlash(entry))
		if !exists(from) {
			// Runtime-only fixtures may omit optional package-validation inputs;
			// complete release packages copy them so installed targets validate
			// independently of the source checkout.
			continue
		}
		if err := copyPath(from, to); err != nil {
			return err
		}
		if err := installFault("copy-component:" + entry); err != nil {
			return err
		}
		if !runtimeFaultChecked {
			runtimeFaultChecked = true
			if err := installFault("copy-component:runtime"); err != nil {
				return err
			}
		}
	}
	return removePycache(target)
}

func removeExistingInstallTarget(target string) error {
	target = filepath.Clean(target)
	leaf := filepath.Base(target)
	parentLeaf := filepath.Base(filepath.Dir(target))
	if leaf != "formal-gates" || (parentLeaf != "skills" && parentLeaf != ".cursor") {
		return fmt.Errorf("refusing to replace unexpected target path: %s", target)
	}
	return os.RemoveAll(target)
}

func copyPath(from, to string) error {
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(from, to)
	}
	return copyFile(from, to, info.Mode())
}

func copyDir(from, to string) error {
	return filepath.WalkDir(from, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		if shouldSkipNativeInstallEntry(rel, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func shouldSkipNativeInstallEntry(rel string, entry os.DirEntry) bool {
	if rel == "." {
		return false
	}
	name := strings.ToLower(entry.Name())
	if entry.IsDir() {
		return name == "__pycache__"
	}
	switch filepath.Ext(name) {
	case ".ps1", ".psm1", ".psd1", ".py", ".pyc", ".pyo", ".sh", ".bash", ".bat", ".cmd", ".js", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

func copyFile(from, to string, mode os.FileMode) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o600
	}
	if filepath.Base(to) == nativeBinaryName() {
		// A package fixture may have been copied with a non-executable mode;
		// installed native binaries must still be runnable by the smoke gate.
		mode |= 0o111
	}
	return os.WriteFile(to, data, mode.Perm())
}

func removePycache(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == "__pycache__" {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			return filepath.SkipDir
		}
		return nil
	})
}

func configureInstallHook(target installTarget) (bool, error) {
	if strings.TrimSpace(target.hookConfig) == "" {
		return false, nil
	}
	if target.host == "dsh" {
		if err := configureDshHook(target); err != nil {
			return false, err
		}
		return true, nil
	}
	before, beforeErr := os.ReadFile(target.hookConfig)
	if beforeErr != nil && !os.IsNotExist(beforeErr) {
		return false, beforeErr
	}
	config, err := readHookConfig(target.hookConfig)
	if err != nil {
		return false, err
	}
	lifecycleHooks, err := lifecycle.HookDefinitions(target.host)
	if err != nil {
		return false, err
	}
	hooks := hookObject(config)
	gateArgs := []string{"hook", "decide"}
	if target.host == "codex" {
		gateArgs = append(gateArgs, "--provider", "codex")
	}
	gateCommand := nativeInstallCommand(targetLauncherPath(target), gateArgs...)
	var desired map[string]any
	shape := "nested"
	switch target.host {
	case "claude":
		desired = map[string]any{
			"PreToolUse": nestedHookEntry("*", gateCommand, false),
		}
	case "codex":
		desired = map[string]any{
			"PreToolUse": nestedHookEntry(hostMatcher("codex"), gateCommand, true),
		}
	case "cursor":
		shape = "flat"
		config["version"] = float64(1)
		desired = map[string]any{
			"preToolUse": flatHookEntry(gateCommand),
		}
	}
	for _, hook := range lifecycleHooks {
		command := nativeInstallCommand(targetLauncherPath(target), hook.Command...)
		if shape == "flat" {
			desired[hook.Event] = flatHookEntry(command)
		} else {
			desired[hook.Event] = nestedHookEntry(hostMatcher(target.host), command, target.host == "codex")
		}
	}
	for event, entry := range desired {
		existing, _ := hooks[event].([]any)
		hooks[event] = append(removeFormalGatesHookEntries(existing, target, shape), entry)
	}
	for event, value := range hooks {
		if _, ok := desired[event]; ok {
			continue
		}
		existing, ok := value.([]any)
		if !ok {
			continue
		}
		hooks[event] = removeFormalGatesHookEntries(existing, target, shape)
	}
	config["hooks"] = hooks
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false, err
	}
	desiredBytes := append(data, '\n')
	if beforeErr == nil && string(before) == string(desiredBytes) {
		return false, nil
	}
	if err := writeHookConfig(target.hookConfig, config); err != nil {
		return false, err
	}
	return true, nil
}

func removeInstallHooks(target installTarget) error {
	if strings.TrimSpace(target.hookConfig) == "" {
		return nil
	}
	if target.host == "dsh" {
		return removeDshHook(target)
	}
	if !isFile(target.hookConfig) {
		return nil
	}
	config, err := readHookConfig(target.hookConfig)
	if err != nil {
		return err
	}
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	before, _ := json.Marshal(config)
	shape := "nested"
	if target.host == "cursor" {
		shape = "flat"
	}
	for event, value := range hooks {
		existing, ok := value.([]any)
		if !ok {
			continue
		}
		hooks[event] = removeFormalGatesHookEntries(existing, target, shape)
	}
	after, _ := json.Marshal(config)
	if string(before) == string(after) {
		return nil
	}
	return writeHookConfig(target.hookConfig, config)
}

func readHookConfig(path string) (map[string]any, error) {
	if !isFile(path) {
		return map[string]any{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return map[string]any{}, nil
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("existing hook config is not valid JSON; refusing to touch it: %s", path)
	}
	if config == nil {
		return nil, fmt.Errorf("existing hook config must be a JSON object; refusing to touch it: %s", path)
	}
	if rawHooks, present := config["hooks"]; present {
		hooks, ok := rawHooks.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("existing hook config has malformed hooks object; refusing to touch it: %s", path)
		}
		for event, rawEntries := range hooks {
			if _, ok := rawEntries.([]any); !ok {
				return nil, fmt.Errorf("existing hook config has malformed %s entries; refusing to touch it: %s", event, path)
			}
		}
	}
	return config, nil
}

func writeHookConfig(path string, config map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data, 0o600)
}

func hookObject(config map[string]any) map[string]any {
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
	}
	return hooks
}

// hostMatcher returns the tool-name matcher for a host: Claude Code uses the glob
// "*" (matches every tool), Codex uses the regex ".*" — Codex's matcher is a regex,
// so the glob "*" is an invalid pattern that matches nothing and the hook never fires.
func hostMatcher(host string) string {
	if host == "codex" {
		return ".*"
	}
	return "*"
}

func nestedHookEntry(matcher, command string, timeout bool) map[string]any {
	hook := map[string]any{
		"type":    "command",
		"command": command,
	}
	if timeout {
		hook["timeout"] = float64(30)
	}
	return map[string]any{
		"matcher": matcher,
		"hooks":   []any{hook},
	}
}

func flatHookEntry(command string) map[string]any {
	return map[string]any{
		"command":    command,
		"timeout":    float64(30),
		"failClosed": true,
	}
}

func removeFormalGatesHookEntries(entries []any, target installTarget, shape string) []any {
	kept := make([]any, 0, len(entries))
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			kept = append(kept, entry)
			continue
		}
		if shape == "nested" {
			nested, ok := entryMap["hooks"].([]any)
			if ok {
				remaining := make([]any, 0, len(nested))
				removed := false
				for _, hook := range nested {
					if !isInstallerHookValue(entryMap, hook, target, shape) {
						remaining = append(remaining, hook)
					} else {
						removed = true
					}
				}
				if !removed {
					kept = append(kept, entryMap)
				} else if len(remaining) > 0 {
					entryMap["hooks"] = remaining
					kept = append(kept, entryMap)
				}
				continue
			}
		}
		if !isInstallerHookValue(nil, entryMap, target, shape) {
			kept = append(kept, entryMap)
		}
	}
	return kept
}

func isInstallerHookValue(parent map[string]any, value any, target installTarget, shape string) bool {
	command, ok := value.(map[string]any)
	if !ok {
		return false
	}
	commandText, ok := command["command"].(string)
	if !ok || !installerHookCommands(target)[normalizeHookCommand(commandText)] {
		return false
	}
	if shape == "nested" {
		// matcher 接受 "*"（Claude Code 与 legacy hook）或 ".*"（Codex 正则），
		// 其余值视为非 formal-gates 安装的 hook。
		if !exactObjectKeys(parent, "matcher", "hooks") || (parent["matcher"] != "*" && parent["matcher"] != ".*") {
			return false
		}
		return exactNestedHookShape(command, target.host) ||
			(exactLegacyNestedHookShape(command) &&
				(isLegacyInstallerHookCommand(commandText) || isLegacyCodexGateCommand(commandText, target)))
	}
	return exactFlatHookShape(command) ||
		(exactLegacyFlatHookShape(command) && isLegacyInstallerHookCommand(commandText))
}

func installerHookCommands(target installTarget) map[string]bool {
	commands := map[string]bool{}
	launchers := []string{targetLauncherPath(target), filepath.Join(target.targetPath, "bin", nativeBinaryName())}
	add := func(args ...string) {
		for _, launcher := range launchers {
			commands[normalizeHookCommand(nativeInstallCommand(launcher, args...))] = true
		}
	}
	gateArgs := []string{"hook", "decide"}
	if target.host == "codex" {
		add("hook", "decide")
		gateArgs = append(gateArgs, "--provider", "codex")
	}
	add(gateArgs...)
	lifecycleHooks, err := lifecycle.HookDefinitions(target.host)
	if err == nil {
		for _, hook := range lifecycleHooks {
			add(hook.Command...)
		}
	}
	for _, command := range []string{
		"pwsh -File hooks/" + "enforce-" + "gate-sequence.ps1",
		"pwsh -File hooks/" + "capture-" + "subagent-receipt.ps1",
	} {
		commands[command] = true
	}
	return commands
}

func exactNestedHookShape(value map[string]any, host string) bool {
	if value == nil || value["type"] != "command" {
		return false
	}
	if host == "codex" {
		return exactObjectKeys(value, "type", "command", "timeout") && value["timeout"] == float64(30)
	}
	return exactObjectKeys(value, "type", "command")
}

func exactFlatHookShape(value map[string]any) bool {
	return value != nil && exactObjectKeys(value, "command", "timeout", "failClosed") &&
		value["timeout"] == float64(30) && value["failClosed"] == true
}

func exactLegacyNestedHookShape(value map[string]any) bool {
	return value != nil && exactObjectKeys(value, "type", "command") && value["type"] == "command"
}

func exactLegacyFlatHookShape(value map[string]any) bool {
	return value != nil && exactObjectKeys(value, "command")
}

func isLegacyInstallerHookCommand(command string) bool {
	return command == "pwsh -File hooks/"+"enforce-"+"gate-sequence.ps1" ||
		command == "pwsh -File hooks/"+"capture-"+"subagent-receipt.ps1"
}

func isLegacyCodexGateCommand(command string, target installTarget) bool {
	if target.host != "codex" {
		return false
	}
	want := normalizeHookCommand(command)
	// Stage 0 explicitly migrates the former target/bin hook to the fixed
	// launcher. Recognizing that one exact old owned shape is cleanup for the
	// migration, not a second supported writer or fallback launcher.
	return want == normalizeHookCommand(nativeInstallCommand(targetLauncherPath(target), "hook", "decide")) ||
		want == normalizeHookCommand(nativeInstallCommand(filepath.Join(target.targetPath, "bin", nativeBinaryName()), "hook", "decide"))
}

func exactObjectKeys(value map[string]any, expected ...string) bool {
	if value == nil || len(value) != len(expected) {
		return false
	}
	keys := make(map[string]bool, len(expected))
	for _, key := range expected {
		keys[key] = true
	}
	for key := range value {
		if !keys[key] {
			return false
		}
	}
	return true
}

func nativeInstallCommand(skillRoot string, args ...string) string {
	launcher := skillRoot
	if filepath.Base(filepath.Clean(launcher)) != nativeBinaryName() {
		launcher = filepath.Join(skillRoot, "bin", nativeBinaryName())
	}
	parts := []string{quoteCommandArg(launcher)}
	for _, arg := range args {
		if isPlainCommandToken(arg) {
			parts = append(parts, arg)
			continue
		}
		parts = append(parts, quoteCommandArg(arg))
	}
	return strings.Join(parts, " ")
}

func targetLauncherPath(target installTarget) string {
	if strings.TrimSpace(target.launcherPath) != "" {
		return target.launcherPath
	}
	return filepath.Join(target.targetPath, "bin", nativeBinaryName())
}

// normalizeHookCommand 去掉命令字符串里的双引号，用于卸载/升级时识别新旧两种 install
// 格式：旧版无条件给 exe 路径加引号、新版仅在需要时加引号，归一化后相同。
func normalizeHookCommand(command string) string {
	return strings.ReplaceAll(command, `"`, "")
}

func quoteCommandArg(value string) string {
	value = slashCommandPath(value)
	if !requiresShellQuoting(value) {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

// requiresShellQuoting 只在值含空白或 shell 元字符时才需要引号。无空格/特殊字符的路径
// 不加引号：Codex 0.146 会把带引号的 exe 路径当成命令的一部分执行失败，导致 hook 被判
// Failed 后 fail-open 放行。
func requiresShellQuoting(value string) bool {
	for _, r := range value {
		switch r {
		case ' ', '\t', '"', '\'', '&', '|', ';', '<', '>', '(', ')', '^', '`':
			return true
		}
	}
	return false
}

func slashCommandPath(value string) string {
	if strings.Contains(value, `\`) || filepath.IsAbs(value) {
		return filepath.ToSlash(value)
	}
	return value
}

func isPlainCommandToken(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '-', '_', '.', '/':
			continue
		default:
			return false
		}
	}
	return true
}
