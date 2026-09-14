package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"formal-gates/internal/engine/protocol"
)

// New result rows bind to the actual process files; retained checkpoint rows do
// not require their temporary execution workspace to remain alive forever.
func (f *Facade) bindVerificationEvidence(event protocol.Event) error {
	if event.Regular == nil || event.Regular.VerificationResult == nil {
		return nil
	}
	result := event.Regular.VerificationResult
	run, err := f.Show()
	if err != nil {
		return err
	}
	x, ok := run.Executions[result.ActionID]
	if !ok {
		return nil
	} // protocol owns admission
	var old []protocol.CaseExecution
	if run.Regular != nil && run.Regular.Verification != nil {
		for _, w := range run.Regular.Verification.Waves {
			if x.Claim.Task.Step == "repair" {
				checkpoint := w.Checkpoints["repair"]
				if checkpoint.BindingDigest == x.Claim.PrerequisiteDigest {
					old = checkpoint.Cases
				}
			}
			for _, target := range w.Targets {
				if target.Task == x.Claim.Task {
					old = w.Checkpoints[target.Target].Cases
				}
			}
		}
	}
	for i := range result.Cases {
		row := &result.Cases[i]
		retained := false
		for _, prior := range old {
			if reflect.DeepEqual(prior, *row) {
				retained = true
				break
			}
		}
		if retained || row.Status == "BLOCKED" {
			continue
		}
		target := latestTaskOperation(x, "qa.target", row.CaseID)
		collect := latestTaskOperation(x, "qa.collect", row.CaseID)
		if target == nil || collect == nil || target.Receipt == nil || collect.Receipt == nil {
			continue
		} // typed validation diagnoses missing operations
		if target.Receipt.Status != "COMPLETED" || collect.Receipt.Status != "COMPLETED" {
			continue
		}
		stdout := filepath.Join(f.taskOperationDir(target.ID), "stdout.log")
		stderr := filepath.Join(f.taskOperationDir(target.ID), "stderr.log")
		out, err := os.ReadFile(stdout)
		if err != nil {
			return fmt.Errorf("case %s stdout evidence: %w", row.CaseID, err)
		}
		errorOut, err := os.ReadFile(stderr)
		if err != nil {
			return fmt.Errorf("case %s stderr evidence: %w", row.CaseID, err)
		}
		if string(out) != collect.Receipt.Output || string(errorOut) != collect.Receipt.Stderr || collect.Receipt.ExitCode != target.Receipt.ExitCode {
			return fmt.Errorf("case %s collected evidence conflicts with target", row.CaseID)
		}
		if (row.OperationID != "" && row.OperationID != collect.ID) || (row.ExitCode != nil && *row.ExitCode != collect.Receipt.ExitCode) || (row.StdoutPath != "" && row.StdoutPath != stdout) || (row.StderrPath != "" && row.StderrPath != stderr) || (row.Command != nil && !reflect.DeepEqual(row.Command, target.Command)) {
			return fmt.Errorf("case %s result conflicts with actual command/streams/exit", row.CaseID)
		}
		row.OperationID = collect.ID
		code := collect.Receipt.ExitCode
		row.ExitCode = &code
		row.Command = target.Command
		row.StdoutPath = stdout
		row.StderrPath = stderr
		if row.Output == "" {
			data, _ := json.Marshal(map[string]any{"stdout": string(out), "stderr": string(errorOut), "exitCode": code})
			row.Output = string(data)
		}
	}
	return nil
}
