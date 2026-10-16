package harness

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCommandNativeRequiresDirectory(t *testing.T) {
	var errout bytes.Buffer
	code := RunCommand([]string{"native"}, strings.NewReader(""), nil, &errout)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if errout.String() != "harness native requires a job directory\n" {
		t.Fatalf("stderr = %q", errout.String())
	}
}

func settleNativeReceipt(jobDir string, receipt map[string]any) (map[string]any, error) {
	requestValue, err := readJSONFile(filepath.Join(jobDir, "request.json"))
	if err != nil {
		return nil, err
	}
	request, ok := requestValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("request.json of %s is not an object", jobDir)
	}
	if _, refusal := VerifyProcess(jobDir, receipt, receipt, request, receiptProofs(receipt)); refusal != "" {
		return nil, fmt.Errorf("%s: %s", jobDir, refusal)
	}
	if !valuesEqual(getOf(receipt, "exit_code"), 0) {
		detail := ""
		if body, err := readJSONFile(filepath.Join(jobDir, "native-error.json")); err == nil {
			detail = textOf(getOf(body, "error"))
		}
		if detail == "" {
			detail = "native job exited " + textOf(getOf(receipt, "exit_code"))
		}
		return nil, fmt.Errorf("%s: %s", jobDir, detail)
	}
	answerValue, err := readJSONFile(filepath.Join(jobDir, "answer.json"))
	if err != nil {
		return nil, fmt.Errorf("%s produced no framed answer: %w", jobDir, err)
	}
	answer, ok := answerValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("answer.json of %s is not an object", jobDir)
	}
	return answer, nil
}

func receiptProofs(receipt map[string]any) []Proof {
	streams := objectOrEmpty(getOf(receipt, "streams"))
	proofs := make([]Proof, 0, len(streams))
	for name, bound := range streams {
		proofs = append(proofs, Proof{Name: name, Expected: objectOrEmpty(bound)})
	}
	return proofs
}

func TestRunCommandRejectsUnsupportedCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"start"}, {"resume"}, {"demo"}, {"setup"}, {"doctor"}, {"hosts"}, {"archive"}, {"prune"}} {
		var out, errout bytes.Buffer
		if code := RunCommand(args, strings.NewReader(""), &out, &errout); code != 2 || out.Len() != 0 || errout.String() != "unknown native command\n" {
			t.Fatalf("%v: %d %q %q", args, code, out.String(), errout.String())
		}
	}
}
