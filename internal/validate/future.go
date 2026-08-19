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
	if stateVersion != CurrentStateSchemaVersion {
		return FutureDefinition{}, &UnsupportedRunVersionError{Field: "stateSchemaVersion", Expected: CurrentStateSchemaVersion, Observed: stateVersion}
	}
	if workflowVersion != CurrentWorkflowDefinitionVersion {
		return FutureDefinition{}, &UnsupportedRunVersionError{Field: "workflowDefinitionVersion", Expected: CurrentWorkflowDefinitionVersion, Observed: workflowVersion}
	}
	if digest != CurrentWorkflowDefinitionDigest {
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
	envelope := VersionEnvelope{
		Writer:                    "engine",
		StateSchemaVersion:        definition.SchemaVersion,
		WorkflowDefinitionVersion: definition.WorkflowVersion,
		DefinitionSource:          definition.Source,
		DefinitionDigest:          definition.Digest,
		PackageDigest:             strings.TrimSpace(packageDigest),
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
	checks := []struct {
		field, observed, expected string
	}{
		{"writer", envelope.Writer, "engine"},
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

func DiagnoseFutureState(root, path string) (DiagnoseReport, error) {
	report, err := DiagnoseState(path)
	if err != nil {
		return report, err
	}
	definition, definitionErr := LoadFutureDefinition(root)
	if definitionErr != nil {
		if IsUnsupportedRunVersion(definitionErr) {
			report.Recommendation = definitionErr.Error() + "; rebuild the candidate from the owning definition"
			return report, nil
		}
		return report, definitionErr
	}
	report.Supported = VersionEnvelope{
		Writer:                    "engine",
		StateSchemaVersion:        definition.SchemaVersion,
		WorkflowDefinitionVersion: definition.WorkflowVersion,
		DefinitionSource:          definition.Source,
		DefinitionDigest:          definition.Digest,
	}
	var raw map[string]any
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return report, readErr
	}
	if jsonErr := json.Unmarshal(data, &raw); jsonErr == nil {
		envelope := VersionEnvelope{
			Writer:                    rawString(raw, "writer"),
			StateSchemaVersion:        rawString(raw, "stateSchemaVersion"),
			WorkflowDefinitionVersion: rawString(raw, "workflowDefinitionVersion"),
			DefinitionSource:          rawString(raw, "definitionSource"),
			DefinitionDigest:          rawString(raw, "definitionDigest"),
			PackageDigest:             rawString(raw, "packageDigest"),
		}
		if validateErr := validateFutureEnvelope(envelope, definition); validateErr != nil {
			report.Recommendation = validateErr.Error() + "; rebuild it with the owning future writer"
		}
	}
	return report, nil
}
