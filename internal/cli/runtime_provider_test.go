package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/host"
)

func TestRegularInstalledDSHUsesCanonicalProvider(t *testing.T) {
	root, _ := cliWorkflowFixture(t)
	binary, _ := installHostCLI(t, host.DeepSeek, root)
	mustWriteCLI(t, filepath.Join(root, ".gitignore"), ".gates/\n.dsh/\n.formal-gates-resources/\n")
	cliGit(t, root, "add", ".gitignore", "AGENTS.md")
	cliGit(t, root, "commit", "-m", "isolated DSH intake fixture")
	const runID = "dsh-canonical-intake"
	var started facade.Run
	output := runInstalledCLI(t, binary, root, nil, "", "workflow", "start", "--root", root, "--run-id", runID, "--requirement", "requirements.md")
	if err := json.Unmarshal([]byte(output), &started); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, runID, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request facade.StartRequest
	if err := json.Unmarshal(raw, &request); err != nil || request.Provider != host.DeepSeek {
		t.Fatalf("installation alias leaked into authoritative provider: %+v %v", request, err)
	}
	var reopened facade.Run
	output = runInstalledCLI(t, binary, root, nil, "", "workflow", "show", "--root", root, "--run-id", runID)
	if err := json.Unmarshal([]byte(output), &reopened); err != nil || reopened.Revision != started.Revision || len(reopened.Executions) != 0 {
		t.Fatalf("read-only reopen changed state or invoked native work: %+v %v", reopened, err)
	}
}
