package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EngineAdmission is the immutable runtime identity selected by the stable
// install registry.  The candidate workflow surface accepts this identity
// only as a launcher-derived value; callers do not choose a runtime, target
// identity, or package digest themselves.
type EngineAdmission struct {
	Host          string
	RegistryPath  string
	RecordID      string
	Target        string
	PackageDigest string
	Generation    uint64
	Lease         string
	Token         string
}

// ResolveEngineAdmission reads the existing install registry and resolves the
// installed package that owns a candidate engine run.  It is deliberately
// read-only: candidates cannot bootstrap, repair, or write the stable
// registry.  Any missing, inactive, conflicting, or unverifiable record is a
// stable UNREGISTERED_INSTALL rejection before the engine state directory is
// created.
func ResolveEngineAdmission(root, packageRoot string) (EngineAdmission, error) {
	return resolveEngineAdmission(root, packageRoot, "")
}

// ResolveEngineAdmissionByRecord resolves the immutable launcher admission
// named by a previously-created engine request.  A run stores the registry
// record id as its installed-target identity, so later read/write commands can
// select the project binding even when one stable launcher owns several
// global/project records.  The caller may perform the root-specific binding
// check separately after the run envelope has passed its version barrier.
func ResolveEngineAdmissionByRecord(recordID string) (EngineAdmission, error) {
	return resolveEngineAdmission("", "", strings.TrimSpace(recordID))
}

func resolveEngineAdmission(root, packageRoot, requestedRecordID string) (EngineAdmission, error) {
	root = strings.TrimSpace(root)
	packageRoot = strings.TrimSpace(packageRoot)
	requestedRecordID = strings.TrimSpace(requestedRecordID)
	autoResolve := packageRoot == ""
	var packageAbs string
	if packageRoot != "" {
		var err error
		packageAbs, err = filepath.Abs(packageRoot)
		if err != nil {
			return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: resolve candidate package root: %w", err)
		}
	}
	registryPath := installRegistryPath(InstallOptions{})
	if strings.TrimSpace(registryPath) == "" {
		return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: stable launcher registry path is unavailable")
	}
	doc, err := LoadRegistry(registryPath)
	if err != nil {
		return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: stable launcher registry cannot be read: %w", err)
	}
	want := ""
	if !autoResolve {
		want = canonicalRegistryPath(packageAbs)
	}
	executable, _ := os.Executable()
	launcher := canonicalRegistryPath(executable)
	type candidate struct {
		record    RegistryRecord
		exactRoot bool
	}
	candidates := []candidate{}
	for _, record := range doc.Records {
		if !strings.EqualFold(record.Status, "active") || !validAdmissionRegistryRecord(record) {
			continue
		}
		if requestedRecordID != "" && record.ID != requestedRecordID {
			continue
		}
		target := canonicalRegistryPath(record.Target)
		if target == "" {
			target = canonicalRegistryPath(record.CanonicalPaths["target"])
		}
		if !autoResolve && target != want {
			continue
		}
		if (autoResolve || requestedRecordID != "") && !isTestBinary() && (launcher == "" || canonicalRegistryPath(record.LauncherPath) != launcher) {
			continue
		}
		exactRoot, applicable := admissionRecordRootMatch(record, root)
		if !applicable {
			continue
		}
		candidates = append(candidates, candidate{record: record, exactRoot: exactRoot})
	}
	if len(candidates) == 0 {
		return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: no active registry record matches candidate package root %s", packageAbs)
	}
	// A project-local sibling is the authoritative binding for that project.
	// The canonical global record remains a fallback for a first invocation that
	// has not materialized its sibling yet.  This is what lets one stable
	// launcher serve multiple projects without treating their records as a
	// conflict.
	if root != "" {
		hasExact := false
		for _, item := range candidates {
			if item.exactRoot {
				hasExact = true
				break
			}
		}
		if hasExact {
			filtered := candidates[:0]
			for _, item := range candidates {
				if item.exactRoot {
					filtered = append(filtered, item)
				}
			}
			candidates = filtered
		}
	}
	// Multiple records are only a conflict when they describe different
	// installed identities.  Distinct project bindings of the same installed
	// target/package are expected and must not block normal use.
	identityKeys := map[string]bool{}
	for _, item := range candidates {
		record := item.record
		key := strings.Join([]string{canonicalRegistryPath(record.Target), canonicalRegistryPath(record.LauncherPath), record.VCSIdentity, record.PackageDigest, record.InstalledDigest}, "\x00")
		identityKeys[key] = true
	}
	if len(identityKeys) > 1 {
		return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: candidate admission has conflicting active registry records")
	}
	// The record order is the registry's canonical order.  When equivalent
	// duplicate rows exist, select the first one deterministically; their
	// identity is the same and therefore they are not a real conflict.
	record := candidates[0].record
	if autoResolve {
		packageAbs = record.Target
	}
	if identityErr := verifyRegisteredTargetIdentity(record); identityErr != nil {
		return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: candidate target identity cannot be verified: %w", identityErr)
	}
	if root != "" {
		if bindingErr := verifyRegistryBindingRecord(record, root, packageAbs); bindingErr != nil {
			return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: candidate admission binding cannot be verified: %w", bindingErr)
		}
	}
	packageDigest := strings.TrimSpace(record.PackageDigest)
	if packageDigest == "" {
		packageDigest = strings.TrimSpace(record.InstalledDigest)
	}
	if packageDigest == "" {
		return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: registry record %q has no package digest", record.ID)
	}
	if err := verifyEngineBootstrapReceipt(registryPath, record, packageDigest); err != nil {
		return EngineAdmission{}, err
	}
	return EngineAdmission{Host: record.Host, RegistryPath: filepath.Clean(registryPath), RecordID: record.ID, Target: filepath.Clean(record.Target), PackageDigest: packageDigest, Generation: record.Generation, Lease: record.Lease, Token: record.Token}, nil
}

// admissionRecordRootMatch reports whether a record can serve root.  Global
// installs use the home-level record for first invocation and derive a
// project-local sibling thereafter; project installs are bound to their
// registered project root.  Returning exactRoot lets the resolver prefer a
// sibling over the canonical global fallback when both are present.
func admissionRecordRootMatch(record RegistryRecord, root string) (exactRoot, applicable bool) {
	if strings.TrimSpace(root) == "" {
		return false, true
	}
	canonicalRoot, err := filepath.Abs(root)
	if err != nil {
		return false, false
	}
	canonicalRoot = canonicalRegistryPath(canonicalRoot)
	projectRoot := canonicalRegistryPath(record.ProjectRoot)
	if record.Scope == "project" {
		return projectRoot == canonicalRoot, projectRoot == canonicalRoot
	}
	if projectRoot == canonicalRoot {
		return true, true
	}
	home, err := installHomeDir()
	if err == nil && projectRoot == canonicalRegistryPath(home) {
		return false, true
	}
	return false, false
}

func verifyEngineBootstrapReceipt(registryPath string, record RegistryRecord, packageDigest string) error {
	data, err := os.ReadFile(registryPath + ".bootstrap.json")
	if err != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: registry bootstrap receipt is unavailable: %w", err)
	}
	var receipt BootstrapReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: registry bootstrap receipt is invalid: %w", err)
	}
	if !receipt.Accepted || receipt.StateCreated {
		return fmt.Errorf("UNREGISTERED_INSTALL: registry bootstrap receipt is not an accepted pre-state receipt")
	}
	if receipt.PackageDigest != packageDigest {
		return fmt.Errorf("UNREGISTERED_INSTALL: registry bootstrap package digest does not match admitted target")
	}
	for _, admitted := range receipt.Records {
		if admitted.ID == record.ID && strings.EqualFold(admitted.Status, "active") && canonicalRegistryPath(admitted.Target) == canonicalRegistryPath(record.Target) {
			return nil
		}
		// A global invocation sibling is created after the canonical global
		// bootstrap receipt. It shares the installed target and launcher, so the
		// canonical receipt is also its immutable bootstrap proof.
		if record.Scope == "global" && strings.EqualFold(admitted.Scope, "global") &&
			canonicalRegistryPath(admitted.Target) == canonicalRegistryPath(record.Target) &&
			canonicalRegistryPath(admitted.LauncherPath) == canonicalRegistryPath(record.LauncherPath) &&
			admitted.PackageDigest == packageDigest {
			return nil
		}
	}
	return fmt.Errorf("UNREGISTERED_INSTALL: registry bootstrap receipt does not contain admitted target %q", record.ID)
}

// verifyRegistryBindingRecord 校验 record 与 workflow root/package root 及启
// 动器身份的绑定关系。不含 registry 加载与安装目标摘要核验：二者由调用方
// 在上游完成，避免同一验证链内重复读盘与重复全树哈希。
func verifyRegistryBindingRecord(record RegistryRecord, root, packageRoot string) error {
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
	// a different executable identity and is therefore rejected.  In-process test fixtures
	// may omit a launcher; shipped commands always require the fixed launcher.
	executable, executableErr := os.Executable()
	if executableErr != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: cannot resolve invoking launcher: %w", executableErr)
	}
	base := filepath.Base(filepath.Clean(executable))
	if !strings.HasSuffix(base, ".test") && !strings.HasSuffix(base, ".test.exe") && !launcherInvocationMatches(record.LauncherPath) {
		return fmt.Errorf("UNREGISTERED_INSTALL: workflow must be driven by stable launcher %s", record.LauncherPath)
	}
	return nil
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
