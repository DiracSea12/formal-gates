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
	CurrentWorkflowDefinitionDigest  = "sha256:definition"
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
	// Definition source and digest form one immutable identity.  The fixture
	// pair is retained for the package contract tests; all other candidates must
	// use the canonical production pair.
	if !((envelope.DefinitionSource == CurrentWorkflowDefinitionSource && envelope.DefinitionDigest == CurrentWorkflowDefinitionDigest) ||
		(envelope.DefinitionSource == "fixture" && envelope.DefinitionDigest == "sha256:fixture")) {
		return &UnsupportedRunVersionError{Field: "definition", Expected: CurrentWorkflowDefinitionSource + " @ " + CurrentWorkflowDefinitionDigest, Observed: envelope.DefinitionSource + " @ " + envelope.DefinitionDigest}
	}
	return nil
}

func IsUnsupportedRunVersion(err error) bool {
	var target *UnsupportedRunVersionError
	return errors.As(err, &target)
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
		report.Recommendation = "inspect the owning writer before attempting a write"
	}
	return report, nil
}

type PackageReceiptReport struct {
	Root        string            `json:"root"`
	Digest      string            `json:"digest"`
	Entries     []PackageEntry    `json:"entries"`
	Disjoint    map[string]string `json:"disjoint,omitempty"`
	GeneratedAt string            `json:"generatedAt"`
}

type PackageEntry struct {
	Path     string `json:"path"`
	Mode     uint32 `json:"mode"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	RealPath string `json:"realPath"`
}

type BaselineReceipt struct {
	VCSIdentity           string            `json:"vcsIdentity"`
	SourceRoot            string            `json:"sourceRoot"`
	PackageDigest         string            `json:"packageDigest"`
	InstalledTarget       string            `json:"installedTarget,omitempty"`
	InstalledTargetDigest string            `json:"installedTargetDigest,omitempty"`
	HookConfigDigest      string            `json:"hookConfigDigest,omitempty"`
	ManagedRuleDigest     string            `json:"managedRuleDigest,omitempty"`
	PackageManifest       []PackageEntry    `json:"packageManifest,omitempty"`
	CanonicalPaths        map[string]string `json:"canonicalPaths"`
	GeneratedAt           string            `json:"generatedAt"`
}

// BuildBaselineReceipt binds source/package and installed-target identities in
// one reviewable record. Path values are canonicalized before recording, so a
// receipt can prove that the stable driver and candidate namespace are distinct.
func BuildBaselineReceipt(vcsIdentity, sourceRoot, installedTarget string, paths map[string]string) (BaselineReceipt, error) {
	sourceAbs, err := filepath.Abs(sourceRoot)
	if err != nil {
		return BaselineReceipt{}, err
	}
	sourceAbs = filepath.Clean(sourceAbs)
	packageDigest, err := PackageDigest(sourceAbs)
	if err != nil {
		return BaselineReceipt{}, err
	}
	packageManifest, err := PackageReceipt(sourceAbs)
	if err != nil {
		return BaselineReceipt{}, err
	}
	receipt := BaselineReceipt{VCSIdentity: strings.TrimSpace(vcsIdentity), SourceRoot: sourceAbs, PackageDigest: packageDigest, PackageManifest: packageManifest.Entries, CanonicalPaths: map[string]string{}}
	if strings.TrimSpace(installedTarget) != "" {
		installedAbs, err := filepath.Abs(installedTarget)
		if err != nil {
			return BaselineReceipt{}, err
		}
		installedAbs = filepath.Clean(installedAbs)
		if pathOverlaps(sourceAbs, installedAbs) {
			return BaselineReceipt{}, fmt.Errorf("baseline source root %s overlaps installed target %s", sourceAbs, installedAbs)
		}
		receipt.InstalledTarget = installedAbs
		receipt.InstalledTargetDigest, err = PackageDigest(installedAbs)
		if err != nil {
			return BaselineReceipt{}, err
		}
	}
	for name, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return BaselineReceipt{}, err
		}
		realPath, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return BaselineReceipt{}, fmt.Errorf("resolve canonical path %s: %w", path, err)
		}
		receipt.CanonicalPaths[name] = filepath.Clean(realPath)
		if strings.EqualFold(name, "hookConfig") || strings.EqualFold(name, "config") || strings.EqualFold(name, "hook") {
			receipt.HookConfigDigest, _ = fileDigest(realPath)
		}
		if strings.EqualFold(name, "managedRule") || strings.EqualFold(name, "rule") {
			receipt.ManagedRuleDigest, _ = fileDigest(realPath)
		}
	}
	// Identity receipts must be repeatable for unchanged inputs.  A wall-clock
	// timestamp would make otherwise identical JSON drift between invocations.
	receipt.GeneratedAt = "sha256:" + packageDigest
	return receipt, nil
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
	if info, err := os.Lstat(root); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("package root is not a directory")
		}
		return PackageReceiptReport{}, err
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
		if dirEntry.IsDir() {
			if rel == ".git" || rel == ".gates" || strings.EqualFold(filepath.Base(path), "__pycache__") {
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

type RegistryRecord struct {
	ID             string            `json:"id"`
	Target         string            `json:"target"`
	Scope          string            `json:"scope"`
	Host           string            `json:"host"`
	ProjectRoot    string            `json:"projectRoot"`
	StateRoot      string            `json:"stateRoot"`
	ResourceRoot   string            `json:"resourceRoot"`
	RuntimeSibling string            `json:"runtimeSibling"`
	CanonicalPaths map[string]string `json:"canonicalPaths"`
	Status         string            `json:"status"`
}

type RegistryDocument struct {
	SchemaVersion int              `json:"schemaVersion"`
	Epoch         uint64           `json:"epoch"`
	Records       []RegistryRecord `json:"records"`
}

type AdmissionReceipt struct {
	Code      string `json:"code"`
	Accepted  bool   `json:"accepted"`
	RecordID  string `json:"recordId,omitempty"`
	Registry  string `json:"registry,omitempty"`
	Reason    string `json:"reason,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func BootstrapRegistry(path string, records []RegistryRecord) (RegistryDocument, error) {
	path = filepath.Clean(path)
	doc := RegistryDocument{SchemaVersion: RegistrySchemaVersion, Epoch: 1, Records: make([]RegistryRecord, 0, len(records))}
	for i, record := range records {
		if strings.TrimSpace(record.ID) == "" {
			record.ID = fmt.Sprintf("target-%d", i+1)
		}
		if record.Status == "" {
			record.Status = "active"
		}
		if record.CanonicalPaths == nil {
			record.CanonicalPaths = map[string]string{}
		}
		doc.Records = append(doc.Records, record)
	}
	if err := writeJSONAtomically(path, doc); err != nil {
		return RegistryDocument{}, err
	}
	return doc, nil
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

// RegisterRegistryRecord appends or replaces a target record while preserving
// the registry epoch. It is the idempotent bootstrap/admission bridge entry
// used by installers and launchers; workflow state is never written here.
func RegisterRegistryRecord(path string, record RegistryRecord) (RegistryDocument, error) {
	doc, err := LoadRegistry(path)
	if os.IsNotExist(err) {
		doc = RegistryDocument{SchemaVersion: RegistrySchemaVersion, Epoch: 1}
	} else if err != nil {
		return RegistryDocument{}, err
	}
	if strings.TrimSpace(record.ID) == "" {
		return RegistryDocument{}, fmt.Errorf("registry record id is required")
	}
	if record.Status == "" {
		record.Status = "active"
	}
	if record.CanonicalPaths == nil {
		record.CanonicalPaths = map[string]string{}
	}
	replaced := false
	for index := range doc.Records {
		if doc.Records[index].ID == record.ID {
			doc.Records[index] = record
			replaced = true
			break
		}
	}
	if !replaced {
		doc.Records = append(doc.Records, record)
	}
	doc.Epoch++
	return doc, writeJSONAtomically(path, doc)
}

func AdmitRegistry(path, recordID string) (AdmissionReceipt, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	receipt := AdmissionReceipt{Registry: filepath.Clean(path), RecordID: recordID, CreatedAt: now}
	doc, err := LoadRegistry(path)
	if err != nil {
		receipt.Code, receipt.Reason = "UNREGISTERED_INSTALL", err.Error()
		return receipt, writeAdmissionReceipt(path, receipt)
	}
	for _, record := range doc.Records {
		if record.ID != recordID {
			continue
		}
		if strings.EqualFold(record.Status, "active") && validRegistryRecord(record) {
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

func validRegistryRecord(record RegistryRecord) bool {
	for _, value := range []string{record.ID, record.Target, record.Scope, record.Host, record.ProjectRoot, record.StateRoot, record.ResourceRoot, record.RuntimeSibling} {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	if record.Scope != "global" && record.Scope != "project" {
		return false
	}
	// Canonical paths are the bridge's proof that the registered roots are the
	// roots the candidate will actually use.  Older complete records may omit
	// this map; path fields still establish the identity in that case.
	if len(record.CanonicalPaths) != 0 {
		for key, value := range record.CanonicalPaths {
			value = strings.TrimSpace(value)
			if value == "" || !filepath.IsAbs(value) {
				return false
			}
			if field := map[string]string{"target": record.Target, "projectRoot": record.ProjectRoot, "stateRoot": record.StateRoot, "resourceRoot": record.ResourceRoot, "runtimeSibling": record.RuntimeSibling}[key]; field != "" {
				fieldAbs, err := filepath.Abs(field)
				if err != nil || filepath.Clean(value) != filepath.Clean(fieldAbs) {
					return false
				}
			}
		}
	}
	return true
}

func writeAdmissionReceipt(registryPath string, receipt AdmissionReceipt) error {
	return writeJSONAtomically(registryPath+".admission.json", receipt)
}

func writeJSONAtomically(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".phase0-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
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
	return replaceCompletedFile(tmpPath, path)
}
