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
	raw := []byte(`{"removeCases":[3],"removeSharedSteps":[2],"cases":[{"case":2,"scenario":"SUCCESS","oracle":"rewritten oracle"}],"coverage":[null,[2]]}`)
	original := bytes.Clone(raw)
	result, err := DecodeResultWithPaths(input, "result", raw, "verified-original-output", []string{})
	if err != nil {
		t.Fatal(err)
	}
	manifest := result.Result.Validation.Manifest
	if !reflect.DeepEqual(manifest.Cases[0], input.Validation.Manifest.Cases[0]) || !reflect.DeepEqual(manifest.SharedSteps, input.Validation.Manifest.SharedSteps[:1]) {
		t.Fatal("retention changed issued narrative, operations, argv/stdin or files")
	}
	if len(manifest.Cases) != 2 || manifest.Cases[1].CaseID != input.Validation.Manifest.Cases[1].CaseID || manifest.Cases[1].Oracle != "rewritten oracle" || len(manifest.Edges) != 3 || manifest.Edges[2].CaseID != manifest.Cases[1].CaseID {
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
	value := Design{DesignManifest: DesignManifest{Cases: []DesignedCase{}, Coverage: [][]int{{}, {1}}}}
	manifest := decodeSemantic(t, input, value).Validation.Manifest
	want := []coverage.CoverageEdge{{ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "two", CaseID: "keep"}}
	if !reflect.DeepEqual(manifest.Edges, want) {
		t.Fatalf("changed mapping mixed with original relations: %+v", manifest.Edges)
	}
	value.DesignManifest.RemoveCases, value.DesignManifest.RemoveSharedSteps = []int{1, 2, 3}, []int{1, 2}
	value.DesignManifest.Coverage = [][]int{nil, nil}
	manifest = decodeSemantic(t, input, value).Validation.Manifest
	if len(manifest.Cases) != 0 || len(manifest.SharedSteps) != 0 || len(manifest.Edges) != 0 {
		t.Fatal("omission silently retained a deleted entry or relation")
	}
}

func TestDesignChangeRejectsUnknownDuplicateAndAuthoredIDs(t *testing.T) {
	for _, raw := range []string{
		`{"removeCases":[99],"cases":[],"coverage":[null,null]}`,
		`{"removeCases":[1,1],"cases":[],"coverage":[null,null]}`,
		`{"retainCases":["keep"],"cases":[{"caseId":"keep","scenario":"MISUSE","oracle":"conflicting body"}],"coverage":{}}`,
		`{"removeSharedSteps":[99],"cases":[],"coverage":[null,null]}`,
		`{"removeSharedSteps":[1,1],"cases":[],"coverage":[null,null]}`,
		`{"retainSharedSteps":["setup"],"sharedSteps":[{"id":"setup","procedure":"conflicting instructions"}],"cases":[],"coverage":{}}`,
	} {
		if _, err := DecodeResultWithPaths(retainedDesignInput(), "result", []byte(raw), "original-output", []string{}); err == nil {
			t.Fatalf("ambiguous retention accepted: %s", raw)
		}
	}
}

func TestDesignRetentionPreservesSharedReferenceForCoreValidation(t *testing.T) {
	input := retainedDesignInput()
	manifest := decodeSemantic(t, input, Design{DesignManifest: DesignManifest{RemoveSharedSteps: []int{1, 2}, Cases: []DesignedCase{}, Coverage: [][]int{nil, nil}}}).Validation.Manifest
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
	value := Design{DesignManifest: DesignManifest{RemoveCases: []int{2, 3}, RemoveSharedSteps: []int{2}, Cases: []DesignedCase{}, Coverage: [][]int{{1}, nil}}}
	manifest := decodeSemantic(t, input, value).Validation.Manifest
	if !reflect.DeepEqual(manifest.Edges, original) || !reflect.DeepEqual(manifest.Cases[0], input.Validation.Manifest.Cases[0]) {
		t.Fatal("unchanged selected coverage relations or case were rewritten by model choice order")
	}
}

func TestWhiteboxDesignKeepsCompleteNativeDeliveryPaths(t *testing.T) {
	input := retainedDesignInput()
	input.Validation.Manifest.Binding.ReviewKind = coverage.ReviewWhitebox
	paths := []string{"keep.fixture", "support.data", "removed.fixture"}
	raw := []byte(`{"removeCases":[2,3],"removeSharedSteps":[2],"cases":[],"coverage":[null,[]]}`)
	event, err := DecodeResultWithPaths(input, "result", raw, "original-source", paths)
	if err != nil {
		t.Fatal(err)
	}
	if event.Result.Validation.Development == nil || !reflect.DeepEqual(event.Result.Validation.Development.Paths, paths) {
		t.Fatalf("captured delivery dropped helper/removal: %+v", event.Result.Validation.Development)
	}
	if !reflect.DeepEqual(event.Result.Validation.Manifest.TestFiles(), []string{"keep.fixture", "setup.fixture"}) {
		t.Fatal("captured delivery invented semantic case relations")
	}
	input.Validation.Interruption = &protocol.RecoveryRecord{}
	recovered, err := DecodeResultWithPaths(input, "result", raw, "original-source", paths)
	if err != nil || !reflect.DeepEqual(event, recovered) {
		t.Fatal("recovery changed native delivery or semantic relations", err)
	}
}

func TestCaseFileSelectionsPreserveReferencesAndSelectPublishedSymbols(t *testing.T) {
	input := semanticInput()
	input.Validation.Manifest.Binding.ReviewKind = coverage.ReviewWhitebox
	input.Validation.Manifest.Cases[0].Files = []string{"case_test.go", "support.data"}
	input.Validation.Manifest.Cases[0].TestRef = "case_test.go::TestOriginal"
	input.Validation.AffectedCases = []string{"new"}
	facts := []LocationFact{
		{Kind: "listing", Paths: []string{"case_test.go", "support.data", "replacement_test.go"}},
		{Kind: "read", Paths: []string{"case_test.go"}, Content: "package sample\nimport \"testing\"\nfunc TestOriginal(t *testing.T) {}\nfunc TestOther(t *testing.T) {}\n"},
		{Kind: "read", Paths: []string{"replacement_test.go"}, Content: "package sample\nimport check \"testing\"\nfunc TestCreated(t *check.T) {}\n"},
	}
	for _, sample := range []struct{ raw, ref string }{
		{`{"cases":[{}]}`, "case_test.go::TestOriginal"},
		{`{"cases":[{"operations":[{"kind":"ARGV","argv":["go","test","."]}]}]}`, ""},
		{`{"cases":[{"files":[{"read":2}],"testFile":1,"testSymbol":2}]}`, "case_test.go::TestOther"},
		{`{"cases":[{"files":[{"read":3}],"testFile":1,"testSymbol":1}]}`, "replacement_test.go::TestCreated"},
	} {
		event, err := DecodeResultWithFacts(input, "result", []byte(sample.raw), "original-source", []string{}, facts)
		if err != nil {
			t.Fatal(sample.raw, err)
		}
		if got := event.Result.Validation.Manifest.Cases[0].TestRef; got != sample.ref {
			t.Fatalf("reference %q != %q", got, sample.ref)
		}
		input.Validation.Interruption = &protocol.RecoveryRecord{}
		recovered, err := DecodeResultWithFacts(input, "result", []byte(sample.raw), "original-source", []string{}, facts)
		if err != nil || !reflect.DeepEqual(event, recovered) {
			t.Fatal("recovery changed selected file/reference", err)
		}
		input.Validation.Interruption = nil
	}
	for _, raw := range []string{
		`{"cases":[{"files":["case_test.go"]}]}`,
		`{"cases":[{"files":[{"read":2,"line":1}],"testFile":1,"testSymbol":1}]}`,
		`{"cases":[{"files":[{"requirementLine":1}]}]}`,
		`{"cases":[{"testRef":"case_test.go::TestOriginal"}]}`,
		`{"cases":[{"testRef":"TestCreated"}]}`,
		`{"cases":[{"testFile":1,"testSymbol":1}]}`,
		`{"cases":[{"files":[{"listing":1,"entry":1}],"testFile":1,"testSymbol":1}]}`,
		`{"cases":[{"files":[{"read":2}],"testFile":2,"testSymbol":1}]}`,
		`{"cases":[{"files":[{"read":3}],"testFile":1,"testSymbol":2}]}`,
		`{"cases":[{"files":[{"read":2}],"testFile":1}]}`,
		`{"cases":[{"files":[{"read":2}],"testSymbol":1}]}`,
	} {
		if _, err := DecodeResultWithFacts(input, "invalid", []byte(raw), "original-source", []string{}, facts); err == nil {
			t.Fatal("copied/invalid file identity accepted", raw)
		}
	}
	if input.Validation.Manifest.Cases[0].TestRef != "case_test.go::TestOriginal" || !reflect.DeepEqual(input.Validation.Manifest.Cases[0].Files, []string{"case_test.go", "support.data"}) {
		t.Fatal("assembly mutated original file/reference facts")
	}
}

func TestDesignFileSchemaPublishesOnlyFileSelections(t *testing.T) {
	input := semanticInput()
	input.Validation.Manifest.Binding.ReviewKind = coverage.ReviewWhitebox
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	body := schema["anyOf"].([]any)[0].(map[string]any)
	item := body["properties"].(map[string]any)["cases"].(map[string]any)["items"].(map[string]any)
	properties := item["properties"].(map[string]any)
	location := properties["files"].(map[string]any)["items"].(map[string]any)
	if location["type"] != "object" || properties["testFile"] == nil || properties["testSymbol"] == nil || properties["testRef"] != nil || location["properties"].(map[string]any)["line"] != nil || location["properties"].(map[string]any)["requirementLine"] != nil {
		t.Fatal("schema publishes raw/non-file identity", location)
	}
	input.Validation.Manifest.Binding.ReviewKind = coverage.ReviewBlackbox
	schema, err = ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	body = schema["anyOf"].([]any)[0].(map[string]any)
	item = body["properties"].(map[string]any)["cases"].(map[string]any)["items"].(map[string]any)
	if item["properties"].(map[string]any)["testFile"] != nil || item["properties"].(map[string]any)["testSymbol"] != nil {
		t.Fatal("blackbox receives file-bound whitebox reference choice")
	}
}
