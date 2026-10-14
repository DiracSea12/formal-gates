package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
)

func materialEditChecks(material *worker.Material) []protocol.DevelopmentCheck {
	if material == nil {
		return nil
	}
	input := material.Input
	if input.Handler != "engine.development.batch.v5" && input.Handler != "engine.repair.v6" {
		return nil
	}
	var checks []protocol.DevelopmentCheck
	if input.Validation != nil && input.Validation.Development != nil {
		checks = input.Validation.Development.Batch.Checks
	} else if input.Handler == "engine.repair.v6" {
		checks = input.ApprovedChecks
	}
	var edits []protocol.DevelopmentCheck
	for _, check := range checks {
		if check.Purpose == protocol.CheckEdit {
			edits = append(edits, check)
		}
	}
	return edits
}

// edit records the original authorized mutation before its local write. Its
// unfinished deterministic checks remain discoverable in the original job.
func (t *nativeWorkspaceTools) edit(raw json.RawMessage, mutate func() (any, error)) (any, error) {
	if len(materialEditChecks(t.material)) == 0 {
		return mutate()
	}
	number, err := t.publish(nativeToolFact{Kind: "edit", Content: raw})
	if err != nil {
		return nil, err
	}
	result, mutationErr := mutate()
	if mutationErr != nil {
		if _, err := t.completeEditChecks(number, mutationErr.Error()); err != nil {
			return nil, err
		}
		return nil, mutationErr
	}
	checks, err := t.completeEditChecks(number, "")
	if err != nil {
		return nil, err
	}
	value := result.(map[string]any)
	value["checks"] = checks
	return value, nil
}

// workspaceDigest uses the native provider's complete current source facts.
func (t *nativeWorkspaceTools) workspaceDigest() (string, error) {
	if t.material == nil || t.material.Input.Validation == nil || t.material.Input.Validation.Workspace == nil {
		return "", fmt.Errorf("edit check requires its original registered workspace")
	}
	resource := t.material.Input.Validation.Workspace
	return vcs.NewNative(resource.Path, t.material.Input.VCS, nil).At(resource.Path, resource.Handle).WithRuntimeFiles(t.runtimeFiles).WorkingTreeDigest(resource.Source)
}

func editFactChecks(fact nativeToolFact) ([]protocol.DevelopmentCheck, map[string]any, error) {
	request, err := readJSONFile(filepath.Join(fact.Job, "native-request.json"))
	if err != nil {
		return nil, nil, err
	}
	path := textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "path"))
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var material worker.Material
	if err := json.Unmarshal(body, &material); err != nil {
		return nil, nil, err
	}
	return materialEditChecks(&material), objectOrEmpty(request), nil
}

func editCheckRequest(fact nativeToolFact, check protocol.DevelopmentCheck, original map[string]any) (map[string]any, error) {
	request := map[string]any{"kind": "approved-development-check", "argv": coverage.ShellCommand(check.Command), "cwd": fact.Workspace, "stdin": "", "environment": map[string]any{"FORMAL_GATES_WORKSPACE": fact.Workspace}, "engine": original["engine"], "check": check, "edit_request": json.RawMessage(fact.Content), "native_request_digest": fact.RequestDigest}
	body, err := canonicalBody(request)
	if err != nil {
		return nil, err
	}
	value, err := decodeNativeJSON(body)
	return objectOrEmpty(value), err
}

func editCheckDirectory(fact nativeToolFact, number, check int) string {
	return filepath.Join(fact.Job, "edit-checks", strconv.Itoa(number), strconv.Itoa(check+1))
}

func editCheckReceipt(directory string, request map[string]any) (map[string]any, error) {
	value, err := readJSONFile(filepath.Join(directory, "exit.json"))
	receipt := objectOrEmpty(value)
	if err != nil {
		return nil, err
	}
	streams := objectOrEmpty(receipt["streams"])
	proofs := []Proof{}
	for _, name := range []string{"stdout.log", "stderr.log"} {
		proofs = append(proofs, Proof{Name: name, Expected: objectOrEmpty(streams[name])})
	}
	if _, refusal := VerifyProcess(directory, receipt, receipt, request, proofs); refusal != "" {
		return nil, fmt.Errorf("%s", refusal)
	}
	return receipt, nil
}

// completeEditChecks observes receipts first and executes only unclaimed original
// requests. Live owners finish the original request; dead owners without a
// receipt retain an unknown effect and cannot grant another execution.
func (t *nativeWorkspaceTools) completeEditChecks(number int, mutationError string) ([]any, error) {
	fact, err := t.fact(number, "edit")
	if err != nil {
		return nil, err
	}
	checks, original, err := editFactChecks(fact)
	if err != nil {
		return nil, err
	}
	results := []any{}
	for index, check := range checks {
		if err := t.enterExecutionWindow(); err != nil {
			return nil, err
		}
		directory := editCheckDirectory(fact, number, index)
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, err
		}
		value, err := readJSONFile(filepath.Join(directory, "request.json"))
		request := objectOrEmpty(value)
		if os.IsNotExist(err) {
			source, err := t.workspaceDigest()
			if err != nil {
				return nil, err
			}
			request, err = editCheckRequest(fact, check, original)
			if err != nil {
				return nil, err
			}
			request["source_digest"] = source
			environment, err := executionEnvironmentDigest(request)
			if err != nil {
				return nil, err
			}
			request["environment_digest"] = environment
			if err := writeAtomicJSON(filepath.Join(directory, "request.json"), request); err != nil {
				return nil, err
			}
			value, err := readJSONFile(filepath.Join(directory, "request.json"))
			if err != nil {
				return nil, err
			}
			request = objectOrEmpty(value)
		} else if err != nil {
			return nil, err
		}
		expected, err := editCheckRequest(fact, check, original)
		if err != nil {
			return nil, err
		}
		for field, value := range expected {
			if !valuesEqual(request[field], value) {
				return nil, fmt.Errorf("edit check %s differs from its original issued mutation", field)
			}
		}
		receipt, err := editCheckReceipt(directory, request)
		if os.IsNotExist(err) {
			if _, ownerErr := os.Stat(filepath.Join(directory, "owner")); ownerErr == nil {
				t.leaveExecutionWindow()
				for supervisorAlive(directory) {
					receipt, err = editCheckReceipt(directory, request)
					if !os.IsNotExist(err) {
						break
					}
					time.Sleep(20 * time.Millisecond)
				}
				if os.IsNotExist(err) {
					receipt, err = editCheckReceipt(directory, request)
				}
				if err != nil {
					return nil, fmt.Errorf("original edit check owner stopped without completion evidence: %w", err)
				}
				result, err := editCheckResult(index, directory, receipt)
				if err != nil {
					return nil, err
				}
				results = append(results, result)
				continue
			} else if !os.IsNotExist(ownerErr) {
				return nil, ownerErr
			}
			source, err := t.workspaceDigest()
			if err != nil {
				return nil, err
			}
			environment, err := executionEnvironmentDigest(request)
			if err != nil {
				return nil, err
			}
			if source != textOf(request["source_digest"]) || environment != textOf(request["environment_digest"]) {
				return nil, fmt.Errorf("unfinished edit check inputs differ from the original request")
			}
			if mutationError != "" {
				ensureEmptyStream(filepath.Join(directory, "stdout.log"))
				ensureEmptyStream(filepath.Join(directory, "stderr.log"))
				digest, err := CanonicalDigest(request)
				if err != nil {
					return nil, err
				}
				stdout, _ := fileProofOf(filepath.Join(directory, "stdout.log"))
				stderr, _ := fileProofOf(filepath.Join(directory, "stderr.log"))
				receipt = map[string]any{"exit_code": 127, "error": mutationError, "request_digest": digest, "streams": map[string]any{"stdout.log": stdout, "stderr.log": stderr}}
				if err := writeAtomicJSON(filepath.Join(directory, "exit.json"), receipt); err != nil {
					return nil, err
				}
			} else {
				if err := claimJob(directory, request); err != nil {
					return nil, err
				}
				t.leaveExecutionWindow()
				receipt, err = executeRequest(directory, request)
				if err != nil {
					return nil, err
				}
			}
		} else if err != nil {
			return nil, err
		}
		result, err := editCheckResult(index, directory, receipt)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func editCheckResult(index int, directory string, receipt map[string]any) (map[string]any, error) {
	stdout, err := os.ReadFile(filepath.Join(directory, "stdout.log"))
	if err != nil {
		return nil, err
	}
	stderr, err := os.ReadFile(filepath.Join(directory, "stderr.log"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"check": index + 1, "exitCode": receipt["exit_code"], "error": receipt["error"], "stdout": string(stdout), "stderr": string(stderr)}, nil
}

func verifyEditCheckFacts(facts []nativeToolFact) error {
	for number, fact := range facts {
		if fact.Kind != "edit" {
			continue
		}
		checks, original, err := editFactChecks(fact)
		if err != nil {
			return err
		}
		for index, check := range checks {
			directory := editCheckDirectory(fact, number+1, index)
			value, err := readJSONFile(filepath.Join(directory, "request.json"))
			if err != nil {
				return err
			}
			request := objectOrEmpty(value)
			expected, err := editCheckRequest(fact, check, original)
			if err != nil {
				return err
			}
			for field, value := range expected {
				if !valuesEqual(request[field], value) {
					return fmt.Errorf("edit check %s differs from its original issued mutation", field)
				}
			}
			if textOf(request["source_digest"]) == "" || textOf(request["environment_digest"]) == "" {
				return fmt.Errorf("edit check lacks original workspace/environment inputs")
			}
			receipt, err := editCheckReceipt(directory, request)
			if err != nil {
				return err
			}
			if getOf(receipt, "environment_identity") != nil && !valuesEqual(receipt["environment_identity"], request["environment_digest"]) {
				return fmt.Errorf("edit check environment differs from its actual execution")
			}
		}
	}
	return nil
}
