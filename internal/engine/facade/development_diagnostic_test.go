package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func TestFailedDevelopmentBuildCarriesOriginalDiagnosticsToContinuation(t *testing.T) {
	const command = "go build app.go"
	f, state, base := developmentCheckFixture(t,
		protocol.DevelopmentCheck{ID: "build", Purpose: protocol.CheckFullBuild, Command: command},
		protocol.DevelopmentCheck{ID: "tests", Purpose: protocol.CheckFullTests, Command: "true"})
	retainNativeEvidence(t, f)
	intent := developmentCheckIntent(t, f, state, base, "build", command)
	evidence := observeDevelopmentCheck(t, f, state, intent, true)
	stdout, err := os.ReadFile(filepath.Join(evidence.Directory, "stdout.log"))
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.ReadFile(filepath.Join(evidence.Directory, "stderr.log"))
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ExitCode == 0 || evidence.Error != "" || len(stderr) == 0 {
		t.Fatalf("actual build did not produce compiler diagnostics: %+v", evidence)
	}
	if evidence.StdoutText != string(stdout) || evidence.StderrText != string(stderr) ||
		evidence.Stdout.SHA256 != fmt.Sprintf("%x", sha256.Sum256(stdout)) || evidence.Stdout.Size != len(stdout) ||
		evidence.Stderr.SHA256 != fmt.Sprintf("%x", sha256.Sum256(stderr)) || evidence.Stderr.Size != len(stderr) ||
		evidence.GoTestName != "" || evidence.GoResult != "" {
		t.Fatalf("development evidence differs from verified streams: %+v", evidence)
	}
	reopened := *f
	if actual := observeDevelopmentCheck(t, &reopened, state, intent, false); !reflect.DeepEqual(actual, evidence) {
		t.Fatalf("recovery changed original diagnostics: %+v", actual)
	}
	catalog, err := prompts.LoadPromptCatalog("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range []string{"engine.development.batch.v6", "engine.repair.v7"} {
		t.Run(handler, func(t *testing.T) {
			input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: handler, Provider: "host",
				Pending:             protocol.PendingAction{ActionID: evidence.SourceActionID, AttemptID: "original-attempt"},
				RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed requirement"},
				Validation:          &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Checks: []protocol.CheckEvidence{evidence}}}}
			material, err := worker.Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			actionInput := strings.Split(strings.Split(material.Prompt, "[Action input]\n")[1], "\n\n[Dispatch]")[0]
			var projection protocol.WorkerInput
			if err := json.Unmarshal([]byte(actionInput), &projection); err != nil {
				t.Fatal(err)
			}
			if projection.Validation == nil || projection.Validation.Development == nil || len(projection.Validation.Development.Checks) != 1 {
				t.Fatal("original failed check was omitted from continuation")
			}
			check := projection.Validation.Development.Checks[0]
			if check.StdoutText != string(stdout) || check.StderrText != string(stderr) ||
				check.ExitCode != evidence.ExitCode || check.Error != evidence.Error ||
				check.Stdout != evidence.Stdout || check.Stderr != evidence.Stderr ||
				check.Invocation != evidence.Invocation || check.Tool != evidence.Tool || check.Environment != evidence.Environment {
				t.Fatalf("continuation lost original failure evidence: %+v", check)
			}
			if !reflect.DeepEqual(material.Input.Validation.Development.Checks[0], evidence) {
				t.Fatal("render changed original admission/recovery evidence")
			}
			if check.Directory != "" || strings.Contains(material.Prompt, evidence.Directory) {
				t.Fatal("continuation needs original diagnostics without exposing private job paths")
			}
		})
	}
	t.Logf("original build exit=%d stdout=%q stderr=%q", evidence.ExitCode, stdout, stderr)
}
