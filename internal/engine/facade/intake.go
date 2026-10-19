package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/requirement"
)

// IntakeMaterial is the complete normalized content shown for confirmation.
type IntakeMaterial = protocol.IntakeMaterial

// IntakePreview prepares bindings without confirming them or creating a run.
type IntakePreview struct {
	Content             string                                   `json:"content"`
	Revision            string                                   `json:"revision"`
	RequirementDocument *requirement.FrozenRequirementProjection `json:"requirementDocument,omitempty"`
	Materials           []IntakeMaterial                         `json:"materials"`
	IntakeCandidate     IntakeConfirmationReceipt                `json:"intakeCandidate"`
}

// PrepareIntake uses selected files and the existing receipt contract. A
// candidate becomes confirmation evidence only after the user confirms the
// displayed complete material and revisions; Start still rechecks every file.
func PrepareIntake(root, requirementPath, solutionPath string, artifacts []string, structured bool) (IntakePreview, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return IntakePreview{}, err
	}
	relative := func(path string) (string, error) {
		if strings.TrimSpace(path) == "" {
			return "", fmt.Errorf("intake material path is required")
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		path, err := filepath.Rel(root, filepath.Clean(path))
		if err != nil || path == "." || !filepath.IsLocal(path) {
			return "", fmt.Errorf("intake material must be a file within the project: %s", path)
		}
		return filepath.ToSlash(path), nil
	}
	requirementPath, err = relative(requirementPath)
	if err != nil {
		return IntakePreview{}, err
	}
	if solutionPath == "" {
		solutionPath = requirementPath
	}
	solutionPath, err = relative(solutionPath)
	if err != nil {
		return IntakePreview{}, err
	}
	paths := []string{requirementPath, solutionPath}
	for _, artifact := range artifacts {
		path, err := relative(artifact)
		if err != nil {
			return IntakePreview{}, err
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	preview := IntakePreview{IntakeCandidate: IntakeConfirmationReceipt{Source: DefaultIntakeSource, Authority: DefaultIntakeAuthority, Transport: DefaultIntakeTransport, RequirementSource: requirementPath}}
	if solutionPath != requirementPath {
		preview.IntakeCandidate.SolutionSource = solutionPath
	}
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return IntakePreview{}, err
		}
		content := strings.ReplaceAll(string(data), "\r\n", "\n")
		revision := requirementContentRevision(data)
		preview.Materials = append(preview.Materials, IntakeMaterial{Path: path, Content: content, Revision: revision})
		preview.IntakeCandidate.Artifacts = append(preview.IntakeCandidate.Artifacts, IntakeArtifact{Path: path, Revision: revision})
		if path == requirementPath {
			preview.Content, preview.Revision = content, revision
			preview.IntakeCandidate.RequirementRevision = revision
			document, err := requirement.ParseRequirementProjection(path, data)
			if err == nil {
				preview.RequirementDocument = &document
			} else if structured {
				return IntakePreview{}, err
			}
		}
		if path == solutionPath {
			preview.IntakeCandidate.SolutionRevision, preview.IntakeCandidate.SolutionDigest = revision, "sha256:"+revision
		}
	}
	preview.IntakeCandidate.Digest, err = IntakeDigest(preview.IntakeCandidate)
	return preview, err
}

// CheckIntakeCandidate revalidates a saved confirmation candidate's pinned
// artifact bindings and reloads its materials for preview. It records no
// binding, intake, or run.
func CheckIntakeCandidate(root string, receipt IntakeConfirmationReceipt, structured bool) (IntakePreview, error) {
	if err := validateArtifactBindings(root, receipt); err != nil {
		return IntakePreview{}, err
	}
	return prepareConfirmedIntake(root, &receipt, structured)
}

// prepareConfirmedIntake reuses the selected paths of the current confirmation.
func prepareConfirmedIntake(root string, receipt *IntakeConfirmationReceipt, structured bool) (IntakePreview, error) {
	paths := make([]string, len(receipt.Artifacts))
	for i, artifact := range receipt.Artifacts {
		paths[i] = artifact.Path
	}
	return PrepareIntake(root, receipt.RequirementSource, receipt.SolutionSource, paths, structured)
}

// MaterialDelta is the complete ordered difference for one confirmed material.
type MaterialDelta struct {
	Path              string                 `json:"path"`
	BaseRevision      string                 `json:"baseRevision"`
	CandidateRevision string                 `json:"candidateRevision"`
	Hunks             []requirement.TextHunk `json:"hunks"`
}

// captureIntake retains full original materials under their existing receipt.
func captureIntake(root string, receipt *IntakeConfirmationReceipt) (IntakePreview, error) {
	preview, err := prepareConfirmedIntake(root, receipt, false)
	if err != nil {
		return preview, err
	}
	if !slices.Equal(preview.IntakeCandidate.Artifacts, receipt.Artifacts) {
		return preview, fmt.Errorf("confirmed material bytes differ from their original receipt")
	}
	preview.IntakeCandidate = *receipt
	return preview, nil
}

func saveIntake(directory string, preview IntakePreview) error {
	_, err := SaveOriginalCandidate(filepath.Join(directory, "payloads", "intake"), preview)
	return err
}

// confirmedIntake selects evidence using only the current authoritative receipt.
func (f *Facade) confirmedIntake(run Run) (IntakePreview, error) {
	directory := f.stateDir
	if run.MasterRunID != "" {
		directory = filepath.Join(f.root, EngineNamespace, run.MasterRunID)
	}
	paths, err := OriginalPayloadPaths(filepath.Join(directory, "payloads", "intake"))
	if err != nil {
		return IntakePreview{}, err
	}
	digest, err := IntakeDigest(*run.IntakeConfirmationReceipt)
	if err != nil {
		return IntakePreview{}, err
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return IntakePreview{}, err
		}
		var preview IntakePreview
		if err := json.Unmarshal(body, &preview); err != nil {
			return IntakePreview{}, err
		}
		original, err := IntakeDigest(preview.IntakeCandidate)
		if err != nil {
			return IntakePreview{}, err
		}
		if original == digest {
			return preview, nil
		}
	}
	return IntakePreview{}, fmt.Errorf("complete confirmed intake materials are unavailable for %s", run.IntakeConfirmationReceipt.RequirementRevision)
}

func materialDelta(base, candidate IntakePreview) ([]MaterialDelta, string) {
	var changes []MaterialDelta
	for i, material := range candidate.Materials {
		original := base.Materials[i]
		hunks := requirement.TextDelta(original.Content, material.Content)
		if len(hunks) != 0 {
			changes = append(changes, MaterialDelta{Path: material.Path, BaseRevision: original.Revision, CandidateRevision: material.Revision, Hunks: hunks})
		}
	}
	digest := controlDigest(struct {
		Base, Candidate IntakeConfirmationReceipt
		Changes         []MaterialDelta
	}{base.IntakeCandidate, candidate.IntakeCandidate, changes})
	return changes, digest
}
