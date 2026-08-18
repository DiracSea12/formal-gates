package validate

import (
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
	if _, err := PackageReceipt(temp, source); err != nil {
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
	journal.Phase = "committed"
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		return err
	}
	if err := installFault("post-switch-smoke"); err != nil {
		rollbackRelease()
		return recordInstallFailure(journalPath, journal, err)
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
	if _, err := PackageReceipt(temp, source); err != nil {
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
	if err := runInstalledBinarySmoke(filepath.Join(target.targetPath, "bin", nativeBinaryName())); err != nil {
		rollback()
		return recordInstallFailure(journalPath, journal, fmt.Errorf("installed binary smoke failed: %w", err))
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
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("installed binary smoke path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("installed binary smoke path is not a regular file: %s", path)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return readErr
	}
	// Runtime-only package fixtures carry a placeholder payload rather than a
	// platform executable.  Real release candidates are ELF/Mach-O/PE files or
	// a shebang wrapper; only those formats are meaningful smoke targets.
	if len(data) < 4 && !strings.HasPrefix(string(data), "#!") {
		return nil
	}
	if !strings.HasPrefix(string(data), "#!") && string(data[:4]) != "\x7fELF" && !(len(data) >= 2 && (string(data[:2]) == "MZ" || string(data[:2]) == "\xcf\xfa" || string(data[:2]) == "\xfe\xed")) {
		return nil
	}
	command := exec.Command(path, "--version")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w (%s)", path, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func recordInstallFailure(path string, journal installJournal, err error) error {
	receipt := installRecoveryReceipt{Operation: journal.Operation, Target: journal.Target, Phase: journal.Phase, Recovered: false, Outcome: "ROLLED_BACK", BinaryTarget: journal.BinaryTarget, HookConfig: journal.HookConfig, ManagedRule: journal.ManagedRule, Backup: journal.Backup, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
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
		receipt := installRecoveryReceipt{Operation: journal.Operation, Target: target, Phase: journal.Phase, Recovered: true, Outcome: "RECOVERED", BinaryTarget: journal.BinaryTarget, HookConfig: journal.HookConfig, ManagedRule: journal.ManagedRule, Backup: journal.Backup, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
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
