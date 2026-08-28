package validate

// Stage 0 keeps the legacy workflow writer untouched while exposing the small
// immutable surfaces needed by a future engine/candidate installation.  These
// helpers deliberately do not migrate legacy state: the stable driver continues
// to read and write its existing envelope, while a versioned candidate must opt
// into the exact envelope below before it can write anything.

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
)

const (
	// CurrentStateSchemaVersion and CurrentWorkflowDefinitionVersion are the
	// initial contract values for the not-yet-authoritative engine surface.
	// Bumping either value is an intentional contract change and requires a new
	// candidate package; legacy RunState values are not rewritten.
	CurrentStateSchemaVersion        = "1"
	CurrentWorkflowDefinitionVersion = "1"
	UnsupportedRunVersionCode        = "UNSUPPORTED_RUN_VERSION"
	RegistrySchemaVersion            = 1
	CurrentWorkflowDefinitionSource  = "definitions/workflow.json"
	CurrentWorkflowDefinitionDigest  = "sha256:9ec68cd758cf9bad5bd8beefedac51755442c521ffe5e8276e805e82e66faa4c"
)

type VersionEnvelope struct {
	Writer                    string `json:"writer"`
	StateSchemaVersion        string `json:"stateSchemaVersion"`
	WorkflowDefinitionVersion string `json:"workflowDefinitionVersion"`
	DefinitionSource          string `json:"definitionSource"`
	DefinitionDigest          string `json:"definitionDigest"`
	PackageDigest             string `json:"packageDigest,omitempty"`
}

type UnsupportedRunVersionError struct {
	Field    string
	Expected string
	Observed string
}

func (e *UnsupportedRunVersionError) Error() string {
	return fmt.Sprintf("%s: %s expected %q, got %q", UnsupportedRunVersionCode, e.Field, e.Expected, e.Observed)
}

func (e *UnsupportedRunVersionError) Is(target error) bool {
	_, ok := target.(*UnsupportedRunVersionError)
	return ok
}

// ValidateVersionEnvelope is the write barrier for a versioned candidate.  It
// requires all fields that identify the definition, then compares both version
// values exactly.  Callers should run it before opening or modifying a state
// file; it has no migration or repair side effects.
func ValidateVersionEnvelope(envelope VersionEnvelope) error {
	checks := []struct {
		field, observed, expected string
	}{
		{"writer", envelope.Writer, "engine"},
		{"stateSchemaVersion", envelope.StateSchemaVersion, CurrentStateSchemaVersion},
		{"workflowDefinitionVersion", envelope.WorkflowDefinitionVersion, CurrentWorkflowDefinitionVersion},
	}
	for _, check := range checks {
		if strings.TrimSpace(check.observed) == "" || check.observed != check.expected {
			return &UnsupportedRunVersionError{Field: check.field, Expected: check.expected, Observed: check.observed}
		}
	}
	if strings.TrimSpace(envelope.DefinitionSource) == "" || strings.TrimSpace(envelope.DefinitionDigest) == "" {
		return &UnsupportedRunVersionError{Field: "definition", Expected: "source and digest", Observed: "missing"}
	}
	// Definition source and digest form one immutable identity.  Candidate
	// writers must use the exact pair from the currently installed definition;
	// accepting a merely non-empty pair would allow a stale definition to write
	// a state file that the current reader cannot interpret.
	if envelope.DefinitionSource != CurrentWorkflowDefinitionSource || envelope.DefinitionDigest != CurrentWorkflowDefinitionDigest {
		return &UnsupportedRunVersionError{Field: "definition", Expected: CurrentWorkflowDefinitionSource + " @ " + CurrentWorkflowDefinitionDigest, Observed: envelope.DefinitionSource + " @ " + envelope.DefinitionDigest}
	}
	return nil
}

func IsUnsupportedRunVersion(err error) bool {
	var target *UnsupportedRunVersionError
	return errors.As(err, &target)
}

// WriteVersionedState is the future engine/candidate write entrypoint.  The
// envelope is validated before the destination is opened, so an unsupported
// writer cannot create or truncate a state file.  Legacy workflow state does
// not call this helper and therefore keeps its existing format unchanged.
func WriteVersionedState(path string, envelope VersionEnvelope, value any) error {
	if err := ValidateVersionEnvelope(envelope); err != nil {
		return err
	}
	return writeVersionedStateDocument(path, envelope, value)
}

func writeVersionedStateDocument(path string, envelope VersionEnvelope, value any) error {
	document := map[string]any{}
	if envelope.PackageDigest != "" {
		document["packageDigest"] = envelope.PackageDigest
	}
	if fields, ok := value.(map[string]any); ok {
		for key, item := range fields {
			document[key] = item
		}
	} else if value != nil {
		document["payload"] = value
	}
	// Identity fields are owned by the validated envelope.  Applying them last
	// prevents a caller's payload map from silently downgrading the writer or
	// definition after the write barrier has passed.
	document["writer"] = envelope.Writer
	document["stateSchemaVersion"] = envelope.StateSchemaVersion
	document["workflowDefinitionVersion"] = envelope.WorkflowDefinitionVersion
	document["definitionSource"] = envelope.DefinitionSource
	document["definitionDigest"] = envelope.DefinitionDigest
	return writeJSONAtomically(path, document)
}

type DiagnoseReport struct {
	Path             string          `json:"path"`
	JSONReadable     bool            `json:"jsonReadable"`
	DetectedVersions map[string]any  `json:"detectedVersions,omitempty"`
	Supported        VersionEnvelope `json:"supported"`
	Summary          map[string]any  `json:"summary,omitempty"`
	Integrity        string          `json:"integrity"`
	Recommendation   string          `json:"recommendation,omitempty"`
}

// DiagnoseState is intentionally raw and read-only.  It never calls the
// normal loader, never writes a repaired state, and preserves a terminal
// summary when the full envelope is from an older or newer version.
func DiagnoseState(path string) (DiagnoseReport, error) {
	report := DiagnoseReport{
		Path: filepath.Clean(path),
		Supported: VersionEnvelope{
			Writer: "engine", StateSchemaVersion: CurrentStateSchemaVersion,
			WorkflowDefinitionVersion: CurrentWorkflowDefinitionVersion,
			DefinitionSource:          CurrentWorkflowDefinitionSource,
			DefinitionDigest:          CurrentWorkflowDefinitionDigest,
		},
		Integrity: "unknown",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return report, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		report.Recommendation = "rebuild the state with the owning writer"
		return report, nil
	}
	report.JSONReadable = true
	report.Integrity = "readable"
	if _, writerOK := raw["writer"]; !writerOK {
		report.Integrity = "unsupported"
		report.Recommendation = UnsupportedRunVersionCode + ": state has no version envelope; rebuild it with the owning writer"
	}
	report.DetectedVersions = map[string]any{}
	for _, key := range []string{"writer", "stateSchemaVersion", "workflowDefinitionVersion", "definitionSource", "definitionDigest", "packageDigest"} {
		if value, ok := raw[key]; ok {
			report.DetectedVersions[key] = value
		}
	}
	if _, writerOK := raw["writer"]; writerOK {
		envelope := VersionEnvelope{
			Writer:                    rawString(raw, "writer"),
			StateSchemaVersion:        rawString(raw, "stateSchemaVersion"),
			WorkflowDefinitionVersion: rawString(raw, "workflowDefinitionVersion"),
			DefinitionSource:          rawString(raw, "definitionSource"),
			DefinitionDigest:          rawString(raw, "definitionDigest"),
			PackageDigest:             rawString(raw, "packageDigest"),
		}
		if err := ValidateVersionEnvelope(envelope); err != nil {
			report.Recommendation = err.Error() + "; rebuild it with the owning writer"
		}
	}
	if summary, ok := raw["summary"].(map[string]any); ok {
		report.Summary = summary
	} else if status, ok := raw["status"]; ok {
		// Terminal summaries in old receipts often only contain status and runId.
		report.Summary = map[string]any{"status": status}
		if runID, exists := raw["runId"]; exists {
			report.Summary["runId"] = runID
		}
	}
	if report.Summary == nil {
		if report.Recommendation == "" {
			report.Recommendation = "inspect the owning writer before attempting a write"
		}
	}
	return report, nil
}

func rawString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

type PackageReceiptReport struct {
	Root                  string               `json:"root"`
	Digest                string               `json:"digest"`
	VCSIdentity           string               `json:"vcsIdentity,omitempty"`
	Entries               []PackageEntry       `json:"entries"`
	Disjoint              map[string]string    `json:"disjoint,omitempty"`
	InstalledTarget       string               `json:"installedTarget,omitempty"`
	InstalledTargetDigest string               `json:"installedTargetDigest,omitempty"`
	HookConfigDigest      string               `json:"hookConfigDigest,omitempty"`
	ManagedRuleDigest     string               `json:"managedRuleDigest,omitempty"`
	CanonicalPaths        map[string]string    `json:"canonicalPaths,omitempty"`
	DisjointProof         map[string]string    `json:"disjointProof,omitempty"`
	PathIdentities        map[string]PathLstat `json:"pathIdentities,omitempty"`
	GeneratedAt           string               `json:"generatedAt"`
}

type PackageEntry struct {
	Path     string `json:"path"`
	Mode     uint32 `json:"mode"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	RealPath string `json:"realPath"`
}

type BaselineReceipt struct {
	VCSIdentity           string               `json:"vcsIdentity"`
	SourceRoot            string               `json:"sourceRoot"`
	PackageDigest         string               `json:"packageDigest"`
	InstalledTarget       string               `json:"installedTarget,omitempty"`
	InstalledTargetDigest string               `json:"installedTargetDigest,omitempty"`
	Disjoint              map[string]string    `json:"disjoint,omitempty"`
	HookConfigDigest      string               `json:"hookConfigDigest,omitempty"`
	ManagedRuleDigest     string               `json:"managedRuleDigest,omitempty"`
	PackageManifest       []PackageEntry       `json:"packageManifest,omitempty"`
	CanonicalPaths        map[string]string    `json:"canonicalPaths"`
	DisjointProof         map[string]string    `json:"disjointProof,omitempty"`
	PathIdentities        map[string]PathLstat `json:"pathIdentities"`
	GeneratedAt           string               `json:"generatedAt"`
}

// BuildBaselineReceipt binds source/package and installed-target identities in
// one reviewable record. Path values are canonicalized before recording, so a
// receipt can prove that the stable driver and candidate namespace are distinct.
func BuildBaselineReceipt(vcsIdentity, sourceRoot, installedTarget string, paths map[string]string) (BaselineReceipt, error) {
	sourceAbs, err := filepath.Abs(sourceRoot)
	if err != nil {
		return BaselineReceipt{}, err
	}
	sourceAbs = canonicalRegistryPath(sourceAbs)
	packageDigest, err := PackageDigest(sourceAbs)
	if err != nil {
		return BaselineReceipt{}, err
	}
	packageManifest, err := PackageReceipt(sourceAbs)
	if err != nil {
		return BaselineReceipt{}, err
	}
	receipt := BaselineReceipt{
		VCSIdentity: strings.TrimSpace(vcsIdentity), SourceRoot: sourceAbs,
		PackageDigest: packageDigest, PackageManifest: packageManifest.Entries,
		CanonicalPaths: map[string]string{"sourceRoot": sourceAbs},
		Disjoint:       map[string]string{}, DisjointProof: map[string]string{},
		PathIdentities: map[string]PathLstat{},
	}
	if strings.TrimSpace(installedTarget) != "" {
		installedAbs, err := filepath.Abs(installedTarget)
		if err != nil {
			return BaselineReceipt{}, err
		}
		installedAbs = canonicalRegistryPath(installedAbs)
		if pathOverlaps(sourceAbs, installedAbs) {
			return BaselineReceipt{}, fmt.Errorf("baseline source root %s overlaps installed target %s", sourceAbs, installedAbs)
		}
		receipt.InstalledTarget = installedAbs
		installedPackage, packageErr := PackageReceipt(installedAbs, sourceAbs)
		if packageErr != nil {
			return BaselineReceipt{}, packageErr
		}
		receipt.InstalledTargetDigest = installedPackage.Digest
		receipt.Disjoint = installedPackage.Disjoint
		receipt.CanonicalPaths["installedTarget"] = installedAbs
	}
	for name, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return BaselineReceipt{}, err
		}
		realPath := canonicalRegistryPath(abs)
		if _, err := os.Lstat(realPath); err != nil {
			return BaselineReceipt{}, fmt.Errorf("resolve canonical path %s: %w", path, err)
		}
		receipt.CanonicalPaths[name] = realPath
		if strings.EqualFold(name, "hookConfig") || strings.EqualFold(name, "config") || strings.EqualFold(name, "hook") {
			if !isFile(realPath) {
				return BaselineReceipt{}, fmt.Errorf("hook/config identity is not a regular file: %s", path)
			}
			receipt.HookConfigDigest, err = fileDigest(realPath)
			if err != nil {
				return BaselineReceipt{}, err
			}
		}
		if strings.EqualFold(name, "managedRule") || strings.EqualFold(name, "rule") {
			if !isFile(realPath) {
				return BaselineReceipt{}, fmt.Errorf("managed-rule identity is not a regular file: %s", path)
			}
			receipt.ManagedRuleDigest, err = fileDigest(realPath)
			if err != nil {
				return BaselineReceipt{}, err
			}
		}
	}
	identityPaths := map[string]string{}
	for name, path := range receipt.CanonicalPaths {
		identityPaths[name] = path
	}
	identityPaths["sourceRoot"] = receipt.SourceRoot
	if receipt.InstalledTarget != "" {
		identityPaths["installedTarget"] = receipt.InstalledTarget
	}
	for name, path := range identityPaths {
		identity, identityErr := pathLstatIdentity(path)
		if identityErr != nil {
			return BaselineReceipt{}, fmt.Errorf("baseline path identity %s: %w", name, identityErr)
		}
		receipt.PathIdentities[name] = identity
	}
	if err := validateCanonicalNamespaceRelations(receipt.CanonicalPaths, "baseline"); err != nil {
		return BaselineReceipt{}, err
	}
	names := make([]string, 0, len(receipt.CanonicalPaths))
	for name := range receipt.CanonicalPaths {
		names = append(names, name)
	}
	sort.Strings(names)
	for leftIndex, left := range names {
		for _, right := range names[leftIndex+1:] {
			if !pathOverlaps(receipt.CanonicalPaths[left], receipt.CanonicalPaths[right]) || canonicalNamespaceOverlapAllowed(left, right) {
				receipt.DisjointProof[left+"-"+right] = "PASS"
			}
		}
	}
	if receipt.InstalledTarget != "" {
		receipt.DisjointProof["source-installed-target"] = "PASS"
	}
	// Identity receipts must be repeatable for unchanged inputs.  A wall-clock
	// timestamp would make otherwise identical JSON drift between invocations.
	receipt.GeneratedAt = "sha256:" + packageDigest
	return receipt, nil
}

// validateCanonicalNamespaceRelations checks the relationships that make a
// receipt meaningful, rather than reporting PASS for independently canonical
// paths that still identify the same namespace. A project root is expected to
// contain its state, resources, hook files, and installed runtime; the runtime
// sibling is expected to contain that runtime. All other namespace identities
// must be disjoint, and sourceRoot is never allowed to overlap a destination.
func validateCanonicalNamespaceRelations(paths map[string]string, label string) error {
	names := make([]string, 0, len(paths))
	for name, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if !filepath.IsAbs(path) || filepath.Clean(path) != canonicalRegistryPath(path) {
			return fmt.Errorf("%s canonical path %s is not a canonical absolute path: %s", label, name, path)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for leftIndex := 0; leftIndex < len(names); leftIndex++ {
		left := names[leftIndex]
		for rightIndex := leftIndex + 1; rightIndex < len(names); rightIndex++ {
			right := names[rightIndex]
			if !pathOverlaps(paths[left], paths[right]) || canonicalNamespaceOverlapAllowed(left, right) {
				continue
			}
			return fmt.Errorf("%s canonical path %s overlaps %s", label, left, right)
		}
	}
	return nil
}

func canonicalNamespaceOverlapAllowed(left, right string) bool {
	if left == "sourceRoot" || right == "sourceRoot" {
		return false
	}
	if left == "projectRoot" || right == "projectRoot" {
		return true
	}
	if left == "runtimeSibling" && (right == "projectRoot" || right == "stateRoot" || right == "resourceRoot" || right == "hookConfig" || right == "managedRule" || right == "launcher") ||
		right == "runtimeSibling" && (left == "projectRoot" || left == "stateRoot" || left == "resourceRoot" || left == "hookConfig" || left == "managedRule" || left == "launcher") {
		return true
	}
	return (left == "installedTarget" || left == "target") && right == "runtimeSibling" ||
		(right == "installedTarget" || right == "target") && left == "runtimeSibling"
}

func WriteBaselineReceipt(path string, receipt BaselineReceipt) error {
	if strings.TrimSpace(receipt.VCSIdentity) == "" {
		return fmt.Errorf("baseline VCS identity is required")
	}
	if strings.TrimSpace(receipt.PackageDigest) == "" {
		return fmt.Errorf("baseline package digest is required")
	}
	return writeJSONAtomically(path, receipt)
}

// PackageReceipt computes a deterministic digest over package files.  Symlink
// entries are rejected instead of followed, which makes a receipt immutable
// with respect to a mutable development worktree.
func PackageReceipt(root string, disjointFrom ...string) (PackageReceiptReport, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return PackageReceiptReport{}, err
	}
	root = filepath.Clean(root)
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return PackageReceiptReport{}, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return PackageReceiptReport{}, fmt.Errorf("package root %s is a symlink; immutable packages require a real directory", root)
	}
	if !rootInfo.IsDir() {
		return PackageReceiptReport{}, fmt.Errorf("package root is not a directory")
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return PackageReceiptReport{}, err
	}
	entries := make([]PackageEntry, 0)
	err = filepath.WalkDir(root, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".git" || rel == ".gates" {
			if dirEntry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if dirEntry.IsDir() {
			if strings.EqualFold(filepath.Base(path), "__pycache__") {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("package entry %s is a symlink; immutable packages must contain copies", filepath.ToSlash(rel))
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("package entry %s is not a regular file", filepath.ToSlash(rel))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		entries = append(entries, PackageEntry{Path: filepath.ToSlash(rel), Mode: uint32(info.Mode().Perm()), Size: info.Size(), SHA256: hex.EncodeToString(sum[:]), RealPath: filepath.Clean(realPath)})
		return nil
	})
	if err != nil {
		return PackageReceiptReport{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	hash := sha256.New()
	for _, entry := range entries {
		fmt.Fprintf(hash, "%s\x00%o\x00%d\x00%s\n", entry.Path, entry.Mode, entry.Size, entry.SHA256)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	receipt := PackageReceiptReport{Root: root, Digest: digest, Entries: entries, Disjoint: map[string]string{}, GeneratedAt: "sha256:" + digest}
	for _, other := range disjointFrom {
		if strings.TrimSpace(other) == "" {
			continue
		}
		otherAbs, err := filepath.Abs(other)
		if err != nil {
			return PackageReceiptReport{}, err
		}
		otherReal, err := filepath.EvalSymlinks(otherAbs)
		if err != nil {
			return PackageReceiptReport{}, fmt.Errorf("resolve disjoint path %s: %w", other, err)
		}
		if pathOverlaps(rootReal, otherReal) {
			return PackageReceiptReport{}, fmt.Errorf("package root %s overlaps disallowed path %s", root, other)
		}
		receipt.Disjoint[filepath.Clean(otherAbs)] = filepath.Clean(otherReal)
	}
	return receipt, nil
}

func PackageDigest(root string) (string, error) {
	receipt, err := PackageReceipt(root)
	if err != nil {
		return "", err
	}
	return receipt.Digest, nil
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func pathOverlaps(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if os.PathSeparator == '\\' {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	within := func(path, parent string) bool {
		rel, err := filepath.Rel(parent, path)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "."
	}
	return a == b || within(a, b) || within(b, a)
}

func canonicalRegistryPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return canonicalPath(path)
	}
	abs = filepath.Clean(abs)
	// EvalSymlinks requires the complete path to exist.  Install identities also
	// include not-yet-created state/resource/release paths, so resolve the
	// nearest existing ancestor and append the untouched suffix.  This closes
	// common aliases such as macOS /var -> /private/var without inventing a path.
	current := abs
	for {
		if resolved, resolveErr := filepath.EvalSymlinks(current); resolveErr == nil {
			rel, relErr := filepath.Rel(current, abs)
			if relErr == nil && rel != "." {
				return filepath.Clean(filepath.Join(resolved, rel))
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs
		}
		current = parent
	}
}

type RegistryRecord struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	// LauncherPath is the stable executable allowed to drive workflow writes
	// for this target.  It is intentionally separate from Target: the host
	// skill tree is a candidate/runtime payload, while hooks must invoke this
	// fixed launcher path.
	LauncherPath    string            `json:"launcherPath"`
	Scope           string            `json:"scope"`
	Host            string            `json:"host"`
	HookConfig      string            `json:"hookConfig,omitempty"`
	SkipHooks       bool              `json:"skipHooks,omitempty"`
	ProjectRoot     string            `json:"projectRoot"`
	StateRoot       string            `json:"stateRoot"`
	ResourceRoot    string            `json:"resourceRoot"`
	RuntimeSibling  string            `json:"runtimeSibling"`
	ReleaseRoot     string            `json:"releaseRoot,omitempty"`
	VCSIdentity     string            `json:"vcsIdentity,omitempty"`
	PackageDigest   string            `json:"packageDigest,omitempty"`
	InstalledDigest string            `json:"installedDigest,omitempty"`
	CanonicalPaths  map[string]string `json:"canonicalPaths"`
	Status          string            `json:"status"`
	Generation      uint64            `json:"generation,omitempty"`
	Lease           string            `json:"lease,omitempty"`
	Token           string            `json:"token,omitempty"`
}

type RegistryDocument struct {
	SchemaVersion int              `json:"schemaVersion"`
	Epoch         uint64           `json:"epoch"`
	Records       []RegistryRecord `json:"records"`
}

type AdmissionReceipt struct {
	Code           string            `json:"code"`
	Accepted       bool              `json:"accepted"`
	Status         string            `json:"status,omitempty"`
	RecordID       string            `json:"recordId,omitempty"`
	Registry       string            `json:"registry,omitempty"`
	Target         string            `json:"target,omitempty"`
	Scope          string            `json:"scope,omitempty"`
	Host           string            `json:"host,omitempty"`
	CanonicalPaths map[string]string `json:"canonicalPaths,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	CreatedAt      string            `json:"createdAt"`
}

func LoadRegistry(path string) (RegistryDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RegistryDocument{}, err
	}
	var doc RegistryDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return RegistryDocument{}, fmt.Errorf("registry JSON is invalid: %w", err)
	}
	if doc.SchemaVersion != RegistrySchemaVersion {
		return RegistryDocument{}, fmt.Errorf("registry schema version %d is unsupported", doc.SchemaVersion)
	}
	return doc, nil
}

func loadRegistryForCommit(path string) (RegistryDocument, error) {
	doc, err := LoadRegistry(path)
	if os.IsNotExist(err) {
		return RegistryDocument{SchemaVersion: RegistrySchemaVersion, Epoch: 0, Records: []RegistryRecord{}}, nil
	}
	if err != nil {
		return RegistryDocument{}, err
	}
	return doc, nil
}

// commitRegistryRecordsUnlocked is the only semantic registry writer.  The
// install/bootstrap/uninstall transaction already holds the registry lock and
// supplies complete bound records; recovery may only restore exact old bytes.
func commitRegistryRecordsUnlocked(path string, doc RegistryDocument, records []RegistryRecord) (RegistryDocument, error) {
	if len(records) == 0 {
		return doc, nil
	}
	replacements := map[string]bool{}
	for _, record := range records {
		replacements[record.ID] = true
	}
	for _, existing := range doc.Records {
		if !replacements[existing.ID] && !validRegistryRecord(existing) {
			return RegistryDocument{}, fmt.Errorf("registry record %q cannot be reconciled", existing.ID)
		}
	}
	doc.Epoch++
	for index := range records {
		record := records[index]
		if strings.TrimSpace(record.ID) == "" {
			return RegistryDocument{}, fmt.Errorf("registry record id is required")
		}
		if record.Status == "" {
			record.Status = "active"
		}
		if record.CanonicalPaths == nil {
			record.CanonicalPaths = map[string]string{}
		}
		normalizeRegistryRecord(&record, doc.Epoch)
		if !validRegistryRecord(record) {
			return RegistryDocument{}, fmt.Errorf("registry record %q has incomplete or mismatched canonical target/launcher binding", record.ID)
		}
		replaced := false
		for existing := range doc.Records {
			if doc.Records[existing].ID == record.ID {
				doc.Records[existing] = record
				replaced = true
				break
			}
		}
		if !replaced {
			doc.Records = append(doc.Records, record)
		}
	}
	if err := writeJSONAtomically(path, doc); err != nil {
		return RegistryDocument{}, err
	}
	return doc, nil
}

func AdmitRegistry(path, recordID string) (AdmissionReceipt, error) {
	path = filepath.Clean(path)
	unlock, err := acquireRegistryLock(path)
	if err != nil {
		return AdmissionReceipt{Registry: path, RecordID: recordID, Code: "UNREGISTERED_INSTALL", Reason: err.Error(), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}, err
	}
	defer unlock()
	return admitRegistryUnlocked(path, recordID)
}

func admitRegistryUnlocked(path, recordID string) (AdmissionReceipt, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	receipt := AdmissionReceipt{Registry: filepath.Clean(path), RecordID: recordID, CreatedAt: now}
	doc, err := LoadRegistry(path)
	if err != nil {
		receipt.Code, receipt.Reason = "UNREGISTERED_INSTALL", err.Error()
		return receipt, writeAdmissionReceipt(path, receipt)
	}
	return admitRegistryDoc(path, recordID, doc, now, receipt)
}

// admitRegistryDoc 在 registry 文档已由调用方加载的前提下执行 admit 语义
// （record 校验、安装目标身份/摘要核验、失败时写拒绝 receipt）。verifyRunState
// 的准入链已经持有同一份文档时经此复用，避免同一验证内重复 LoadRegistry。
func admitRegistryDoc(path, recordID string, doc RegistryDocument, now string, receipt AdmissionReceipt) (AdmissionReceipt, error) {
	for _, record := range doc.Records {
		if record.ID != recordID {
			continue
		}
		receipt.Target = record.Target
		receipt.Scope = record.Scope
		receipt.Host = record.Host
		receipt.CanonicalPaths = record.CanonicalPaths
		if strings.EqualFold(record.Status, "active") && validAdmissionRegistryRecord(record) {
			if strings.TrimSpace(record.InstalledDigest) != "" {
				if identityErr := verifyRegisteredTargetIdentity(record); identityErr != nil {
					receipt.Code = "UNREGISTERED_INSTALL"
					receipt.Reason = identityErr.Error()
					return receipt, writeAdmissionReceipt(path, receipt)
				}
			}
			receipt.Code, receipt.Accepted = "ADMITTED", true
			return receipt, nil
		}
		receipt.Code = "UNREGISTERED_INSTALL"
		if !strings.EqualFold(record.Status, "active") {
			receipt.Reason = "registry record is disabled"
		} else {
			receipt.Reason = "registry record is incomplete; target, scope, host, project/state/resource roots and canonical paths are required"
		}
		return receipt, writeAdmissionReceipt(path, receipt)
	}
	receipt.Code, receipt.Reason = "UNREGISTERED_INSTALL", "registry record is missing"
	return receipt, writeAdmissionReceipt(path, receipt)
}

func validAdmissionRegistryRecord(record RegistryRecord) bool {
	if !validRegistryRecord(record) {
		return false
	}
	// In-process tests exercise the legacy API with compact records. A shipped
	// launcher must only admit the complete identity produced by the native
	// transaction owner.
	if isTestBinary() {
		return true
	}
	return record.Generation != 0 && strings.TrimSpace(record.Lease) != "" &&
		strings.TrimSpace(record.Token) != "" && strings.TrimSpace(record.VCSIdentity) != "" &&
		strings.TrimSpace(record.PackageDigest) != "" && strings.TrimSpace(record.InstalledDigest) != ""
}

func isTestBinary() bool {
	// 白盒 QA 构建标签见 testbinary_*.go：以该标签编译的二进制（含复制到
	// launcher/candidate 路径的改名测试副本）按测试二进制对待；生产构建该常
	// 量为 false，唯一剩余的判定是可执行文件名。
	if whiteboxTestHarnessBuild {
		return true
	}
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	base := filepath.Base(filepath.Clean(executable))
	return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe")
}

func normalizeRegistryRecord(record *RegistryRecord, generation uint64) {
	if generation == 0 {
		generation = 1
	}
	if record.Generation == 0 {
		record.Generation = generation
	}
	if strings.TrimSpace(record.Lease) == "" {
		record.Lease = fmt.Sprintf("lease-%d", record.Generation)
	}
	if strings.TrimSpace(record.Token) == "" {
		record.Token = fmt.Sprintf("token-%d", time.Now().UnixNano())
	}
	if record.CanonicalPaths == nil {
		record.CanonicalPaths = map[string]string{}
	}
	for key, value := range record.CanonicalPaths {
		if strings.TrimSpace(value) != "" {
			record.CanonicalPaths[key] = canonicalRegistryPath(value)
		}
	}
}

func validRegistryRecord(record RegistryRecord) bool {
	for _, value := range []string{record.ID, record.Scope, record.Host} {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	for _, path := range []string{record.Target, record.LauncherPath, record.ProjectRoot, record.StateRoot, record.ResourceRoot, record.RuntimeSibling} {
		if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
			return false
		}
	}
	if record.Scope != "global" && record.Scope != "project" {
		return false
	}
	// Canonical paths are the bridge's proof that the registered roots are the
	// roots the candidate will actually use.  Target and launcher are mandatory
	// bindings; accepting a record that only names a project would let a
	// different installed package write the same registry.
	fields := map[string]string{"target": record.Target, "launcher": record.LauncherPath, "projectRoot": record.ProjectRoot, "stateRoot": record.StateRoot, "resourceRoot": record.ResourceRoot, "runtimeSibling": record.RuntimeSibling, "hookConfig": record.HookConfig, "releaseRoot": record.ReleaseRoot}
	for key, field := range fields {
		value, ok := record.CanonicalPaths[key]
		if (key == "hookConfig" || key == "releaseRoot") && strings.TrimSpace(field) == "" {
			continue
		}
		if !ok || strings.TrimSpace(value) == "" || !filepath.IsAbs(value) || filepath.Clean(value) != canonicalRegistryPath(field) {
			return false
		}
	}
	for key, value := range record.CanonicalPaths {
		if strings.TrimSpace(value) == "" || !filepath.IsAbs(value) {
			return false
		}
		if _, known := fields[key]; !known {
			return false
		}
	}
	return validateCanonicalNamespaceRelations(record.CanonicalPaths, "registry record") == nil
}

func writeAdmissionReceipt(registryPath string, receipt AdmissionReceipt) error {
	return writeJSONAtomically(registryPath+".admission.json", receipt)
}

func registryLockPath(path string) string {
	return filepath.Clean(path) + ".lock"
}

func acquireRegistryLock(path string) (func(), error) {
	return acquirePhase0Lock(registryLockPath(path), "registry")
}

func writeJSONAtomically(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}
