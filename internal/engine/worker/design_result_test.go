package worker

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func retainedDesignInput() protocol.WorkerInput {
	input := semanticInput()
	input.Validation.Manifest.Cases = []coverage.AcceptanceCase{
		{CaseID: "keep", Mode: coverage.CaseBlackbox, Scenario: "MISUSE", PublicEntry: "app --invalid", Preconditions: "approved fixture", Steps: "run the invalid input", Oracle: "reject invalid input", Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"app", "", "two words"}, Stdin: "line\n"}}, SharedSteps: []string{"setup"}, Files: []string{"keep.fixture"}},
		{CaseID: "change", Mode: coverage.CaseBlackbox, Scenario: "SUCCESS", Oracle: "original success"},
		{CaseID: "delete", Mode: coverage.CaseBlackbox, Scenario: "BOUNDARY", Oracle: "original boundary"},
	}
	input.Validation.Manifest.SharedSteps = []coverage.SharedStep{{ID: "setup", Procedure: "prepare the approved fixture", Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf '%s' 'prepared'"}}, Files: []string{"setup.fixture"}}, {ID: "unused", Procedure: "unused setup"}}
	input.Validation.Manifest.Edges = []coverage.CoverageEdge{
		{ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: "keep"},
		{ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "one", CaseID: "keep"},
		{ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "two", CaseID: "change"},
		{ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "two", CaseID: "delete"},
	}
	return input
}

func TestDesignRetentionPreservesIssuedContentAndCoverage(t *testing.T) {
	input := retainedDesignInput()
	before, _ := json.Marshal(input)
	raw := []byte(`{"retainCases":[1],"retainSharedSteps":[1],"cases":[{"scenario":"SUCCESS","oracle":"rewritten oracle"}],"coverage":[null,[2]]}`)
	original := bytes.Clone(raw)
	result, err := DecodeResultWithPaths(input, "result", raw, "verified-original-output", []string{})
	if err != nil {
		t.Fatal(err)
	}
	manifest := result.Result.Validation.Manifest
	if !reflect.DeepEqual(manifest.Cases[0], input.Validation.Manifest.Cases[0]) || !reflect.DeepEqual(manifest.SharedSteps, input.Validation.Manifest.SharedSteps[:1]) {
		t.Fatal("retention changed issued narrative, operations, argv/stdin or files")
	}
	if len(manifest.Cases) != 2 || manifest.Cases[1].Oracle != "rewritten oracle" || len(manifest.Edges) != 3 || manifest.Edges[2].CaseID != manifest.Cases[1].CaseID {
		t.Fatal("rewriting or deleting a case changed retained relations")
	}
	input.Validation.Interruption = &protocol.RecoveryRecord{}
	resumed, err := DecodeResultWithPaths(input, "result", raw, "verified-original-output", []string{})
	if err != nil || !reflect.DeepEqual(result, resumed) {
		t.Fatal("recovered input uses a different assembly", err)
	}
	input.Validation.Interruption = nil
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) || !bytes.Equal(original, raw) || result.Result.PayloadDigest != "verified-original-output" {
		t.Fatal("assembly mutated issued facts or original model output/proof")
	}
}

func TestDesignCoverageReplacesAndClearsExplicitPointMappings(t *testing.T) {
	input := retainedDesignInput()
	value := Design{DesignManifest: DesignManifest{RetainCases: []int{1, 2}, RetainSharedSteps: []int{1}, Cases: []DesignedCase{}, Coverage: [][]int{{}, {1}}}}
	manifest := decodeSemantic(t, input, value).Validation.Manifest
	want := []coverage.CoverageEdge{{ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "two", CaseID: "keep"}}
	if !reflect.DeepEqual(manifest.Edges, want) {
		t.Fatalf("changed mapping mixed with original relations: %+v", manifest.Edges)
	}
	value.DesignManifest.RetainCases, value.DesignManifest.RetainSharedSteps = nil, nil
	value.DesignManifest.Coverage = [][]int{nil, nil}
	manifest = decodeSemantic(t, input, value).Validation.Manifest
	if len(manifest.Cases) != 0 || len(manifest.SharedSteps) != 0 || len(manifest.Edges) != 0 {
		t.Fatal("omission silently retained a deleted entry or relation")
	}
}

func TestDesignRetentionRejectsUnknownDuplicateAndAuthoredIDs(t *testing.T) {
	for _, raw := range []string{
		`{"retainCases":["unknown"],"cases":[],"coverage":{}}`,
		`{"retainCases":["keep","keep"],"cases":[],"coverage":{}}`,
		`{"retainCases":["keep"],"cases":[{"caseId":"keep","scenario":"MISUSE","oracle":"conflicting body"}],"coverage":{}}`,
		`{"retainSharedSteps":["unknown"],"cases":[],"coverage":{}}`,
		`{"retainSharedSteps":["setup","setup"],"cases":[],"coverage":{}}`,
		`{"retainSharedSteps":["setup"],"sharedSteps":[{"id":"setup","procedure":"conflicting instructions"}],"cases":[],"coverage":{}}`,
	} {
		if _, err := DecodeResultWithPaths(retainedDesignInput(), "result", []byte(raw), "original-output", []string{}); err == nil {
			t.Fatalf("ambiguous retention accepted: %s", raw)
		}
	}
}

func TestDesignRetentionPreservesSharedReferenceForCoreValidation(t *testing.T) {
	input := retainedDesignInput()
	manifest := decodeSemantic(t, input, Design{DesignManifest: DesignManifest{RetainCases: []int{1}, Cases: []DesignedCase{}, Coverage: [][]int{nil, nil}}}).Validation.Manifest
	if len(manifest.SharedSteps) != 0 || !reflect.DeepEqual(manifest.Cases[0].SharedSteps, []string{"setup"}) {
		t.Fatal("assembly invented a shared step or erased a real invalid reference")
	}
}

func TestConflictResolutionUsesExactIssuedPaths(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.merge.resolve"
	input.Validation.Conflict = &protocol.ConflictInput{Evidence: protocol.IntegrationEvidence{Conflicts: []protocol.MergeConflict{{Path: "z.go"}, {Path: "a.go"}}}}
	raw := []byte(`{"summary":"Resolved the conflicting behavior while preserving both approved interfaces"}`)
	before, _ := json.Marshal(input)
	original := bytes.Clone(raw)
	result, err := DecodeResultWithPaths(input, "result", raw, "original-native-output", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Result.Validation.Resolution.Paths, []string{"z.go", "a.go"}) || result.Result.PayloadDigest != "original-native-output" {
		t.Fatal("resolution reconstructed a different path set or output proof")
	}
	input.Validation.Interruption = &protocol.RecoveryRecord{}
	resumed, err := DecodeResultWithPaths(input, "result", raw, "original-native-output", []string{})
	if err != nil || !reflect.DeepEqual(result, resumed) {
		t.Fatal("recovery uses another conflict assembly", err)
	}
	input.Validation.Interruption = nil
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) || !bytes.Equal(original, raw) {
		t.Fatal("resolution mutated original input/output")
	}
	if _, err := DecodeResultWithPaths(input, "result", []byte(`{"paths":["a.go"],"summary":"authored path copy"}`), "original-native-output", []string{}); err == nil {
		t.Fatal("model path authority accepted")
	}
	input.Validation.Conflict = nil
	if _, err := DecodeResultWithPaths(input, "result", raw, "original-native-output", []string{}); err == nil {
		t.Fatal("missing original conflict gained a completed result")
	}
	input.Validation.Conflict = &protocol.ConflictInput{}
	if _, err := DecodeResultWithPaths(input, "result", raw, "original-native-output", []string{}); err == nil {
		t.Fatal("empty original conflict gained a completed result")
	}
}

func TestDesignCoverageKeepsOriginalRelationValuesAndOrder(t *testing.T) {
	input := retainedDesignInput()
	input.Validation.Manifest.Edges[0], input.Validation.Manifest.Edges[1] = input.Validation.Manifest.Edges[1], input.Validation.Manifest.Edges[0]
	input.Validation.Manifest.Edges[0].ReviewKind = ""
	original := append([]coverage.CoverageEdge(nil), input.Validation.Manifest.Edges[:2]...)
	value := Design{DesignManifest: DesignManifest{RetainCases: []int{1}, RetainSharedSteps: []int{1}, Cases: []DesignedCase{}, Coverage: [][]int{{1}, nil}}}
	manifest := decodeSemantic(t, input, value).Validation.Manifest
	if !reflect.DeepEqual(manifest.Edges, original) || !reflect.DeepEqual(manifest.Cases[0], input.Validation.Manifest.Cases[0]) {
		t.Fatal("unchanged selected coverage relations or case were rewritten by model choice order")
	}
}
