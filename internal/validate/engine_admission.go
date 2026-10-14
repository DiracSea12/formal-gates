package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/host"
)

// EngineAdmission is the immutable runtime identity selected by the stable
// install registry.  The candidate workflow surface accepts this identity
// only as a launcher-derived value; callers do not choose a runtime, target
// identity, or package digest themselves.
type EngineAdmission struct {
	RegistryPath  string
	RecordID      string
	Target        string
	Provider      string // Canonical host ID from the selected registry record.
	PackageDigest string
	Generation    uint64
	Lease         string
	Token         string
	RuntimeFiles  vcs.RuntimeFiles
}

var errNoEngineAdmission = errors.New("no active registered runtime")

// ProjectRuntimeFiles uses the same verified installation authority for
// read-only project discovery. A project without a registration remains usable.
func ProjectRuntimeFiles(root string) (vcs.RuntimeFiles, error) {
	registry := installRegistryPath(InstallOptions{})
	if _, err := os.Stat(registry); os.IsNotExist(err) {
		return vcs.RuntimeFiles{}, nil
	} else if err != nil {
		return vcs.RuntimeFiles{}, err
	}
	doc, err := LoadRegistry(registry)
	if err != nil {
		return vcs.RuntimeFiles{}, err
	}
	files := vcs.RuntimeFiles{}
	for _, record := range doc.Records {
		if record.Scope == "global" && strings.Contains(record.ID, "-project-") {
			continue
		}
		if !strings.EqualFold(record.Status, "active") || !validRegistryRecord(record) || !launcherInvocationMatches(record.LauncherPath) {
			continue
		}
		if _, applicable := admissionRecordRootMatch(record, root, true); !applicable {
			continue
		}
		installed, err := verifyRegisteredTargetIdentity(record)
		if err != nil {
			return vcs.RuntimeFiles{}, err
		}
		runtimeFiles := registeredRuntimeFiles(record, installed)
		files.Files = append(files.Files, runtimeFiles.Files...)
		files.EmptyDirectories = append(files.EmptyDirectories, runtimeFiles.EmptyDirectories...)
	}
	return files, nil
}

// ResolveEngineAdmission reads the existing install registry and resolves the
// installed package that owns a candidate engine run.  It is deliberately
// read-only: candidates cannot bootstrap, repair, or write the stable
// registry.  Any missing, inactive, conflicting, or unverifiable record is a
// stable UNREGISTERED_INSTALL rejection before the engine state directory is
// created.
func ResolveEngineAdmission(root, packageRoot string, registryPath ...string) (EngineAdmission, error) {
	return resolveEngineAdmission(root, packageRoot, "", registryPath...)
}

// ResolveEngineAdmissionByRecord resolves the immutable launcher admission
// named by a previously-created engine request.  A run stores the registry
// record id as its installed-target identity, so later read/write commands can
// select the project binding even when one stable launcher owns several
// global/project records.  The caller may perform the root-specific binding
// check separately after the run envelope has passed its version barrier.
func ResolveEngineAdmissionByRecord(recordID string, registryPath ...string) (EngineAdmission, error) {
	return resolveEngineAdmission("", "", strings.TrimSpace(recordID), registryPath...)
}

func resolveEngineAdmission(root, packageRoot, requestedRecordID string, registryNamespace ...string) (EngineAdmission, error) {
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
	registryPath := ""
	if len(registryNamespace) != 0 {
		registryPath = strings.TrimSpace(registryNamespace[0])
	} else {
		registryPath = installRegistryPath(InstallOptions{})
	}
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
	type candidate struct {
		record    RegistryRecord
		exactRoot bool
	}
	candidates := []candidate{}
	for _, record := range doc.Records {
		if record.Scope == "global" && strings.Contains(record.ID, "-project-") {
			continue
		}
		if !strings.EqualFold(record.Status, "active") || !validAdmissionRegistryRecord(record) {
			continue
		}
		if requestedRecordID != "" && record.ID != requestedRecordID {
			continue
		}
		target := canonicalRegistryPath(record.Target)
		if !autoResolve && target != want {
			continue
		}
		if (autoResolve || requestedRecordID != "") && !launcherInvocationMatches(record.LauncherPath) {
			continue
		}
		exactRoot, applicable := admissionRecordRootMatch(record, root, len(registryNamespace) != 0)
		if !applicable {
			continue
		}
		candidates = append(candidates, candidate{record: record, exactRoot: exactRoot})
	}
	if len(candidates) == 0 {
		return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: no active registry record matches candidate package root %s: %w", packageAbs, errNoEngineAdmission)
	}
	// An exact project installation takes precedence over a global installation.
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
	installed, identityErr := verifyRegisteredTargetIdentity(record)
	if identityErr != nil {
		return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: candidate target identity cannot be verified: %w", identityErr)
	}
	if root != "" {
		if bindingErr := verifyRegistryBindingRecord(record, root, packageAbs); bindingErr != nil {
			return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: candidate admission binding cannot be verified: %w", bindingErr)
		}
	}
	packageDigest := strings.TrimSpace(record.PackageDigest)
	if err := verifyEngineBootstrapReceipt(registryPath, record, packageDigest); err != nil {
		return EngineAdmission{}, err
	}
	descriptor, err := host.Lookup(record.Host)
	if err != nil {
		return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: registry host cannot be resolved: %w", err)
	}
	runtimeFiles := registeredRuntimeFiles(record, installed)
	return EngineAdmission{RegistryPath: filepath.Clean(registryPath), RecordID: record.ID, Target: filepath.Clean(record.Target), Provider: descriptor.ID, PackageDigest: packageDigest, Generation: record.Generation, Lease: record.Lease, Token: record.Token, RuntimeFiles: runtimeFiles}, nil
}

func registeredRuntimeFiles(record RegistryRecord, installed PackageReceiptReport) vcs.RuntimeFiles {
	runtimeFiles := vcs.RuntimeFiles{}
	for _, entry := range installed.Entries {
		runtimeFiles.Files = append(runtimeFiles.Files, entry.RealPath)
	}
	if len(installed.Entries) != 0 {
		runtimeFiles.Files = append(runtimeFiles.Files, canonicalRegistryPath(record.LauncherPath))
	}
	if record.ResourceRoot != "" {
		runtimeFiles.EmptyDirectories = []string{canonicalRegistryPath(record.ResourceRoot)}
	}
	return runtimeFiles
}

// Project installations bind their exact root; canonical global installations
// serve projects whose original engine request carries the root.
func admissionRecordRootMatch(record RegistryRecord, root string, explicitNamespace bool) (exactRoot, applicable bool) {
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
	if explicitNamespace {
		// Global records serve multiple projects. The run's own request binds
		// its project root; explicit namespaces never consult ambient HOME.
		return false, true
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
	for _, admitted := range receipt.Records {
		if admitted.ID == record.ID && strings.EqualFold(admitted.Status, "active") && canonicalRegistryPath(admitted.Target) == canonicalRegistryPath(record.Target) {
			if admitted.PackageDigest != packageDigest {
				return fmt.Errorf("UNREGISTERED_INSTALL: registry bootstrap package digest does not match admitted target")
			}
			return nil
		}

	}
	return fmt.Errorf("UNREGISTERED_INSTALL: registry bootstrap receipt does not contain admitted target %q", record.ID)
}
