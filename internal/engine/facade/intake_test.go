package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
)

func TestPreparedIntakeBindsSelectedMaterialAndStartsThroughOriginalAdmission(t *testing.T) {
	for _, changed := range []string{"", "requirements.md", "design.md", "reference.md"} {
		t.Run("changed="+changed, func(t *testing.T) {
			root := t.TempDir()
			contents := map[string]string{"requirements.md": strings.ReplaceAll(validationDocument, "\n", "\r\n"), "design.md": "# Plan\r\n\r\nUse the approved entry.\r\n", "reference.md": "# Reference\n\nExact selected evidence.\n"}
			for path, content := range contents {
				if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			preview, err := PrepareIntake(root, filepath.Join(root, "requirements.md"), "./design.md", []string{"reference.md", "./reference.md"}, true)
			if err != nil {
				t.Fatal(err)
			}
			receipt := preview.IntakeCandidate
			if receipt.Source != DefaultIntakeSource || receipt.Authority != DefaultIntakeAuthority || receipt.Transport != DefaultIntakeTransport || receipt.RequirementSource != "requirements.md" || len(preview.Materials) != 3 || len(receipt.Artifacts) != 3 {
				t.Fatalf("prepared identity and selection: %+v", preview)
			}
			for i, material := range preview.Materials {
				normalized := strings.ReplaceAll(contents[material.Path], "\r\n", "\n")
				sum := sha256.Sum256([]byte(normalized))
				expected := hex.EncodeToString(sum[:])
				if material.Content != normalized || material.Revision != expected || receipt.Artifacts[i] != (IntakeArtifact{Path: material.Path, Revision: expected}) {
					t.Fatalf("material bytes or binding differ: %+v", material)
				}
			}
			if preview.RequirementDocument == nil || preview.Content != preview.RequirementDocument.Content || preview.Revision != preview.RequirementDocument.ContentDigest || receipt.RequirementRevision != preview.Revision {
				t.Fatal("precheck and candidate do not share the full confirmed requirement")
			}
			if receipt.SolutionRevision != preview.Materials[0].Revision || receipt.SolutionDigest != "sha256:"+preview.Materials[0].Revision {
				t.Fatal("solution selection was not bound to design.md")
			}
			digest, err := IntakeDigest(receipt)
			if err != nil || digest != receipt.Digest {
				t.Fatal("candidate digest differs from the existing canonical owner", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
				t.Fatal("unconfirmed preview created run or receipt state")
			}
			if changed != "" {
				if err := os.WriteFile(filepath.Join(root, changed), []byte(contents[changed]+"changed\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// This fixture supplies the displayed material's explicit user confirmation.
			_, run, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{RunID: "prepared-intake", Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: receipt}, Admission: testAdmission(t, root, "intake-runtime", "sha256:package", DefaultProvider)})
			if changed != "" {
				if err == nil || !strings.Contains(err.Error(), InvalidIntakeConfirmation) || !strings.Contains(err.Error(), "artifact "+changed+" revision mismatch") {
					t.Fatalf("original file verification did not reject changed material: %v", err)
				}
				if _, err := os.Stat(filepath.Join(root, EngineNamespace)); !os.IsNotExist(err) {
					t.Fatal("changed material reached bootstrap")
				}
				return
			}
			if err != nil || run.Status != "ACTIVE" || !reflect.DeepEqual(run.IntakeConfirmationReceipt, &receipt) {
				t.Fatalf("prepared receipt did not enter original Start: %+v %v", run, err)
			}
			var request StartRequest
			body, err := os.ReadFile(filepath.Join(root, EngineNamespace, "prepared-intake", "request.json"))
			if err != nil || json.Unmarshal(body, &request) != nil || !reflect.DeepEqual(request.IntakeConfirmationReceipt, receipt) {
				t.Fatal("persisted request changed selected material bindings", err)
			}
		})
	}
}

func TestPreparedIntakeKeepsSingleFileDefaultAndLightweightMaterial(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "requirements.md")
	if err := os.WriteFile(path, []byte("# Confirmed request and solution\n"), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := PrepareIntake(root, "requirements.md", "", nil, false)
	if err != nil || len(preview.Materials) != 1 || preview.RequirementDocument != nil || preview.IntakeCandidate.SolutionRevision != preview.Revision || preview.IntakeCandidate.SolutionDigest != "sha256:"+preview.Revision {
		t.Fatalf("single-file lightweight preview: %+v %v", preview, err)
	}
	if _, err := PrepareIntake(root, "requirements.md", "", nil, true); err == nil {
		t.Fatal("regular precheck skipped REQ/AC structure")
	}
	if _, err := PrepareIntake(root, "../requirements.md", "", nil, false); err == nil {
		t.Fatal("selection escaped the project")
	}
}

func TestPreparedIntakeRegularStartBindsPrecheckedDocument(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	preview, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{RunID: "prepared-regular", Route: "regular", VCS: "git", BaseSnapshot: base, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: preview.IntakeCandidate}, Admission: testAdmission(t, root, "regular-intake-runtime", "sha256:package", DefaultProvider)})
	if err != nil || run.Status != "ACTIVE" || !reflect.DeepEqual(run.IntakeConfirmationReceipt, &preview.IntakeCandidate) {
		t.Fatalf("regular Start did not accept its prechecked candidate: %+v %v", run, err)
	}
}
