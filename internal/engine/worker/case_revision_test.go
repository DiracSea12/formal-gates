package worker

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestCaseRevisionKeepsUntouchedContentAndFillsIssuedIDs(t *testing.T) {
	input := retainedDesignInput()
	input.Validation.AffectedCases = []string{"change", "keep"}
	before, _ := json.Marshal(input)
	raw := []byte(`{"cases":[{"oracle":"corrected success"},{"preconditions":"initialize the corrected fixture"}],"sharedSteps":[{"procedure":"prepare the corrected fixture"}]}`)
	result, err := DecodeResultWithPaths(input, "result", raw, "original-revision-output", []string{})
	if err != nil {
		t.Fatal(err)
	}
	manifest := result.Result.Validation.Manifest
	if len(manifest.Cases) != 3 || manifest.Cases[0].CaseID != "keep" || manifest.Cases[1].CaseID != "change" || manifest.Cases[2].CaseID != "delete" || manifest.Cases[0].Preconditions != "initialize the corrected fixture" || manifest.Cases[1].Oracle != "corrected success" || !reflect.DeepEqual(manifest.Cases[2], input.Validation.Manifest.Cases[2]) || !reflect.DeepEqual(manifest.Edges, input.Validation.Manifest.Edges) || !reflect.DeepEqual(manifest.SharedSteps[1], input.Validation.Manifest.SharedSteps[1]) || manifest.SharedSteps[0].Procedure != "prepare the corrected fixture" {
		t.Fatal("revision lost exact issued IDs/order, original untouched cases, shared content or coverage")
	}
	if !reflect.DeepEqual(manifest.Cases[0].Operations, input.Validation.Manifest.Cases[0].Operations) || !reflect.DeepEqual(manifest.Cases[0].Files, input.Validation.Manifest.Cases[0].Files) || result.Result.PayloadDigest != "original-revision-output" {
		t.Fatal("revision lost issued command bytes, files or original output evidence")
	}
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) {
		t.Fatal("revision mutated its original input")
	}
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	properties := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"retainCases", "retainSharedSteps", "coverage"} {
		if properties[field] != nil {
			t.Fatalf("specified revision still asks model to copy %s", field)
		}
	}
	tuple := properties["cases"].(map[string]any)
	if tuple["minItems"] != 2 || tuple["maxItems"] != 2 || tuple["items"] != false {
		t.Fatal("affected revisions are not the exact issued tuple")
	}
	for _, item := range tuple["prefixItems"].([]any) {
		if item.(map[string]any)["properties"].(map[string]any)["caseId"] != nil {
			t.Fatal("revision still asks model to copy the known case ID")
		}
	}
}

func TestCaseRevisionRejectsManualRetentionAndIdentityOrMissingDuty(t *testing.T) {
	input := retainedDesignInput()
	input.Validation.AffectedCases = []string{"change"}
	for _, raw := range []string{`{"retainCases":["keep","change","delete"],"cases":[],"coverage":{}}`, `{"cases":[{"caseId":"change","oracle":"corrected"}]}`, `{"cases":[]}`, `{"cases":[{},{}]}`, `{"cases":[{}],"sharedSteps":[{"id":"setup","procedure":"corrected"}]}`} {
		if _, err := DecodeResultWithPaths(input, "result", []byte(raw), "original-output", []string{}); err == nil {
			t.Fatalf("unissued or copied revision admitted: %s", raw)
		}
	}
	result, err := DecodeResultWithPaths(input, "result", []byte(`{"cases":[{"oracle":""}]}`), "original-output", []string{})
	if err != nil || result.Result.Validation.Manifest.Cases[1].Oracle != "" {
		t.Fatal("an explicit semantic clear was confused with retaining a field", err)
	}
}
