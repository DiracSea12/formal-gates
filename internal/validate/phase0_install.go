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

// stagedInstallTree is the install coordinator's private candidate. Preparing
// every tree before the first switch lets one journal own the whole multi-host
// operation.
type stagedInstallTree struct {
	Source      string
	Destination string
	Temp        string
	Prepared    PackageReceiptReport
	RuntimeOnly bool
}

func newStagedInstallTree(source, destination string, runtimeOnly bool) stagedInstallTree {
	source = canonicalRegistryPath(source)
	destination = canonicalRegistryPath(destination)
	return stagedInstallTree{Source: source, Destination: destination, Temp: filepath.Join(filepath.Dir(destination), fmt.Sprintf(".formal-gates-stage-%d", time.Now().UnixNano())), RuntimeOnly: runtimeOnly}
}

func prepareInstallTree(candidate stagedInstallTree) (stagedInstallTree, error) {
	if err := os.MkdirAll(filepath.Dir(candidate.Destination), 0o700); err != nil {
		return stagedInstallTree{}, err
	}
	if candidate.RuntimeOnly {
		if err := copyInstallRuntime(candidate.Source, candidate.Temp, true); err != nil {
			return stagedInstallTree{}, err
		}
	} else if err := copyTreeImmutable(candidate.Source, candidate.Temp); err != nil {
		return stagedInstallTree{}, err
	}
	receipt, err := PackageReceipt(candidate.Temp, candidate.Source)
	if err != nil {
		_ = os.RemoveAll(candidate.Temp)
		return stagedInstallTree{}, fmt.Errorf("prepared install package validation failed: %w", err)
	}
	candidate.Prepared = receipt
	return candidate, nil
}

func switchPreparedInstallTree(candidate *stagedInstallTree, force bool) error {
	if exists(candidate.Destination) {
		if !force {
			return fmt.Errorf("target already exists: %s; re-run with --force to replace it", candidate.Destination)
		}
		if err := os.RemoveAll(candidate.Destination); err != nil {
			return err
		}
	}
	if err := os.Rename(candidate.Temp, candidate.Destination); err != nil {
		return err
	}
	candidate.Temp = ""
	return nil
}

func verifySwitchedInstallTree(candidate stagedInstallTree, allowPlaceholder bool) error {
	installed, err := PackageReceipt(candidate.Destination, candidate.Source)
	if err != nil {
		return fmt.Errorf("switched install package validation failed: %w", err)
	}
	if installed.Digest != candidate.Prepared.Digest {
		return fmt.Errorf("switched install package digest mismatch: prepared %s, installed %s", candidate.Prepared.Digest, installed.Digest)
	}
	binary := filepath.Join(candidate.Destination, "bin", nativeBinaryName())
	if err := runInstalledBinarySmokeWithPolicy(binary, allowPlaceholder); err != nil {
		return fmt.Errorf("installed binary smoke failed: %w", err)
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
		if rel == ".git" || rel == ".gates" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == "__pycache__" {
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

type installRecoveryReceipt struct {
	Operation    string `json:"operation"`
	Target       string `json:"target"`
	Phase        string `json:"interruptedPhase"`
	Recovered    bool   `json:"recovered"`
	Outcome      string `json:"outcome,omitempty"`
	ObservedFact string `json:"observedFact,omitempty"`
	Reconcile    string `json:"reconcileAction,omitempty"`
	StableDigest string `json:"stableDigest,omitempty"`
	Backup       string `json:"backup,omitempty"`
	ObservedAt   string `json:"observedAt"`
}

// outerInstallJournal is the only durable install/uninstall undo record.  It
// records every old byte/tree before the first mutation and remains present
// until runtime, launcher, configuration, and registry commit are all durable.
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
	Staged          []string              `json:"staged,omitempty"`
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
		Recovered: true, Outcome: "ROLLED_BACK", ObservedFact: cause.Error(),
		Reconcile: "restore all target, release, binary, hook, managed-rule and registry snapshots",
		Backup:    journal.TransactionRoot, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if data, err := os.ReadFile(journal.RegistryPath); err == nil {
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
	}
	if err := restoreOuterTree(journal.Release); err != nil && firstErr == nil {
		firstErr = err
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
	for _, path := range journal.Staged {
		_ = os.RemoveAll(path)
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
		// The committed state is authoritative; only sweep transaction-owned
		// staging/atomic temporary paths, never restore old snapshots.
		for _, staged := range journal.Staged {
			_ = os.RemoveAll(staged)
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

func installFault(phase string) error {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("FORMAL_GATES_INSTALL_FAULT")), phase) {
		return fmt.Errorf("deterministic install fault injected at %s", phase)
	}
	return nil
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
		// Small in-process install fixtures use this exact marker instead of a
		// cross-platform binary. Complete packages always select strict mode.
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
