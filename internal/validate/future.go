package validate

// This file is the public candidate/future envelope surface.  It is separate
// from the stable RunState writer: a candidate must identify the immutable
// workflow definition it was built from before it can create or bump state.

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
	return FutureDefinition{
		Source:          CurrentWorkflowDefinitionSource,
		Digest:          "sha256:" + hex.EncodeToString(sum[:]),
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

func WriteFutureVersionedState(root, path string, envelope VersionEnvelope, value any) error {
	if err := ValidateFutureEnvelope(root, envelope); err != nil {
		return err
	}
	return writeVersionedStateDocument(path, envelope, value)
}

func DiagnoseFutureState(root, path string) (DiagnoseReport, error) {
	report, err := DiagnoseState(path)
	if err != nil {
		return report, err
	}
	definition, definitionErr := LoadFutureDefinition(root)
	if definitionErr != nil {
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

// BumpFutureState advances the generation owned by the candidate writer. It
// validates the current definition before reading or writing and refuses
// legacy/unversioned documents; there is no migration fallback here.
func BumpFutureState(root, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("future state is not JSON: %w", err)
	}
	envelope := VersionEnvelope{
		Writer:                    rawString(document, "writer"),
		StateSchemaVersion:        rawString(document, "stateSchemaVersion"),
		WorkflowDefinitionVersion: rawString(document, "workflowDefinitionVersion"),
		DefinitionSource:          rawString(document, "definitionSource"),
		DefinitionDigest:          rawString(document, "definitionDigest"),
		PackageDigest:             rawString(document, "packageDigest"),
	}
	if err := ValidateFutureEnvelope(root, envelope); err != nil {
		return err
	}
	generation := uint64(0)
	switch value := document["generation"].(type) {
	case float64:
		if value < 0 || value != float64(uint64(value)) {
			return fmt.Errorf("future state generation is invalid")
		}
		generation = uint64(value)
	case nil:
	default:
		return fmt.Errorf("future state generation is invalid")
	}
	if generation == ^uint64(0) {
		return fmt.Errorf("future state generation overflow")
	}
	document["generation"] = generation + 1
	document["writer"] = envelope.Writer
	document["stateSchemaVersion"] = envelope.StateSchemaVersion
	document["workflowDefinitionVersion"] = envelope.WorkflowDefinitionVersion
	document["definitionSource"] = envelope.DefinitionSource
	document["definitionDigest"] = envelope.DefinitionDigest
	return writeJSONAtomically(path, document)
}
