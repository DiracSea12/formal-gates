package validate

// This file owns the read-only candidate/future envelope surface. It is
// separate from the stable RunState writer: a candidate must identify the
// immutable workflow definition it was built from before it can be inspected.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FutureDefinition struct {
	Source          string `json:"source"`
	Digest          string `json:"digest"`
	SchemaVersion   string `json:"stateSchemaVersion"`
	WorkflowVersion string `json:"workflowDefinitionVersion"`
}

// LoadFutureDefinition reads the one package-owned workflow definition and
// computes its digest from bytes.  It does not accept a caller-supplied
// placeholder or an alternate definition path.
func LoadFutureDefinition(root string) (FutureDefinition, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return FutureDefinition{}, err
	}
	source := filepath.Join(rootAbs, filepath.FromSlash(CurrentWorkflowDefinitionSource))
	data, err := os.ReadFile(source)
	if err != nil {
		return FutureDefinition{}, fmt.Errorf("read workflow definition %s: %w", source, err)
	}
	var document struct {
		StateSchemaVersion   string `json:"stateSchemaVersion"`
		WorkflowVersion      string `json:"version"`
		WorkflowVersionAlias string `json:"workflowDefinitionVersion"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return FutureDefinition{}, fmt.Errorf("parse workflow definition %s: %w", source, err)
	}
	stateVersion := strings.TrimSpace(document.StateSchemaVersion)
	workflowVersion := strings.TrimSpace(document.WorkflowVersion)
	if workflowVersion == "" {
		workflowVersion = strings.TrimSpace(document.WorkflowVersionAlias)
	}
	if stateVersion == "" || workflowVersion == "" {
		return FutureDefinition{}, fmt.Errorf("workflow definition %s is missing stateSchemaVersion or version", source)
	}
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	// The current stable definition is one supported candidate. A changed
	// definition becomes a new future candidate only when at least one version
	// is bumped; changing bytes without bumping either version is ambiguous and
	// remains an unsupported run version.
	if digest != CurrentWorkflowDefinitionDigest && stateVersion == CurrentStateSchemaVersion && workflowVersion == CurrentWorkflowDefinitionVersion {
		return FutureDefinition{}, &UnsupportedRunVersionError{Field: "definitionDigest", Expected: CurrentWorkflowDefinitionDigest, Observed: digest}
	}
	return FutureDefinition{
		Source:          CurrentWorkflowDefinitionSource,
		Digest:          digest,
		SchemaVersion:   stateVersion,
		WorkflowVersion: workflowVersion,
	}, nil
}

func GenerateFutureEnvelope(root, packageDigest string) (VersionEnvelope, error) {
	definition, err := LoadFutureDefinition(root)
	if err != nil {
		return VersionEnvelope{}, err
	}
	// The legacy future writer is intentionally independent of installed-package
	// identity. Keep the argument for source compatibility with the stage-0 API,
	// but do not read, validate, or emit it.
	_ = packageDigest
	envelope := VersionEnvelope{
		StateSchemaVersion:        definition.SchemaVersion,
		WorkflowDefinitionVersion: definition.WorkflowVersion,
		DefinitionSource:          definition.Source,
		DefinitionDigest:          definition.Digest,
	}
	if err := validateFutureEnvelope(envelope, definition); err != nil {
		return VersionEnvelope{}, err
	}
	return envelope, nil
}

func ValidateFutureEnvelope(root string, envelope VersionEnvelope) error {
	definition, err := LoadFutureDefinition(root)
	if err != nil {
		return err
	}
	return validateFutureEnvelope(envelope, definition)
}

func validateFutureEnvelope(envelope VersionEnvelope, definition FutureDefinition) error {
	if strings.TrimSpace(envelope.Writer) != "" {
		return &UnsupportedRunVersionError{Field: "writer", Expected: "absent", Observed: envelope.Writer}
	}
	if strings.TrimSpace(envelope.PackageDigest) != "" {
		return &UnsupportedRunVersionError{Field: "packageDigest", Expected: "absent", Observed: envelope.PackageDigest}
	}
	checks := []struct {
		field, observed, expected string
	}{
		{"stateSchemaVersion", envelope.StateSchemaVersion, definition.SchemaVersion},
		{"workflowDefinitionVersion", envelope.WorkflowDefinitionVersion, definition.WorkflowVersion},
		{"definitionSource", envelope.DefinitionSource, definition.Source},
		{"definitionDigest", envelope.DefinitionDigest, definition.Digest},
	}
	for _, check := range checks {
		if strings.TrimSpace(check.observed) == "" || check.observed != check.expected {
			return &UnsupportedRunVersionError{Field: check.field, Expected: check.expected, Observed: check.observed}
		}
	}
	return nil
}

func WriteFutureEnvelope(root, path string, envelope VersionEnvelope) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("future envelope output path is required")
	}
	if err := ValidateFutureEnvelope(root, envelope); err != nil {
		return err
	}
	return writeJSONAtomically(path, envelope)
}

func LoadFutureEnvelope(root, path string) (VersionEnvelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return VersionEnvelope{}, err
	}
	var envelope VersionEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return VersionEnvelope{}, fmt.Errorf("future envelope JSON is invalid: %w", err)
	}
	if err := ValidateFutureEnvelope(root, envelope); err != nil {
		return VersionEnvelope{}, err
	}
	return envelope, nil
}

func WriteFutureState(root, path string, envelope VersionEnvelope, value any) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("future state output path is required")
	}
	definition, err := LoadFutureDefinition(root)
	if err != nil {
		return err
	}
	if err := validateFutureEnvelope(envelope, definition); err != nil {
		return err
	}
	// This writer predates package admission. Its on-disk identity is exactly
	// the four historical definition fields; in particular it must not acquire
	// the candidate writer or package digest fields used by phase 2 state.
	document := map[string]any{}
	if fields, ok := value.(map[string]any); ok {
		for key, item := range fields {
			document[key] = item
		}
	} else if value != nil {
		document["payload"] = value
	}
	document["stateSchemaVersion"] = envelope.StateSchemaVersion
	document["workflowDefinitionVersion"] = envelope.WorkflowDefinitionVersion
	document["definitionSource"] = envelope.DefinitionSource
	document["definitionDigest"] = envelope.DefinitionDigest
	return writeJSONAtomically(path, document)
}

func DiagnoseFutureState(root, path string) (DiagnoseReport, error) {
	definition, definitionErr := LoadFutureDefinition(root)
	if definitionErr != nil {
		if IsUnsupportedRunVersion(definitionErr) {
			return DiagnoseReport{Path: filepath.Clean(path), Supported: VersionEnvelope{
				StateSchemaVersion: CurrentStateSchemaVersion, WorkflowDefinitionVersion: CurrentWorkflowDefinitionVersion,
				DefinitionSource: CurrentWorkflowDefinitionSource, DefinitionDigest: CurrentWorkflowDefinitionDigest,
			}, Integrity: "unsupported", Recommendation: definitionErr.Error() + "; rebuild the candidate from the owning definition"}, nil
		}
		return DiagnoseReport{Path: filepath.Clean(path)}, definitionErr
	}
	report := DiagnoseReport{
		Path: filepath.Clean(path), Integrity: "unknown",
		Supported: VersionEnvelope{
			StateSchemaVersion:        definition.SchemaVersion,
			WorkflowDefinitionVersion: definition.WorkflowVersion,
			DefinitionSource:          definition.Source,
			DefinitionDigest:          definition.Digest,
		},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return report, err
	}
	var raw map[string]any
	if jsonErr := json.Unmarshal(data, &raw); jsonErr != nil {
		report.Recommendation = "rebuild the state with the owning future writer"
		return report, nil
	}
	report.JSONReadable = true
	report.Integrity = "readable"
	report.DetectedVersions = map[string]any{}
	for _, key := range []string{"stateSchemaVersion", "workflowDefinitionVersion", "definitionSource", "definitionDigest"} {
		if value, ok := raw[key]; ok {
			report.DetectedVersions[key] = value
		}
	}
	envelope := VersionEnvelope{
		StateSchemaVersion:        rawString(raw, "stateSchemaVersion"),
		WorkflowDefinitionVersion: rawString(raw, "workflowDefinitionVersion"),
		DefinitionSource:          rawString(raw, "definitionSource"),
		DefinitionDigest:          rawString(raw, "definitionDigest"),
	}
	if _, hasWriter := raw["writer"]; hasWriter {
		report.Integrity = "unsupported"
		report.Recommendation = (&UnsupportedRunVersionError{Field: "writer", Expected: "absent", Observed: rawString(raw, "writer")}).Error() + "; rebuild it with the owning future writer"
	} else if _, hasPackageDigest := raw["packageDigest"]; hasPackageDigest {
		report.Integrity = "unsupported"
		report.Recommendation = (&UnsupportedRunVersionError{Field: "packageDigest", Expected: "absent", Observed: rawString(raw, "packageDigest")}).Error() + "; rebuild it with the owning future writer"
	} else if validateErr := validateFutureEnvelope(envelope, definition); validateErr != nil {
		report.Integrity = "unsupported"
		report.Recommendation = validateErr.Error() + "; rebuild it with the owning future writer"
	}
	if summary, ok := raw["summary"].(map[string]any); ok {
		report.Summary = summary
	} else if status, ok := raw["status"]; ok {
		report.Summary = map[string]any{"status": status}
		if runID, exists := raw["runId"]; exists {
			report.Summary["runId"] = runID
		}
	}
	if report.Summary == nil && report.Recommendation == "" {
		report.Recommendation = "inspect the owning writer before attempting a write"
	}
	return report, nil
}
