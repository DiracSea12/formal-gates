package contracttests

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	currentDefinition "formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/testkit"
)

// The raw diagnose path must preserve unsupported and malformed inputs while
// reporting the bytes it can safely understand. It never invokes the normal
// versioned loader or creates recovery artifacts.
func TestEngineRawDiagnoseIsReadOnlyForUnsupportedInputs(t *testing.T) {
	supported := persistence.Envelope{
		Writer:                    persistence.Writer,
		StateSchemaVersion:        encoder.StateSchemaVersion,
		WorkflowDefinitionVersion: currentDefinition.WorkflowDefinitionVersion,
		DefinitionSource:          facade.DefaultDefinitionSource,
		DefinitionDigest:          currentDefinition.WorkflowDefinitionDigest,
		PackageDigest:             testkit.HarnessPackageDigest,
		InstalledTargetIdentity:   "diagnostic-fixture",
	}
	fixtures := []struct {
		name    string
		content string
		assert  func(*testing.T, facade.DiagnosticReport)
	}{
		{
			name:    "legacy summary without envelope",
			content: "{\n  \"status\": \"SEALED\",\n  \"runId\": \"legacy-run\"\n}\n",
			assert: func(t *testing.T, report facade.DiagnosticReport) {
				if !report.JSONReadable || report.Integrity != "unsupported" ||
					report.Summary["status"] != "SEALED" || report.Summary["runId"] != "legacy-run" ||
					len(report.DetectedVersions) != 0 ||
					report.Recommendation != persistence.UnsupportedRunVersionCode+": state has no version envelope; rebuild it with the owning writer" {
					t.Fatalf("legacy diagnose report = %+v", report)
				}
			},
		},
		{
			name: "mismatched engine envelope",
			content: "{\n" +
				"  \"writer\": \"engine\",\n" +
				"  \"stateSchemaVersion\": \"" + encoder.StateSchemaVersion + "\",\n" +
				"  \"workflowDefinitionVersion\": \"old\",\n" +
				"  \"definitionSource\": \"definitions/workflow.json\",\n" +
				"  \"definitionDigest\": \"sha256:old\",\n" +
				"  \"packageDigest\": \"sha256:old\",\n" +
				"  \"status\": \"ACTIVE\"\n" +
				"}\n",
			assert: func(t *testing.T, report facade.DiagnosticReport) {
				wantDetected := map[string]any{
					"writer":                    "engine",
					"stateSchemaVersion":        encoder.StateSchemaVersion,
					"workflowDefinitionVersion": "old",
					"definitionSource":          "definitions/workflow.json",
					"definitionDigest":          "sha256:old",
					"packageDigest":             "sha256:old",
				}
				if !report.JSONReadable || report.Integrity != "unsupported" || !reflect.DeepEqual(report.DetectedVersions, wantDetected) ||
					report.Summary["status"] != "ACTIVE" ||
					report.Recommendation != (&persistence.UnsupportedRunVersionError{
						Field: "workflowDefinitionVersion", Expected: supported.WorkflowDefinitionVersion, Observed: "old",
					}).Error()+"; rebuild it with the owning writer" {
					t.Fatalf("mismatched diagnose report = %+v", report)
				}
			},
		},
		{
			name:    "malformed JSON",
			content: "{\"writer\":\"engine\"",
			assert: func(t *testing.T, report facade.DiagnosticReport) {
				if report.JSONReadable || report.Integrity != "unknown" || report.DetectedVersions != nil || report.Summary != nil ||
					report.Recommendation != "rebuild the state with the owning writer" {
					t.Fatalf("malformed diagnose report = %+v", report)
				}
			},
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "state.json")
			if err := os.WriteFile(path, []byte(fixture.content), 0o640); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			beforeInfo, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat fixture: %v", err)
			}
			report, err := facade.Diagnose(path, supported.PackageDigest, supported.InstalledTargetIdentity)
			if err != nil {
				t.Fatalf("DiagnoseStateWithSupported: %v", err)
			}
			if report.Path != filepath.Clean(path) || report.Supported != supported {
				t.Fatalf("diagnose identity = path %q supported %+v", report.Path, report.Supported)
			}
			fixture.assert(t, report)
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read diagnosed fixture: %v", err)
			}
			afterInfo, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat diagnosed fixture: %v", err)
			}
			if !bytes.Equal(before, after) || beforeInfo.Mode() != afterInfo.Mode() ||
				beforeInfo.Size() != afterInfo.Size() || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
				t.Fatalf("raw diagnose changed its input: before=%+v after=%+v", beforeInfo, afterInfo)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("read diagnose directory: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != "state.json" {
				t.Fatalf("raw diagnose created artifacts: %v", entries)
			}
		})
	}
}
