package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	Source  string
	Host    string
	Scope   string
	Project string
}

type InstallReport struct {
	Targets              []InstallTargetReport `json:"targets"`
	GeneratedAt          string                `json:"generatedAt,omitempty"`
	Registry             string                `json:"registry,omitempty"`
	ReceiptPath          string                `json:"receiptPath,omitempty"`
	BootstrapReceiptPath string                `json:"bootstrapReceiptPath,omitempty"`
}

type InstallTargetReport struct {
	Host              string            `json:"host"`
	TargetPath        string            `json:"targetPath"`
	HookConfig        string            `json:"hookConfig,omitempty"`
	ManagedRulePath   string            `json:"managedRulePath,omitempty"`
	SourceRoot        string            `json:"sourceRoot,omitempty"`
	SourceDigest      string            `json:"sourceDigest,omitempty"`
	InstalledDigest   string            `json:"installedDigest,omitempty"`
	HookDigest        string            `json:"hookDigest,omitempty"`
	ManagedRuleDigest string            `json:"managedRuleDigest,omitempty"`
	Manifest          []PackageEntry    `json:"manifest,omitempty"`
	CanonicalPaths    map[string]string `json:"canonicalPaths,omitempty"`
	Smoke             string            `json:"smoke,omitempty"`
}

type UninstallReport struct {
	Targets []InstallTargetReport `json:"targets"`
}

type installTarget struct {
	host            string
	targetPath      string
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
	for _, target := range targets {
		if pathOverlaps(sourceAbs, target.targetPath) {
			return InstallReport{}, fmt.Errorf("install source %s overlaps target %s", sourceAbs, target.targetPath)
		}
	}
	if strings.TrimSpace(options.ReleaseRoot) != "" && pathOverlaps(sourceAbs, options.ReleaseRoot) {
		return InstallReport{}, fmt.Errorf("install source %s overlaps release root %s", sourceAbs, options.ReleaseRoot)
	}
	sourcePackage, err := PackageReceipt(sourceAbs)
	if err != nil {
		return InstallReport{}, fmt.Errorf("formal-gates source failed immutable package validation: %w", err)
	}
	registryPath := installRegistryPath(options)
	// A source checkout/release must pass the complete package contract.  The
	// documented runtime-only install fixture intentionally omits development
	// sources, so it still receives strict manifest validation without being
	// rejected for files that are not part of the copied runtime subset.
	if isFile(filepath.Join(sourceAbs, "internal", "validate", "runner.go")) {
		if packageResult := Package(sourceAbs); !packageResult.OK() {
			return InstallReport{}, fmt.Errorf("formal-gates source failed complete package validation: %s", resultSummary(packageResult))
		}
	} else {
		manifestData, readErr := os.ReadFile(filepath.Join(sourceAbs, "formal-gates.manifest.json"))
		if readErr != nil {
			return InstallReport{}, fmt.Errorf("formal-gates manifest cannot be read: %w", readErr)
		}
		var manifestShape map[string]any
		if unmarshalErr := json.Unmarshal(manifestData, &manifestShape); unmarshalErr != nil {
			return InstallReport{}, fmt.Errorf("formal-gates manifest JSON is invalid: %w", unmarshalErr)
		}
		if name, _ := manifestShape["name"].(string); name != "formal-gates" {
			return InstallReport{}, fmt.Errorf("formal-gates manifest name must be formal-gates")
		}
		expected, _ := manifestShape["package_digest"].(string)
		if strings.TrimSpace(expected) == "" {
			expected, _ = manifestShape["packageDigest"].(string)
		}
		if strings.TrimSpace(expected) != "" && !digestMatches(expected, sourcePackage.Digest) {
			return InstallReport{}, fmt.Errorf("formal-gates package digest mismatch: expected %s, got sha256:%s", expected, sourcePackage.Digest)
		}
	}
	if options.Bootstrap {
		return bootstrapInstall(options, targets, sourcePackage, registryPath)
	}
	rule, err := LoadManagedRule(sourceAbs)
	if err != nil {
		return InstallReport{}, err
	}
	report := InstallReport{GeneratedAt: "sha256:" + sourcePackage.Digest}
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
		if faultErr := installFault("registry"); faultErr != nil {
			return InstallReport{}, recordInstallFailure(installJournalPath(filepath.FromSlash(report.Registry)), installJournal{Operation: "install", Target: registryPath, Phase: "intent", CreatedAt: nowReceiptTime()}, faultErr)
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
		outer.Targets = append(outer.Targets, outerTargetSnapshot{TargetPath: target.targetPath, HookPath: target.hookConfig, RulePath: target.managedRulePath, Tree: outerTreeFromBackup(tree), Hook: hook, Rule: rule})
	}
	var releaseBackup installTreeBackup
	if strings.TrimSpace(options.ReleaseRoot) != "" {
		releaseBackup, err = snapshotInstallTree(options.ReleaseRoot, filepath.Join(transactionRoot, "release"))
		if err != nil {
			return InstallReport{}, err
		}
		outer.Release = outerTreeFromBackup(releaseBackup)
		outer.Binary, err = snapshotOuterFile(options.BinaryTarget, filepath.Join(transactionRoot, "binary.before"))
		if err != nil {
			return InstallReport{}, err
		}
	}
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, err
	}
	rollbackAll := func() {
		_ = restoreOuterJournal(outerPath, outer, false)
	}
	if strings.TrimSpace(options.ReleaseRoot) != "" {
		if err := installReleaseTransaction(sourceAbs, options.ReleaseRoot, options.BinaryTarget, options.Force); err != nil {
			rollbackAll()
			return InstallReport{}, err
		}
	}
	records := make([]RegistryRecord, 0, len(targets))
	for _, target := range targets {
		if err := executeInstallTransaction(sourceAbs, target, options.Force, options.SkipHooks, rule); err != nil {
			rollbackAll()
			return InstallReport{}, err
		}
		installedReceipt, receiptErr := PackageReceipt(target.targetPath)
		if receiptErr != nil {
			rollbackAll()
			return InstallReport{}, fmt.Errorf("installed target receipt failed: %w", receiptErr)
		}
		targetReport := InstallTargetReport{
			Host:            target.host,
			TargetPath:      filepath.ToSlash(target.targetPath),
			ManagedRulePath: filepath.ToSlash(target.managedRulePath),
			SourceRoot:      filepath.ToSlash(sourceAbs),
			SourceDigest:    sourcePackage.Digest,
			InstalledDigest: installedReceipt.Digest,
			Manifest:        sourcePackage.Entries,
			CanonicalPaths:  map[string]string{"sourceRoot": canonicalPath(sourceAbs), "target": canonicalPath(target.targetPath)},
			Smoke:           "PASS",
		}
		if !options.SkipHooks {
			targetReport.HookConfig = filepath.ToSlash(target.hookConfig)
			targetReport.HookDigest, _ = fileDigest(target.hookConfig)
			targetReport.CanonicalPaths["hookConfig"] = canonicalPath(target.hookConfig)
		}
		targetReport.ManagedRuleDigest, _ = fileDigest(target.managedRulePath)
		if target.managedRulePath != "" {
			targetReport.CanonicalPaths["managedRule"] = canonicalPath(target.managedRulePath)
		}
		report.Targets = append(report.Targets, targetReport)
		if registryPath != "" {
			records = append(records, installRegistryRecord(target, options))
		}
	}
	outer.Phase = "switched"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		rollbackAll()
		return InstallReport{}, err
	}
	if registryPath != "" {
		if faultErr := installFault("registry-commit"); faultErr != nil {
			rollbackAll()
			return InstallReport{}, recordInstallFailure(installJournalPath(filepath.FromSlash(report.Registry)), installJournal{Operation: "install", Target: registryPath, Phase: "switched", CreatedAt: nowReceiptTime()}, faultErr)
		}
		if err := commitRegistryRecords(registryPath, records); err != nil {
			rollbackAll()
			return InstallReport{}, fmt.Errorf("installation registry admission bridge commit failed: %w", err)
		}
	}
	outer.Phase = "registry-committed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		rollbackAll()
		return InstallReport{}, err
	}
	if report.ReceiptPath != "" {
		if writeErr := writeJSONAtomically(filepath.FromSlash(report.ReceiptPath), report); writeErr != nil {
			rollbackAll()
			return InstallReport{}, fmt.Errorf("failed to persist install receipt: %w", writeErr)
		}
	}
	outer.Phase = "committed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		rollbackAll()
		return InstallReport{}, err
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
	registryPath := installRegistryPath(InstallOptions{Host: options.Host, Scope: options.Scope, Project: options.Project})
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
	rollback := func() {
		_ = restoreOuterJournal(outerPath, outer, false)
	}
	report := UninstallReport{}
	for _, target := range targets {
		if err := executeUninstallTransaction(target); err != nil {
			rollback()
			return UninstallReport{}, err
		}
		report.Targets = append(report.Targets, installTargetReport(target))
	}
	outer.Phase = "switched"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		rollback()
		return UninstallReport{}, err
	}
	if registryPath != "" {
		if doc, loadErr := LoadRegistry(registryPath); loadErr == nil {
			for index := range doc.Records {
				for _, target := range targets {
					if filepath.Clean(doc.Records[index].Target) == filepath.Clean(target.targetPath) {
						doc.Records[index].Status = "disabled"
					}
				}
			}
			doc.Epoch++
			if err := writeJSONAtomically(registryPath, doc); err != nil {
				rollback()
				return UninstallReport{}, fmt.Errorf("uninstall registry bridge update failed: %w", err)
			}
		} else if !os.IsNotExist(loadErr) {
			rollback()
			return UninstallReport{}, fmt.Errorf("uninstall registry bridge unavailable: %w", loadErr)
		}
	}
	outer.Phase = "registry-committed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		rollback()
		return UninstallReport{}, err
	}
	outer.Phase = "committed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		rollback()
		return UninstallReport{}, err
	}
	_ = os.RemoveAll(outer.TransactionRoot)
	_ = os.Remove(outerPath)
	return report, nil
}

// installTreeBackup is the outer install transaction's undo record.  The
// per-target native transaction protects one target; this record lets the
// owner undo already completed targets when a later host, release, registry,
// or receipt commit fails.
type installTreeBackup struct {
	path    string
	backup  string
	existed bool
}

type installTargetBackup struct {
	target installTarget
	tree   installTreeBackup
	hook   installFileBackup
	rule   installFileBackup
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
	if err := copyTreeImmutable(state.path, state.backup); err != nil {
		return installTreeBackup{}, err
	}
	return state, nil
}

func restoreInstallTree(state installTreeBackup) error {
	if strings.TrimSpace(state.path) == "" {
		return nil
	}
	if err := os.RemoveAll(state.path); err != nil {
		return err
	}
	if !state.existed {
		return nil
	}
	return copyTreeImmutable(state.backup, state.path)
}

func snapshotInstallTarget(target installTarget, backup string) (installTargetBackup, error) {
	tree, err := snapshotInstallTree(target.targetPath, backup)
	if err != nil {
		return installTargetBackup{}, err
	}
	hook, err := snapshotInstallFile(target.hookConfig)
	if err != nil {
		return installTargetBackup{}, err
	}
	rule, err := snapshotInstallFile(target.managedRulePath)
	if err != nil {
		return installTargetBackup{}, err
	}
	return installTargetBackup{target: target, tree: tree, hook: hook, rule: rule}, nil
}

func restoreInstallTarget(state installTargetBackup) error {
	if err := restoreInstallTree(state.tree); err != nil {
		return err
	}
	if err := restoreInstallFile(state.hook); err != nil {
		return err
	}
	return restoreInstallFile(state.rule)
}

func installRegistryRecord(target installTarget, options InstallOptions) RegistryRecord {
	projectRoot := options.Project
	if strings.EqualFold(options.Scope, "global") {
		projectRoot, _ = installHomeDir()
	}
	projectRoot = absPath(projectRoot)
	stateRoot := filepath.Join(projectRoot, ".gates")
	resourceRoot := filepath.Join(projectRoot, ".formal-gates-resources")
	canonical := map[string]string{
		"target":         canonicalPath(target.targetPath),
		"projectRoot":    canonicalPath(projectRoot),
		"stateRoot":      canonicalPath(stateRoot),
		"resourceRoot":   canonicalPath(resourceRoot),
		"runtimeSibling": canonicalPath(filepath.Dir(target.targetPath)),
	}
	if target.hookConfig != "" {
		canonical["hookConfig"] = canonicalPath(target.hookConfig)
	}
	return RegistryRecord{
		ID:             fmt.Sprintf("%s-%s", target.host, strings.ToLower(options.Scope)),
		Target:         target.targetPath,
		Scope:          strings.ToLower(options.Scope),
		Host:           target.host,
		HookConfig:     target.hookConfig,
		ProjectRoot:    projectRoot,
		StateRoot:      stateRoot,
		ResourceRoot:   resourceRoot,
		RuntimeSibling: filepath.Dir(target.targetPath),
		CanonicalPaths: canonical,
		Status:         "active",
	}
}

func installRegistryPath(options InstallOptions) string {
	if path := strings.TrimSpace(options.RegistryPath); path != "" {
		return absPath(path)
	}
	if strings.EqualFold(options.Scope, "project") && strings.TrimSpace(options.Project) != "" {
		return filepath.Join(absPath(options.Project), ".gates", "registry.json")
	}
	if strings.EqualFold(options.Scope, "global") {
		if home, err := installHomeDir(); err == nil {
			return filepath.Join(home, ".formal-gates", "registry.json")
		}
	}
	return ""
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
	outer.Registry, err = snapshotOuterFile(registryPath, filepath.Join(transactionRoot, "registry.before"))
	if err != nil {
		return InstallReport{}, err
	}
	outer.Receipt, err = snapshotOuterFile(receiptPath, filepath.Join(transactionRoot, "bootstrap-receipt.before"))
	if err != nil {
		return InstallReport{}, err
	}
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, err
	}
	rollback := func(cause error) error {
		_ = restoreOuterJournal(outerPath, outer, false)
		return outerJournalFailure(outerPath, outer, cause)
	}
	if faultErr := installFault("registry"); faultErr != nil {
		return InstallReport{}, rollback(faultErr)
	}
	if existing, loadErr := LoadRegistry(registryPath); loadErr == nil {
		for _, record := range existing.Records {
			for _, target := range targets {
				if record.ID == installRegistryRecord(target, options).ID && filepath.Clean(record.Target) != filepath.Clean(target.targetPath) {
					receipt := AdmissionReceipt{Code: "UNREGISTERED_INSTALL", Accepted: false, RecordID: record.ID, Registry: registryPath, Reason: "bootstrap target conflicts with an existing registry record", CreatedAt: nowReceiptTime()}
					_ = writeAdmissionReceipt(registryPath, receipt)
					return InstallReport{}, rollback(fmt.Errorf("UNREGISTERED_INSTALL: bootstrap target conflicts with registry record %q", record.ID))
				}
			}
		}
	} else if !os.IsNotExist(loadErr) {
		return InstallReport{}, fmt.Errorf("registry bootstrap cannot read existing registry: %w", loadErr)
	}
	records := make([]RegistryRecord, 0, len(targets))
	for _, target := range targets {
		records = append(records, installRegistryRecord(target, options))
	}
	if err := commitRegistryRecords(registryPath, records); err != nil {
		return InstallReport{}, rollback(err)
	}
	committedRegistry, err := LoadRegistry(registryPath)
	if err != nil {
		return InstallReport{}, rollback(err)
	}
	outer.Phase = "registry-committed"
	if err := persistOuterJournal(outerPath, outer); err != nil {
		return InstallReport{}, rollback(err)
	}
	receipt := map[string]any{
		"operation":     "bootstrap",
		"registry":      filepath.Clean(registryPath),
		"packageDigest": source.Digest,
		"records":       committedRegistry.Records,
		"stateCreated":  false,
		"observedAt":    nowReceiptTime(),
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
	return InstallReport{GeneratedAt: "sha256:" + source.Digest, Registry: filepath.ToSlash(registryPath), ReceiptPath: filepath.ToSlash(receiptPath), BootstrapReceiptPath: filepath.ToSlash(receiptPath)}, nil
}

func commitRegistryRecords(path string, records []RegistryRecord) error {
	doc, err := loadRegistryForCommit(path)
	if err != nil {
		return err
	}
	_, err = commitRegistryRecordsUnlocked(path, doc, records)
	return err
}

func nowReceiptTime() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
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
	for _, entry := range installRuntimeEntries {
		from := filepath.Join(source, filepath.FromSlash(entry))
		to := filepath.Join(target, filepath.FromSlash(entry))
		if err := copyPath(from, to); err != nil {
			return err
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

// isLiveEntry is retained as a compatibility helper for callers that used the
// old package copier. Stage 0 packages are immutable: no runtime entry may be
// a live symlink back to the source worktree.
func isLiveEntry(entry string) bool {
	return false
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

func configureInstallHook(target installTarget) error {
	if strings.TrimSpace(target.hookConfig) == "" {
		return nil
	}
	if target.host == "dsh" {
		return configureDshHook(target)
	}
	config, err := readHookConfig(target.hookConfig)
	if err != nil {
		return err
	}
	lifecycleHooks, err := lifecycle.HookDefinitions(target.host)
	if err != nil {
		return err
	}
	hooks := hookObject(config)
	gateArgs := []string{"hook", "decide"}
	if target.host == "codex" {
		gateArgs = append(gateArgs, "--provider", "codex")
	}
	gateCommand := nativeInstallCommand(target.targetPath, gateArgs...)
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
		command := nativeInstallCommand(target.targetPath, hook.Command...)
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
	return writeHookConfig(target.hookConfig, config)
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
	return config, nil
}

func writeHookConfig(path string, config map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if isFile(path) {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path+".bak", data, 0o600); err != nil {
			return err
		}
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
	add := func(args ...string) {
		commands[normalizeHookCommand(nativeInstallCommand(target.targetPath, args...))] = true
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
	return normalizeHookCommand(command) == normalizeHookCommand(nativeInstallCommand(target.targetPath, "hook", "decide"))
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
	parts := []string{quoteCommandArg(filepath.Join(skillRoot, "bin", nativeBinaryName()))}
	for _, arg := range args {
		if isPlainCommandToken(arg) {
			parts = append(parts, arg)
			continue
		}
		parts = append(parts, quoteCommandArg(arg))
	}
	return strings.Join(parts, " ")
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
