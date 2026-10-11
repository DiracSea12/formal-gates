package facade

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func readCheckDocument(path string, value any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	return decoder.Decode(value)
}

// checkEvidence observes the original owned job before allowing one launch.
// An owner without a verified live process or receipt is an unknown effect.
func (f *Facade) checkEvidence(s *protocol.State, intent protocol.HostActionIntent, mayWrite bool, invoked *bool) (nativeResult, bool, error) {
	var p protocol.CheckParameters
	if err := decodeNativeParams(intent, &p); err != nil {
		return nativeResult{}, false, err
	}
	directory := filepath.Join(f.stateDir, "checks", strings.ReplaceAll(intent.ActionID, ":", "-"))
	request, err := f.checkRequest(s, intent, p)
	if err != nil {
		return nativeResult{}, false, err
	}
	workspace := s.NativeResources[p.WorkspaceID]
	argv, cwd := request["argv"].([]string), request["cwd"].(string)
	testName := ""
	if p.QA != nil && p.QA.GoTestRef != "" {
		resolved, err := coverage.GoTestOperation(p.QA.GoTestRef)
		if err != nil {
			return nativeResult{}, false, err
		}
		testName = resolved.GoTestName
	}

	var recorded map[string]any
	requestErr := readCheckDocument(filepath.Join(directory, "request.json"), &recorded)
	if requestErr == nil && p.QA != nil && p.QA.Preparation != nil {
		request["qaPreparationInput"] = recorded["qaPreparationInput"]
	}
	digest, err := f.checkRequestDigest(request)
	if err != nil {
		return nativeResult{}, false, err
	}
	if requestErr == nil {
		actual, err := f.checkRequestDigest(recorded)
		if err != nil || actual != digest {
			return nativeResult{}, false, fmt.Errorf("check request differs from its approved inputs")
		}
	} else if !os.IsNotExist(requestErr) {
		return nativeResult{}, false, requestErr
	}
	var receipt map[string]any
	receiptErr := readCheckDocument(filepath.Join(directory, "exit.json"), &receipt)
	if receiptErr == nil {
		if requestErr != nil {
			return nativeResult{}, false, requestErr
		}
		streams, ok := receipt["streams"].(map[string]any)
		if !ok {
			return nativeResult{}, false, fmt.Errorf("check receipt lacks stdout/stderr proofs")
		}
		proofs := []map[string]any{}
		for _, name := range []string{"stdout.log", "stderr.log"} {
			proof, ok := streams[name].(map[string]any)
			if !ok {
				return nativeResult{}, false, fmt.Errorf("check receipt lacks %s", name)
			}
			proofs = append(proofs, map[string]any{"name": name, "expected": proof})
		}
		var verified struct {
			OK      bool   `json:"ok"`
			Refusal string `json:"refusal"`
		}
		if err := f.checkBridge("verify-process", map[string]any{"directory": directory, "process": receipt, "receipt": receipt, "request": recorded, "proofs": proofs}, &verified); err != nil {
			return nativeResult{}, false, err
		}
		if !verified.OK {
			return nativeResult{}, false, errors.New(verified.Refusal)
		}
		var stdout, stderr protocol.CheckStreamProof
		for i, target := range []*protocol.CheckStreamProof{&stdout, &stderr} {
			body, err := json.Marshal(proofs[i]["expected"])
			if err != nil {
				return nativeResult{}, false, err
			}
			if err := json.Unmarshal(body, target); err != nil {
				return nativeResult{}, false, err
			}
		}
		code, ok := receipt["exit_code"].(json.Number)
		if !ok {
			return nativeResult{}, false, fmt.Errorf("check receipt lacks an actual exit code")
		}
		exit, err := strconv.Atoi(string(code))
		if err != nil {
			return nativeResult{}, false, err
		}
		errorText, ok := receipt["error"].(string)
		if !ok {
			return nativeResult{}, false, fmt.Errorf("check receipt lacks an execution error")
		}
		tree, err := f.vcs.TreeDigest(p.SourceIdentity)
		if err != nil || tree != p.SourceDigest {
			return nativeResult{}, false, fmt.Errorf("check immutable source differs from its issued digest: %w", err)
		}
		native := f.vcs.At(workspace.Path, workspace.Handle)
		changes, err := native.ChangedPaths()
		if err != nil {
			return nativeResult{}, false, err
		}
		for _, path := range changes {
			entry, err := native.Entry(workspace.Identity, path)
			if err != nil {
				return nativeResult{}, false, err
			}
			if entry != "" {
				errorText = "check changed an immutable source input: " + path
				break
			}
		}
		evidence := &protocol.CheckEvidence{ParametersDigest: intent.PayloadDigest, SourceActionID: p.SourceActionID, SourceIdentity: p.SourceIdentity, SourceDigest: p.SourceDigest, WorkspaceID: workspace.ID, RequestDigest: digest, Directory: directory, ExitCode: exit, Error: errorText, Stdout: stdout, Stderr: stderr}
		evidence.Tool, _ = receipt["tool_identity"].(string)
		evidence.Environment, _ = receipt["environment_identity"].(string)
		invocation, err := json.Marshal(argv)
		if err != nil {
			return nativeResult{}, false, err
		}
		evidence.Invocation, evidence.WorkingDirectory = string(invocation), cwd
		if p.QA != nil {
			stdout, err := os.ReadFile(filepath.Join(directory, "stdout.log"))
			if err != nil {
				return nativeResult{}, false, err
			}
			stderr, err := os.ReadFile(filepath.Join(directory, "stderr.log"))
			if err != nil {
				return nativeResult{}, false, err
			}
			evidence.StdoutText, evidence.StderrText = string(stdout), string(stderr)
			evidence.GoTestName = testName
			if testName != "" {
				evidence.GoResult = coverage.GoTestExecutionResult(stdout, testName)
			}
		}
		if p.QA != nil && p.QA.Preparation != nil {
			evidence.PreparationInput, _ = request["qaPreparationInput"].(string)
			evidence.PreparationArtifacts, err = preparationArtifacts(p)
			if err != nil {
				evidence.Error = err.Error()
			}
		}
		return nativeResult{Check: evidence}, true, nil
	}
	if !os.IsNotExist(receiptErr) {
		return nativeResult{}, false, receiptErr
	}
	if _, err := os.Stat(filepath.Join(directory, "owner")); err == nil {
		var process struct {
			Alive bool `json:"alive"`
		}
		_, aborted := s.AbortDecision()
		observe := func() error {
			return f.checkBridge("supervisor-process", map[string]any{"directory": directory, "requestDigest": digest, "cancel": aborted || s.ResetPending()}, &process)
		}
		var err error
		if aborted || s.ResetPending() {
			err = f.admission.write(observe)
		} else {
			err = observe()
		}
		if err != nil {
			return nativeResult{observedEffect: true}, false, err
		}
		if !process.Alive {
			if _, err := os.Stat(filepath.Join(directory, "exit.json")); err == nil {
				return f.checkEvidence(s, intent, false, invoked)
			} else if !os.IsNotExist(err) {
				return nativeResult{}, false, err
			}
			return nativeResult{observedEffect: true}, false, fmt.Errorf("owned check process is unknown without an exit receipt")
		}

		return nativeResult{observedEffect: true}, false, nil
	} else if !os.IsNotExist(err) {
		return nativeResult{}, false, err
	}
	if !mayWrite {
		return nativeResult{}, false, nil
	}
	if p.QA != nil && p.QA.Preparation != nil && !p.QA.Preparation.Independent {
		input, _ := request["qaPreparationInput"].(string)
		unlock, err := coordination.AcquirePath(filepath.Join(f.stateDir, "checks", "preparation-"+strings.TrimPrefix(input, "sha256:")+".lock"), "QA preparation")
		if err != nil {
			var held *coordination.LockHeldError
			if errors.As(err, &held) {
				return nativeResult{}, false, nil
			}
			return nativeResult{}, false, err
		}
		defer unlock()
		if result, done, found, err := f.reusePreparation(s, intent, p, input); err != nil || found {
			return result, done, err
		}
	}
	if p.QA != nil && p.QA.GoTestRef != "" {
		if _, err := coverage.ResolveCaseOperations(workspace.Path, coverage.AcceptanceCase{CaseID: p.QA.CaseID, TestRef: p.QA.GoTestRef}); err != nil {
			*invoked = true
			return nativeResult{}, false, err
		}
	}
	err = f.admission.write(func() error {
		*invoked = true
		if p.QA != nil {
			for name, value := range request["environment"].(map[string]any) {
				if strings.HasPrefix(name, "FORMAL_GATES_QA_") || name == "TMPDIR" {
					if err := os.MkdirAll(value.(string), 0700); err != nil {
						return err
					}
				}
			}
		}
		if err := os.MkdirAll(directory, 0755); err != nil {
			return err
		}
		body, err := json.Marshal(request)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(directory, "request.json"), body, 0600); err != nil {
			return err
		}
		binary := filepath.Join(f.admission.Target, "bin", "formal-gates")
		cmd := exec.Command(binary, "harness", "supervise", directory)
		cmd.SysProcAttr = checkSupervisorAttributes()
		if err := cmd.Start(); err != nil {
			return err
		}
		go cmd.Wait()
		return nil
	})
	return nativeResult{}, false, err
}

// checkRequest owns the exact approved invocation for execution and admission.
func (f *Facade) checkRequest(s *protocol.State, intent protocol.HostActionIntent, p protocol.CheckParameters) (map[string]any, error) {
	workspace := s.NativeResources[p.WorkspaceID]
	if workspace.Status != "READY" || workspace.Owner != s.RunID || p.QA != nil && p.QA.WorkspaceRoot != workspace.Path {
		return nil, fmt.Errorf("check has no registered ready workspace")
	}
	argv := []string{"/bin/sh", "-c", p.Check.Command}
	cwd, stdin, kind := workspace.Path, "", "approved-development-check"
	if p.QA != nil {
		kind = "approved-qa-check"
		operation := p.QA.Operation
		if p.QA.GoTestRef != "" {
			resolved, err := coverage.GoTestOperation(p.QA.GoTestRef)
			if err != nil {
				return nil, err
			}
			operation, cwd = resolved.Operation, filepath.Join(workspace.Path, resolved.Directory)
		} else {
			cwd = filepath.Join(workspace.Path, p.QA.Directory)
		}
		stdin = operation.Stdin
		switch operation.Kind {
		case coverage.OperationArgv:
			argv = operation.Argv
		case coverage.OperationShell:
			argv = []string{"/bin/sh", "-c", operation.Command}
		default:
			return nil, fmt.Errorf("QA operation has no approved invocation")
		}
	}
	environment := map[string]any{"FORMAL_GATES_WORKSPACE": workspace.Path}
	if p.QA != nil && (p.QA.Preparation == nil || p.QA.Preparation.Independent) {
		base := filepath.Join(workspace.Path, ".gates", "tmp", "qa-cases", strings.TrimPrefix(preparationDigest(p.QA.CaseID), "sha256:"))
		for _, item := range []struct{ Name, Directory string }{{"FORMAL_GATES_QA_INPUT_DIR", "input"}, {"FORMAL_GATES_QA_WORK_DIR", "work"}, {"FORMAL_GATES_QA_OUTPUT_DIR", "output"}, {"TMPDIR", "temp"}, {"TMP", "temp"}, {"TEMP", "temp"}} {
			environment[item.Name] = filepath.Join(base, item.Directory)
		}
	}
	request := map[string]any{"kind": kind, "argv": argv, "cwd": cwd, "stdin": stdin, "environment": environment, "parameters_digest": intent.PayloadDigest, "engine": map[string]any{"root": f.root, "runId": f.runID, "actionId": intent.ActionID}}
	if p.QA != nil && p.QA.Preparation != nil {
		input, err := f.qaPreparationInput(s, p, request)
		if err != nil {
			return nil, err
		}
		request["qaPreparationInput"] = input
	}
	return request, nil
}

func (f *Facade) checkBridge(operation string, payload any, result any) error {
	body, err := json.Marshal(map[string]any{"operation": operation, "payload": payload})
	if err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(f.admission.Target, "bin", "formal-gates"), "harness", "bridge")
	cmd.Stdin = bytes.NewReader(body)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("check evidence verifier: %w: %s", err, stderr.String())
	}
	var response struct {
		OK    bool            `json:"ok"`
		Value json.RawMessage `json:"value"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return err
	}
	if !response.OK {
		return errors.New(response.Error)
	}
	return json.Unmarshal(response.Value, result)
}
func (f *Facade) checkRequestDigest(request map[string]any) (string, error) {
	var value struct {
		Digest string `json:"digest"`
	}
	err := f.checkBridge("digest", map[string]any{"value": request}, &value)
	return value.Digest, err
}
