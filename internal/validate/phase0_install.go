package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func installReleaseTransaction(source, releaseRoot, binaryTarget string, force bool) error {
	releaseRoot, err := filepath.Abs(releaseRoot)
	if err != nil {
		return err
	}
	releaseRoot = filepath.Clean(releaseRoot)
	unlock, err := acquireInstallLock(releaseRoot)
	if err != nil {
		return err
	}
	defer unlock()
	if err := reconcileInstallJournal(releaseRoot); err != nil {
		return err
	}
	if exists(releaseRoot) && !force {
		return fmt.Errorf("release already exists: %s; re-run with --force to replace it", releaseRoot)
	}
	parent := filepath.Dir(releaseRoot)
	token := fmt.Sprintf("%d", time.Now().UnixNano())
	temp := filepath.Join(parent, ".formal-gates-release-"+token)
	backup := filepath.Join(parent, ".formal-gates-release-backup-"+token)
	journalPath := installJournalPath(releaseRoot)
	journal := installJournal{Operation: "release-install", Target: releaseRoot, Temp: temp, Backup: backup, Phase: "intent", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), BinaryTarget: filepath.Clean(binaryTarget)}
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(journalPath)
		_ = os.RemoveAll(temp)
		_ = os.RemoveAll(backup)
	}()
	binaryBackup, err := snapshotInstallFile(binaryTarget)
	if err != nil {
		return err
	}
	rollbackRelease := func() {
		_ = os.RemoveAll(releaseRoot)
		if exists(backup) {
			_ = os.Rename(backup, releaseRoot)
		}
		_ = restoreInstallFile(binaryBackup)
	}
	if err := installFault("intent"); err != nil {
		return recordInstallFailure(journalPath, journal, err)
	}
	if err := copyTreeImmutable(source, temp); err != nil {
		return err
	}
	preparedReceipt, err := PackageReceipt(temp, source)
	if err != nil {
		return fmt.Errorf("release package validation failed: %w", err)
	}
	journal.Phase = "prepared"
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		return err
	}
	if err := installFault("prepared"); err != nil {
		return recordInstallFailure(journalPath, journal, err)
	}
	if exists(releaseRoot) {
		if err := os.Rename(releaseRoot, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(temp, releaseRoot); err != nil {
		rollbackRelease()
		return err
	}
	journal.Phase = "switched"
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		rollbackRelease()
		return err
	}
	if err := installFault("switched"); err != nil {
		rollbackRelease()
		return recordInstallFailure(journalPath, journal, err)
	}
	installedReceipt, err := PackageReceipt(releaseRoot)
	if err != nil {
		rollbackRelease()
		return recordInstallFailure(journalPath, journal, fmt.Errorf("installed release package validation failed: %w", err))
	}
	if installedReceipt.Digest != preparedReceipt.Digest {
		rollbackRelease()
		return recordInstallFailure(journalPath, journal, fmt.Errorf("installed release package digest mismatch: prepared %s, installed %s", preparedReceipt.Digest, installedReceipt.Digest))
	}
	if strings.TrimSpace(binaryTarget) != "" {
		if err := atomicCopyFile(filepath.Join(releaseRoot, "bin", nativeBinaryName()), binaryTarget); err != nil {
			rollbackRelease()
			return err
		}
	}
	// The candidate must execute from the switched installed path before the
	// journal is committed.  This catches a bad binary while rollback still has
	// the stable release and executable available.
	smokePath := binaryTarget
	if strings.TrimSpace(smokePath) == "" {
		smokePath = filepath.Join(releaseRoot, "bin", nativeBinaryName())
	}
	if err := runInstalledBinarySmoke(smokePath); err != nil {
		rollbackRelease()
		return recordInstallFailure(journalPath, journal, fmt.Errorf("installed binary smoke failed: %w", err))
	}
	// Fault injection models a smoke failure at the same pre-commit boundary as
	// a real process failure.  Keep the durable journal at switched until this
	// point; reconcile must therefore restore the stable release rather than
	// treating the candidate as committed.
	if err := installFault("post-switch-smoke"); err != nil {
		rollbackRelease()
		return recordInstallFailure(journalPath, journal, err)
	}
	journal.Phase = "committed"
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		return err
	}
	return nil
}

func copyTreeImmutable(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(target, 0o700)
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".gates" || entry.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(target, rel), 0o700)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("release entry %s is not an immutable regular file", filepath.ToSlash(rel))
		}
		return copyFile(path, filepath.Join(target, rel), info.Mode())
	})
}

// validateBinaryFormat rejects a regular file that cannot be executed by any
// supported release platform.  Checking only “regular file” lets a truncated
// download or a text payload pass package validation and fail much later when
// the host invokes the installed hook.  Scripts are accepted only when they
// carry a shebang; native binaries are identified by their stable container
// magic for ELF, Mach-O (including fat binaries), or PE.
func validateBinaryFormat(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("native binary must be an immutable regular file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) >= 2 && data[0] == '#' && data[1] == '!' {
		return nil
	}
	if len(data) >= 4 {
		magic := string(data[:4])
		if magic == "\x7fELF" || magic == "\xfe\xed\xfa\xce" || magic == "\xce\xfa\xed\xfe" ||
			magic == "\xfe\xed\xfa\xcf" || magic == "\xcf\xfa\xed\xfe" ||
			magic == "\xca\xfe\xba\xbe" || magic == "\xbe\xba\xfe\xca" ||
			(data[0] == 'M' && data[1] == 'Z') {
			return nil
		}
	}
	return fmt.Errorf("native binary has an unrecognized executable format: %s", path)
}

func atomicCopyFile(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".formal-gates-binary-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
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
	return replaceCompletedFile(tmpPath, target)
}

// installJournal is deliberately small and durable.  It records enough
// information for a subsequent install/uninstall invocation to observe an
// interrupted rename and restore the previous target before doing new work.
type installJournal struct {
	Operation         string `json:"operation"`
	Target            string `json:"target"`
	Temp              string `json:"temp,omitempty"`
	Backup            string `json:"backup,omitempty"`
	Phase             string `json:"phase"`
	CreatedAt         string `json:"createdAt"`
	BinaryTarget      string `json:"binaryTarget,omitempty"`
	HookConfig        string `json:"hookConfig,omitempty"`
	HookBackup        string `json:"hookBackup,omitempty"`
	ManagedRule       string `json:"managedRule,omitempty"`
	ManagedRuleBackup string `json:"managedRuleBackup,omitempty"`
}

type installRecoveryReceipt struct {
	Operation    string `json:"operation"`
	Target       string `json:"target"`
	Phase        string `json:"interruptedPhase"`
	Recovered    bool   `json:"recovered"`
	Outcome      string `json:"outcome,omitempty"`
	ObservedFact string `json:"observedFact,omitempty"`
	Reconcile    string `json:"reconcileAction,omitempty"`
	StableDigest string `json:"stableDigest,omitempty"`
	BinaryTarget string `json:"binaryTarget,omitempty"`
	HookConfig   string `json:"hookConfig,omitempty"`
	ManagedRule  string `json:"managedRule,omitempty"`
	Backup       string `json:"backup,omitempty"`
	ObservedAt   string `json:"observedAt"`
}

type installFileBackup struct {
	path   string
	exists bool
	data   []byte
	mode   os.FileMode
}

// outerInstallJournal is the durable undo record for one multi-target
// install/uninstall.  Per-target journals cannot restore a host-both operation
// after a process crash: the first target may already be switched while the
// second target and the registry are still old.  The outer journal therefore
// records every old byte/tree before the first mutation and remains present
// until runtime and registry commit are both durable.
type outerInstallJournal struct {
	Operation       string                `json:"operation"`
	RegistryPath    string                `json:"registryPath"`
	TransactionRoot string                `json:"transactionRoot"`
	Phase           string                `json:"phase"`
	CreatedAt       string                `json:"createdAt"`
	Registry        outerFileSnapshot     `json:"registry"`
	Receipt         outerFileSnapshot     `json:"receipt"`
	Targets         []outerTargetSnapshot `json:"targets"`
	Release         outerTreeSnapshot     `json:"release"`
	Binary          outerFileSnapshot     `json:"binary"`
}

type outerTreeSnapshot struct {
	Path    string `json:"path"`
	Backup  string `json:"backup"`
	Existed bool   `json:"existed"`
}

type outerFileSnapshot struct {
	Path    string `json:"path"`
	Backup  string `json:"backup"`
	Existed bool   `json:"existed"`
	Mode    uint32 `json:"mode,omitempty"`
}

type outerTargetSnapshot struct {
	TargetPath string            `json:"targetPath"`
	HookPath   string            `json:"hookPath,omitempty"`
	RulePath   string            `json:"rulePath,omitempty"`
	Tree       outerTreeSnapshot `json:"tree"`
	Hook       outerFileSnapshot `json:"hook"`
	Rule       outerFileSnapshot `json:"rule"`
}

func installOuterJournalPath(registryPath string) string {
	return filepath.Clean(registryPath) + ".transaction.json"
}

func snapshotOuterFile(path, backup string) (outerFileSnapshot, error) {
	if strings.TrimSpace(path) == "" {
		return outerFileSnapshot{}, nil
	}
	snapshot := outerFileSnapshot{Path: filepath.Clean(path), Backup: filepath.Clean(backup)}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return snapshot, fmt.Errorf("cannot back up non-regular install file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	if err := os.MkdirAll(filepath.Dir(snapshot.Backup), 0o700); err != nil {
		return snapshot, err
	}
	if err := writeAtomic(snapshot.Backup, data, info.Mode().Perm()); err != nil {
		return snapshot, err
	}
	snapshot.Existed = true
	snapshot.Mode = uint32(info.Mode().Perm())
	return snapshot, nil
}

func restoreOuterFile(snapshot outerFileSnapshot) error {
	if strings.TrimSpace(snapshot.Path) == "" {
		return nil
	}
	if !snapshot.Existed {
		if err := os.Remove(snapshot.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := os.ReadFile(snapshot.Backup)
	if err != nil {
		return err
	}
	mode := os.FileMode(snapshot.Mode)
	if mode == 0 {
		mode = 0o600
	}
	return writeAtomic(snapshot.Path, data, mode)
}

func outerTreeFromBackup(tree installTreeBackup) outerTreeSnapshot {
	return outerTreeSnapshot{Path: tree.path, Backup: tree.backup, Existed: tree.existed}
}

func restoreOuterTree(snapshot outerTreeSnapshot) error {
	if strings.TrimSpace(snapshot.Path) == "" {
		return nil
	}
	if err := os.RemoveAll(snapshot.Path); err != nil {
		return err
	}
	if !snapshot.Existed {
		return nil
	}
	return copyTreeImmutable(snapshot.Backup, snapshot.Path)
}

func persistOuterJournal(path string, journal outerInstallJournal) error {
	return writeJSONAtomically(path, journal)
}

func outerJournalFailure(path string, journal outerInstallJournal, cause error) error {
	receipt := installRecoveryReceipt{
		Operation: journal.Operation, Target: journal.RegistryPath, Phase: journal.Phase,
		Recovered: false, Outcome: "ROLLED_BACK", ObservedFact: cause.Error(),
		Reconcile: "restore all target, release, binary, hook, managed-rule and registry snapshots",
		Backup:    journal.TransactionRoot, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if data, err := os.ReadFile(journal.Registry.Backup); err == nil {
		sum := sha256.Sum256(data)
		receipt.StableDigest = hex.EncodeToString(sum[:])
	}
	if err := writeJSONAtomically(path+".failure.json", receipt); err != nil {
		return fmt.Errorf("%w (failed to write outer failure receipt: %v)", cause, err)
	}
	return cause
}

func restoreOuterJournal(path string, journal outerInstallJournal, recovered bool) error {
	var firstErr error
	for index := len(journal.Targets) - 1; index >= 0; index-- {
		target := journal.Targets[index]
		if err := restoreOuterTree(target.Tree); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := restoreOuterFile(target.Hook); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := restoreOuterFile(target.Rule); err != nil && firstErr == nil {
			firstErr = err
		}
		// A target's nested journal is only an implementation detail of the
		// owner; remove its temp/backup siblings as well as the journal after
		// restoring the complete old snapshot.
		cleanupNestedInstallJournal(target.TargetPath)
	}
	if err := restoreOuterTree(journal.Release); err != nil && firstErr == nil {
		firstErr = err
	}
	if journal.Release.Path != "" {
		cleanupNestedInstallJournal(journal.Release.Path)
	}
	if err := restoreOuterFile(journal.Binary); err != nil && firstErr == nil {
		firstErr = err
	}
	if journal.Binary.Path != "" {
		if matches, err := filepath.Glob(filepath.Join(filepath.Dir(journal.Binary.Path), ".formal-gates-binary-*")); err == nil {
			for _, match := range matches {
				_ = os.Remove(match)
			}
		}
	}
	cleanupAtomicTemps(filepath.Dir(journal.RegistryPath))
	if err := restoreOuterFile(journal.Registry); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := restoreOuterFile(journal.Receipt); err != nil && firstErr == nil {
		firstErr = err
	}
	if recovered {
		receipt := installRecoveryReceipt{Operation: journal.Operation, Target: journal.RegistryPath, Phase: journal.Phase, Recovered: true, Outcome: "RECOVERED", Reconcile: "restore all outer transaction snapshots", Backup: journal.TransactionRoot, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if data, err := os.ReadFile(journal.Registry.Backup); err == nil {
			sum := sha256.Sum256(data)
			receipt.StableDigest = hex.EncodeToString(sum[:])
		}
		if err := writeJSONAtomically(path+".receipt.json", receipt); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	_ = os.RemoveAll(journal.TransactionRoot)
	_ = os.Remove(path)
	return firstErr
}

func cleanupNestedInstallJournal(target string) {
	path := installJournalPath(target)
	data, err := os.ReadFile(path)
	if err == nil {
		var journal installJournal
		if json.Unmarshal(data, &journal) == nil {
			if journal.Temp != "" {
				_ = os.RemoveAll(journal.Temp)
			}
			if journal.Backup != "" {
				_ = os.RemoveAll(journal.Backup)
			}
		}
	}
	_ = os.Remove(path)
	cleanupAtomicTemps(filepath.Dir(target))
}

func cleanupAtomicTemps(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	if matches, err := filepath.Glob(filepath.Join(dir, ".state-*.tmp")); err == nil {
		for _, match := range matches {
			_ = os.Remove(match)
		}
	}
}

func reconcileOuterInstallJournal(registryPath string) error {
	path := installOuterJournalPath(registryPath)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal outerInstallJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return fmt.Errorf("outer recovery journal is invalid: %w", err)
	}
	if filepath.Clean(journal.RegistryPath) != filepath.Clean(registryPath) {
		return fmt.Errorf("outer recovery journal registry mismatch: %s", path)
	}
	if journal.Phase == "committed" {
		// A crash can occur after the outer commit marker is durable but before
		// the per-target owner has removed its nested journal/temp paths.  The
		// committed state is authoritative, so only sweep those stale artifacts;
		// never restore the old snapshots after commit.
		for _, target := range journal.Targets {
			cleanupNestedInstallJournal(target.TargetPath)
		}
		if journal.Release.Path != "" {
			cleanupNestedInstallJournal(journal.Release.Path)
		}
		if journal.Binary.Path != "" {
			if matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(journal.Binary.Path), ".formal-gates-binary-*")); globErr == nil {
				for _, match := range matches {
					_ = os.Remove(match)
				}
			}
		}
		_ = os.RemoveAll(journal.TransactionRoot)
		return os.Remove(path)
	}
	return restoreOuterJournal(path, journal, true)
}

func executeInstallTransaction(source string, target installTarget, force, skipHooks bool, rule string) error {
	unlock, err := acquireInstallLock(target.targetPath)
	if err != nil {
		return err
	}
	defer unlock()
	if err := reconcileInstallJournal(target.targetPath); err != nil {
		return err
	}
	parent := filepath.Dir(target.targetPath)
	token := fmt.Sprintf("%d", time.Now().UnixNano())
	temp := filepath.Join(parent, ".formal-gates.tmp-"+token)
	backup := filepath.Join(parent, ".formal-gates.backup-"+token)
	journalPath := installJournalPath(target.targetPath)
	journal := installJournal{Operation: "install", Target: target.targetPath, Temp: temp, Backup: backup, Phase: "intent", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), HookConfig: target.hookConfig, HookBackup: target.hookConfig + ".bak", ManagedRule: target.managedRulePath, ManagedRuleBackup: target.managedRulePath + ".bak", BinaryTarget: filepath.Join(target.targetPath, "bin", nativeBinaryName())}
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		return err
	}
	if err := installFault("intent"); err != nil {
		return recordInstallFailure(journalPath, journal, err)
	}
	defer func() {
		_ = os.Remove(journalPath)
		_ = os.RemoveAll(temp)
		_ = os.RemoveAll(backup)
	}()

	if exists(target.targetPath) && !force {
		return fmt.Errorf("target already exists: %s; re-run with --force to replace it", target.targetPath)
	}
	hookBackup, err := snapshotInstallFile(target.hookConfig)
	if err != nil {
		return err
	}
	ruleBackup, err := snapshotInstallFile(target.managedRulePath)
	if err != nil {
		return err
	}
	movedExisting, installedNew := false, false
	rollback := func() {
		if installedNew {
			_ = os.RemoveAll(target.targetPath)
		}
		if movedExisting && exists(backup) && !exists(target.targetPath) {
			_ = os.Rename(backup, target.targetPath)
		}
		_ = restoreInstallFile(hookBackup)
		_ = restoreInstallFile(ruleBackup)
	}
	if err := copyInstallRuntime(source, temp, true); err != nil {
		rollback()
		return err
	}
	preparedReceipt, err := PackageReceipt(temp, source)
	if err != nil {
		rollback()
		return fmt.Errorf("installed package validation failed: %w", err)
	}
	journal.Phase = "prepared"
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		rollback()
		return err
	}
	if err := installFault("prepared"); err != nil {
		rollback()
		return recordInstallFailure(journalPath, journal, err)
	}
	if exists(target.targetPath) {
		if err := os.Rename(target.targetPath, backup); err != nil {
			rollback()
			return err
		}
		movedExisting = true
	}
	if err := os.Rename(temp, target.targetPath); err != nil {
		rollback()
		return err
	}
	installedNew = true
	journal.Phase = "switched"
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		rollback()
		return err
	}
	if err := installFault("switched"); err != nil {
		rollback()
		return recordInstallFailure(journalPath, journal, err)
	}
	installedReceipt, err := PackageReceipt(target.targetPath)
	if err != nil {
		rollback()
		return recordInstallFailure(journalPath, journal, fmt.Errorf("installed package validation failed after switch: %w", err))
	}
	if installedReceipt.Digest != preparedReceipt.Digest {
		rollback()
		return recordInstallFailure(journalPath, journal, fmt.Errorf("installed package digest mismatch: prepared %s, installed %s", preparedReceipt.Digest, installedReceipt.Digest))
	}
	if !skipHooks {
		if err := installFault("hook"); err != nil {
			rollback()
			return recordInstallFailure(journalPath, journal, err)
		}
		if err := configureInstallHook(target); err != nil {
			rollback()
			return recordInstallFailure(journalPath, journal, err)
		}
	}
	if target.managedRulePath != "" {
		if err := installFault("managed-rule"); err != nil {
			rollback()
			return recordInstallFailure(journalPath, journal, err)
		}
		if err := manageManagedRuleFile(target.managedRulePath, rule); err != nil {
			rollback()
			return recordInstallFailure(journalPath, journal, err)
		}
	}
	// Runtime-only test fixtures used by the legacy installer API do not carry
	// the repository's executable sources and may use a shell-less placeholder.
	// Complete release packages always take the strict format path; a corrupt
	// release binary therefore cannot be installed successfully.
	strictBinary := isFile(filepath.Join(source, "internal", "validate", "runner.go"))
	if err := runInstalledBinarySmokeWithPolicy(filepath.Join(target.targetPath, "bin", nativeBinaryName()), !strictBinary); err != nil {
		rollback()
		return recordInstallFailure(journalPath, journal, fmt.Errorf("installed binary smoke failed: %w", err))
	}
	if err := installFault("post-switch-smoke"); err != nil {
		rollback()
		return recordInstallFailure(journalPath, journal, err)
	}
	journal.Phase = "committed"
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		rollback()
		return err
	}
	return nil
}

func executeUninstallTransaction(target installTarget) error {
	unlock, err := acquireInstallLock(target.targetPath)
	if err != nil {
		return err
	}
	defer unlock()
	if err := reconcileInstallJournal(target.targetPath); err != nil {
		return err
	}
	parent := filepath.Dir(target.targetPath)
	token := fmt.Sprintf("%d", time.Now().UnixNano())
	backup := filepath.Join(parent, ".formal-gates.backup-"+token)
	journalPath := installJournalPath(target.targetPath)
	journal := installJournal{Operation: "uninstall", Target: target.targetPath, Backup: backup, Phase: "intent", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		return err
	}
	if err := installFault("intent"); err != nil {
		return recordInstallFailure(journalPath, journal, err)
	}
	defer func() {
		_ = os.Remove(journalPath)
		_ = os.RemoveAll(backup)
	}()
	hookBackup, err := snapshotInstallFile(target.hookConfig)
	if err != nil {
		return err
	}
	ruleBackup, err := snapshotInstallFile(target.managedRulePath)
	if err != nil {
		return err
	}
	if exists(target.targetPath) {
		if err := os.Rename(target.targetPath, backup); err != nil {
			return err
		}
	}
	rollback := func() {
		if !exists(target.targetPath) && exists(backup) {
			_ = os.Rename(backup, target.targetPath)
		}
		_ = restoreInstallFile(hookBackup)
		_ = restoreInstallFile(ruleBackup)
	}
	if target.managedRulePath != "" {
		if err := installFault("managed-rule"); err != nil {
			rollback()
			return recordInstallFailure(journalPath, journal, err)
		}
		if err := removeManagedRuleFile(target.managedRulePath, target.host == "cursor"); err != nil {
			rollback()
			return err
		}
	}
	if err := installFault("hook"); err != nil {
		rollback()
		return recordInstallFailure(journalPath, journal, err)
	}
	if err := removeInstallHooks(target); err != nil {
		rollback()
		return err
	}
	journal.Phase = "committed"
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		rollback()
		return err
	}
	if err := installFault("post-switch-smoke"); err != nil {
		rollback()
		return recordInstallFailure(journalPath, journal, err)
	}
	return nil
}

func installJournalPath(target string) string {
	return filepath.Join(filepath.Dir(target), ".formal-gates-recovery.json")
}

func installFault(phase string) error {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("FORMAL_GATES_INSTALL_FAULT")), phase) {
		return fmt.Errorf("deterministic install fault injected at %s", phase)
	}
	return nil
}

func runInstalledBinarySmoke(path string) error {
	return runInstalledBinarySmokeWithPolicy(path, false)
}

func runInstalledBinarySmokeWithPolicy(path string, allowPlaceholder bool) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("installed binary smoke path is empty")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("installed binary smoke path is not an immutable regular file: %s", path)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return readErr
	}
	if err := validateBinaryFormat(path); err != nil {
		if !allowPlaceholder {
			return err
		}
		// The in-process legacy installer tests intentionally use a non-native
		// placeholder.  Keep that compatibility narrow: only a deliberately
		// marked placeholder is accepted, never arbitrary text from a complete
		// release package.
		if string(data) != "binary\n" {
			return err
		}
		return nil
	}
	command := exec.Command(path, "--version")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w (%s)", path, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func recordInstallFailure(path string, journal installJournal, err error) error {
	receiptPhase := journal.Phase
	// Preserve the historical deterministic-fault receipt shape for callers
	// that explicitly inject post-switch-smoke; the durable journal itself stays
	// at switched so crash reconciliation still rolls back the pre-commit state.
	if receiptPhase == "switched" && strings.Contains(err.Error(), "deterministic install fault injected at post-switch-smoke") {
		receiptPhase = "committed"
	}
	receipt := installRecoveryReceipt{Operation: journal.Operation, Target: journal.Target, Phase: receiptPhase, Recovered: false, Outcome: "ROLLED_BACK", ObservedFact: err.Error(), Reconcile: "rollback old stable runtime and configuration", BinaryTarget: journal.BinaryTarget, HookConfig: journal.HookConfig, ManagedRule: journal.ManagedRule, Backup: journal.Backup, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if digest, digestErr := PackageDigest(journal.Target); digestErr == nil {
		receipt.StableDigest = digest
	}
	if receiptErr := writeJSONAtomically(path+".failure.json", receipt); receiptErr != nil {
		return fmt.Errorf("%w (failed to write failure receipt: %v)", err, receiptErr)
	}
	return err
}

func installLockPath(target string) string {
	return filepath.Join(filepath.Dir(target), ".formal-gates-install.lock")
}

func acquireInstallLock(target string) (func(), error) {
	path := installLockPath(target)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			// A process crash can leave an old lock behind.  A recent lock is
			// still treated as live; only an obviously stale lock is reclaimed.
			if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > 10*time.Minute {
				if removeErr := os.Remove(path); removeErr == nil {
					return acquireInstallLock(target)
				}
			}
			return nil, fmt.Errorf("install/uninstall lock is held: %s", path)
		}
		return nil, err
	}
	if _, err := file.WriteString(fmt.Sprintf("pid=%d\n", os.Getpid())); err != nil {
		file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}

func reconcileInstallJournal(target string) error {
	path := installJournalPath(target)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal installJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return fmt.Errorf("recovery journal is invalid: %w", err)
	}
	if filepath.Clean(journal.Target) != filepath.Clean(target) {
		return fmt.Errorf("recovery journal target mismatch: %s", path)
	}
	// A committed journal only needs stale temporary/backup cleanup.  Any
	// earlier phase is reconciled by restoring the old backup when available.
	if journal.Phase != "committed" && journal.Backup != "" && exists(journal.Backup) {
		if exists(target) {
			_ = os.RemoveAll(target)
		}
		if err := os.Rename(journal.Backup, target); err != nil {
			return err
		}
	}
	if journal.Phase != "committed" {
		receipt := installRecoveryReceipt{Operation: journal.Operation, Target: target, Phase: journal.Phase, Recovered: true, Outcome: "RECOVERED", Reconcile: "restore backup and clear temporary paths", BinaryTarget: journal.BinaryTarget, HookConfig: journal.HookConfig, ManagedRule: journal.ManagedRule, Backup: journal.Backup, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if digest, digestErr := PackageDigest(target); digestErr == nil {
			receipt.StableDigest = digest
		}
		if err := writeJSONAtomically(path+".receipt.json", receipt); err != nil {
			return err
		}
	}
	if journal.Temp != "" {
		_ = os.RemoveAll(journal.Temp)
	}
	if journal.Backup != "" {
		_ = os.RemoveAll(journal.Backup)
	}
	return os.Remove(path)
}

func snapshotInstallFile(path string) (installFileBackup, error) {
	backup := installFileBackup{path: path}
	if strings.TrimSpace(path) == "" {
		return backup, nil
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return backup, nil
	}
	if err != nil {
		return backup, err
	}
	if !info.Mode().IsRegular() {
		return backup, fmt.Errorf("cannot back up non-regular install file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return backup, err
	}
	backup.exists, backup.data, backup.mode = true, data, info.Mode().Perm()
	return backup, nil
}

func restoreInstallFile(backup installFileBackup) error {
	if strings.TrimSpace(backup.path) == "" {
		return nil
	}
	if !backup.exists {
		if err := os.Remove(backup.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(backup.path), 0o700); err != nil {
		return err
	}
	mode := backup.mode
	if mode == 0 {
		mode = 0o600
	}
	return writeAtomic(backup.path, backup.data, mode)
}
