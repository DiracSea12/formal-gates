package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PortableCanaryOptions struct{ Root string }
type CanaryCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}
type PortableCanaryReport struct {
	SchemaVersion int           `json:"schemaVersion"`
	Root          string        `json:"root"`
	Checks        []CanaryCheck `json:"checks"`
}

func PortableCanary(options PortableCanaryOptions) (PortableCanaryReport, Result) {
	root := cleanRoot(options.Root)
	report := PortableCanaryReport{SchemaVersion: 1, Root: slash(absPath(root))}
	var result Result
	add := func(name string, err error) {
		status, detail := "PASS", ""
		if err != nil {
			status, detail = "FAIL", err.Error()
			result.add(name, detail)
		}
		report.Checks = append(report.Checks, CanaryCheck{Name: name, Status: status, Detail: detail})
	}
	packageResult := Package(root)
	if packageResult.OK() {
		add("package-validate", nil)
	} else {
		add("package-validate", fmt.Errorf("%s", resultSummary(packageResult)))
	}
	catalog, err := LoadPromptCatalog(root)
	add("prompt-catalog", err)
	behavior, behaviorResult := Behavior(BehaviorOptions{Root: root})
	if behaviorResult.OK() && behavior.Summary.Total > 0 {
		add("behavior-harness", nil)
	} else {
		add("behavior-harness", fmt.Errorf("%s", resultSummary(behaviorResult)))
	}
	decision, err := Hook([]byte(`{"command":"pwsh -File ./scripts/gate-workflow.ps1"}`))
	if err == nil && decision.PermissionDecision != "deny" {
		err = fmt.Errorf("legacy command was allowed")
	}
	add("hook-blocks-legacy-command", err)
	if len(catalog.Gates) > 0 {
		add("lightweight-workflow", runLightweightCanary(root, catalog))
	}
	tempRoot, err := os.MkdirTemp("", "formal-gates-install-canary-")
	if err != nil {
		add("install", err)
	} else {
		defer os.RemoveAll(tempRoot)
		addInstallChecks(root, tempRoot, func(name string, ok bool, detail string) {
			if ok {
				add(name, nil)
			} else {
				add(name, fmt.Errorf("%s", detail))
			}
		})
	}
	return report, result
}

func runLightweightCanary(packageRoot string, catalog PromptCatalog) error {
	root, err := os.MkdirTemp("", "formal-gates-workflow-canary-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	requirement := filepath.Join(root, "requirement.md")
	if err := os.WriteFile(requirement, []byte("confirmed behavior\n"), 0o600); err != nil {
		return err
	}
	state, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, RunID: "canary", Flow: "formal", RequirementSource: "requirement.md", RequirementConfirmed: true, VCS: "git", BaseSnapshot: "base", CurrentSnapshot: "current"})
	if err != nil {
		return err
	}
	if _, err := PrepareGate(root, packageRoot, state.RunID, catalog.Gates[0].ID, "current"); err != nil {
		return err
	}
	if _, err := RecordAction(root, packageRoot, state.RunID, "start-readiness", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision); err != nil {
		return err
	}
	if _, err := RecordQADesign(root, packageRoot, state.RunID, []QACaseInput{{Description: "confirmed behavior", Procedure: "exercise it", Oracle: "it is observed"}}, "", state.RequirementRevision, state.CatalogRevision); err != nil {
		return err
	}
	if _, err := RecordQAExecution(root, packageRoot, state.RunID, []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "exercised it", Observation: "observed", OracleResult: "matched"}}, "", state.RequirementRevision, state.CatalogRevision, "current", "current"); err != nil {
		return err
	}
	for _, gate := range catalog.Gates {
		if _, err := RecordGate(root, packageRoot, state.RunID, gate.ID, "PASS", "", nil, state.RequirementRevision, state.CatalogRevision, "current", "current"); err != nil {
			return err
		}
	}
	_, err = Seal(root, packageRoot, state.RunID, "current", "current")
	return err
}

func PortableCanaryJSON(report PortableCanaryReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func addInstallChecks(root, tempRoot string, addCheck func(string, bool, string)) {
	for _, tc := range []struct{ name, host string }{{"install-claude-codex-native-runtime", "both"}, {"install-cursor-native-runtime", "cursor"}} {
		project := filepath.Join(tempRoot, tc.name)
		if err := os.MkdirAll(project, 0o700); err != nil {
			addCheck(tc.name, false, err.Error())
			continue
		}
		report, err := Install(InstallOptions{Source: root, Host: tc.host, Scope: "project", Project: project, Force: true})
		if err != nil {
			addCheck(tc.name, false, err.Error())
			continue
		}
		if detail := installedScriptRuntimeDetail(report); detail != "" {
			addCheck(tc.name, false, detail)
			continue
		}
		addCheck(tc.name, true, "installed runtime uses native commands")
	}
}

func installedScriptRuntimeDetail(report InstallReport) string {
	for _, target := range report.Targets {
		var found []string
		err := filepath.WalkDir(target.TargetPath, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if isScriptRuntimeExtension(entry.Name()) {
				found = append(found, slash(path))
			}
			return nil
		})
		if err != nil {
			return err.Error()
		}
		if len(found) > 0 {
			return "installed script runtime files: " + strings.Join(found, ", ")
		}
		if strings.TrimSpace(target.HookConfig) != "" {
			text, err := readText(target.HookConfig)
			if err != nil {
				return err.Error()
			}
			lower := strings.ToLower(text)
			for _, marker := range []string{".ps1", "powershell", "pwsh", "python", "node", "bash"} {
				if strings.Contains(lower, marker) {
					return "hook config contains script runtime marker " + marker
				}
			}
		}
	}
	return ""
}
func isScriptRuntimeExtension(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ps1", ".psm1", ".psd1", ".py", ".pyc", ".pyo", ".sh", ".bash", ".bat", ".cmd", ".js", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}
func resultSummary(result Result) string {
	messages := make([]string, 0, len(result.Failures))
	for _, failure := range result.Failures {
		messages = append(messages, failure.Path+": "+failure.Message)
	}
	return strings.Join(messages, "; ")
}
