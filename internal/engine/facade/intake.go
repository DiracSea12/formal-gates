package facade

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"formal-gates/internal/requirement"
)

// IntakeMaterial is the complete normalized content shown for confirmation.
type IntakeMaterial struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

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
			if structured {
				document, err := requirement.ParseRequirementProjection(path, data)
				if err != nil {
					return IntakePreview{}, err
				}
				preview.RequirementDocument = &document
			}
		}
		if path == solutionPath {
			preview.IntakeCandidate.SolutionRevision, preview.IntakeCandidate.SolutionDigest = revision, "sha256:"+revision
		}
	}
	preview.IntakeCandidate.Digest, err = IntakeDigest(preview.IntakeCandidate)
	return preview, err
}
