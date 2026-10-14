package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

func assertIntakePayloadWithoutRun(t *testing.T, root string, want facade.IntakeConfirmationReceipt) {
	t.Helper()
	paths, err := facade.OriginalPayloadPaths(workflowPayloadDirectory(root, "", "intake"))
	if err != nil || len(paths) != 1 {
		t.Fatal("original intake candidate not saved", paths, err)
	}
	var got facade.IntakeConfirmationReceipt
	if err := readJSONFileInto(paths[0], &got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("original candidate content changed", got, err)
	}
	for _, path := range []string{facade.EngineNamespace, ".gates/registry.json"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatal("preparation started a run or registry", path, err)
		}
	}
}

func TestIntakeSelectsOriginalDocumentsAndMaterialPositions(t *testing.T) {
	root := t.TempDir()
	request := cliStructuredRequirement("Select original materials.")
	mustWriteCLI(t, filepath.Join(root, "need.md"), request)
	mustWriteCLI(t, filepath.Join(root, "design.txt"), "Complete independent solution\r\n原文  ")
	mustWriteCLI(t, filepath.Join(root, "reference.txt"), "Original reference\n")
	var out, stderr bytes.Buffer
	invoke := func(args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal(args, stderr.String())
		}
	}
	invoke("workflow", "start", "--root", root, "--precheck", "--materials")
	var materials []requirementDocumentChoice
	if err := json.Unmarshal(out.Bytes(), &materials); err != nil || len(materials) != 3 {
		t.Fatal("material positions", out.String(), err)
	}
	invoke("workflow", "start", "--root", root, "--precheck", "--document", "1", "--solution-document", "1", "--artifact-document", "3")
	var preview facade.IntakePreview
	if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.IntakeCandidate.RequirementSource != "need.md" || len(preview.Materials) != 3 || preview.IntakeCandidate.SolutionRevision != preview.Materials[0].Revision {
		t.Fatal("selected original materials were rebound", preview)
	}
	assertIntakePayloadWithoutRun(t, root, preview.IntakeCandidate)
	original, err := os.ReadFile(filepath.Join(root, "design.txt"))
	if err != nil || string(original) != "Complete independent solution\r\n原文  " {
		t.Fatal("precheck changed original bytes", err)
	}
	invoke("workflow", "start", "--root", root, "--precheck", "--document", "1", "--solution-document", "1", "--artifact-document", "3")
	assertIntakePayloadWithoutRun(t, root, preview.IntakeCandidate)
	t.Log("document position 1 and material positions 1/3 preserved full original intake; repeated preparation reused the same payload without run/registry")
}

func TestLightweightIntakeSelectsOrdinaryMaterialPositions(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		t.Run(strconv.FormatBool(multiple), func(t *testing.T) {
			root := t.TempDir()
			originals := map[string]string{"request.md": "# 普通需求与方案\r\n保留原文、空格  和引号 \"原样\"。\r\n"}
			if multiple {
				originals["design.txt"] = "独立方案\r\n"
				originals["reference.txt"] = "原参考\n"
			}
			for path, content := range originals {
				mustWriteCLI(t, filepath.Join(root, path), content)
			}
			var out, stderr bytes.Buffer
			invoke := func(flags ...string) int {
				t.Helper()
				out.Reset()
				stderr.Reset()
				args := append([]string{"workflow", "start", "--precheck", "--root", root}, flags...)
				return Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr})
			}
			if invoke("--route", "lightweight", "--materials") != 0 {
				t.Fatal(stderr.String())
			}
			var materials []requirementDocumentChoice
			if err := json.Unmarshal(out.Bytes(), &materials); err != nil || len(materials) != len(originals) {
				t.Fatal("ordinary material positions", out.String(), err)
			}
			positions := map[string]int{}
			for _, material := range materials {
				positions[material.Path] = material.Position
			}
			if invoke("--route", "lightweight") != 0 {
				t.Fatal(stderr.String())
			}
			if multiple {
				var choices []requirementDocumentChoice
				if err := json.Unmarshal(out.Bytes(), &choices); err != nil || !reflect.DeepEqual(choices, materials) {
					t.Fatal("ambiguous lightweight intake did not retain original positions", out.String(), err)
				}
				if paths, err := facade.OriginalPayloadPaths(workflowPayloadDirectory(root, "", "intake")); err != nil || len(paths) != 0 {
					t.Fatal("ambiguous selection saved a candidate", paths, err)
				}
			}
			flags := []string{"--route", "lightweight", "--document", strconv.Itoa(positions["request.md"])}
			if multiple {
				flags = append(flags, "--solution-document", strconv.Itoa(positions["design.txt"]), "--artifact-document", strconv.Itoa(positions["reference.txt"]))
			}
			for range 2 {
				if invoke(flags...) != 0 {
					t.Fatal(stderr.String())
				}
				var preview facade.IntakePreview
				if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
					t.Fatal(err)
				}
				if preview.RequirementDocument != nil || preview.Content != strings.ReplaceAll(originals["request.md"], "\r\n", "\n") || preview.IntakeCandidate.RequirementSource != "request.md" || preview.IntakeCandidate.RequirementRevision != preview.Revision || len(preview.Materials) != len(originals) {
					t.Fatal("lightweight selection changed original confirmation material", preview)
				}
				for _, material := range preview.Materials {
					if material.Content != strings.ReplaceAll(originals[material.Path], "\r\n", "\n") {
						t.Fatal("selected material lost its original bytes", material)
					}
				}
				assertIntakePayloadWithoutRun(t, root, preview.IntakeCandidate)
			}
			if invoke("--route", "regular", "--document", "1") == 0 || !strings.Contains(stderr.String(), "select an original displayed requirement document") {
				t.Fatal("regular document selection admitted ordinary material", out.String(), stderr.String())
			}
			if invoke("--route", "regular", "--requirement", "request.md") == 0 || !strings.Contains(stderr.String(), "document must contain exactly one ## 需求点 section") {
				t.Fatal("regular precheck stopped requiring structured requirements", out.String(), stderr.String())
			}
			for path, content := range originals {
				actual, err := os.ReadFile(filepath.Join(root, path))
				if err != nil || string(actual) != content {
					t.Fatal("intake changed source material", path, err)
				}
			}
		})
	}
}

func TestControlPayloadSelectionConfirmsOriginalAskWithoutRewriting(t *testing.T) {
	root := t.TempDir()
	f := startAdmittedEngineRun(t, root)
	var out, stderr bytes.Buffer
	call := func(args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append([]string{"workflow"}, args...)
		args = append(args, "--root", root)
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal(args, stderr.String())
		}
	}
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	for _, command := range []string{"show", "status", "next"} {
		call(command)
	}
	if !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
		t.Fatal("pure projections saved payloads or state")
	}
	call("next", "--request", "ABORT")
	var selected struct {
		Position int `json:"confirmationCandidate"`
	}
	if json.Unmarshal(out.Bytes(), &selected) != nil || selected.Position != 1 {
		t.Fatal(out.String())
	}
	if run, err := f.Show(); err != nil || len(run.PendingAsks) != 0 {
		t.Fatal("preparation applied request before confirmation", run, err)
	}
	paths, _ := facade.OriginalPayloadPaths(workflowPayloadDirectory(root, "writer-exclusive", "control"))
	original, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	call("submit", "--confirmation-candidate", "1", "--single-step")
	run, err := f.Show()
	if err != nil || len(run.PendingAsks) != 1 {
		t.Fatal(run, err)
	}
	request := run.PendingAsks[0]
	reason := "真实用户原文 \"拒绝\"\r\n尾随空格  "
	call("next", "--ask", "1", "--choice", "decline", "--reason", reason)
	if json.Unmarshal(out.Bytes(), &selected) != nil || selected.Position != 2 {
		t.Fatal("payload ordinal shifted", out.String())
	}
	if got, err := os.ReadFile(paths[0]); err != nil || !bytes.Equal(got, original) {
		t.Fatal("preparing another candidate replaced original", err)
	}
	call("submit", "--confirmation-candidate", "2", "--single-step")
	run, err = f.Show()
	if err != nil || len(run.PendingAsks) != 0 || run.Status != "ACTIVE" {
		t.Fatal(run, err)
	}
	paths, _ = facade.OriginalPayloadPaths(workflowPayloadDirectory(root, "writer-exclusive", "control"))
	var candidate facade.ControlCandidate
	if err := readJSONFileInto(paths[1], &candidate); err != nil || candidate.Event.Decide.Request != protocol.RequestID(request.RequestID) || candidate.Input.Reason != reason {
		t.Fatal("original request/token/content lost", candidate, err)
	}
	accepted := snapshotEngineRunTree(t, root, "writer-exclusive")
	call("submit", "--confirmation-candidate", "2", "--single-step")
	if !reflect.DeepEqual(accepted, snapshotEngineRunTree(t, root, "writer-exclusive")) {
		t.Fatal("replay modified original accepted state")
	}
	t.Log("program payload positions 1/2 selected original request and Ask decision; preparation had no effect; exact text and replay preserved")
}

func TestProgressPayloadSelectionReadsOriginalCursor(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	var out, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "progress", "--root", root, "--save-progress"}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	paths, _ := facade.OriginalPayloadPaths(workflowPayloadDirectory(root, "writer-exclusive", "progress"))
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	original, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var cursor facade.Progress
	if err := json.Unmarshal(original, &cursor); err != nil {
		t.Fatal(err)
	}
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	out.Reset()
	if code := Run("formal-gates", []string{"workflow", "progress", "--root", root, "--after", "1", "--machine"}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	var next facade.Progress
	if json.Unmarshal(out.Bytes(), &next) != nil || next.AfterRevision != cursor.Revision || next.Root != root || next.RunID != cursor.RunID || !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
		t.Fatal("cursor selection changed owner/revision/state", out.String())
	}
	t.Logf("saved progress position 1 consumed original revision %d without writing state or cursor", cursor.Revision)
}

func TestNativePayloadSelectionBindsOriginalRequestAndTextBytes(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	job := filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "jobs", "original")
	pending := `[{"connection":1,"method":"session/request_permission","options":[{"optionId":"allow-once","name":"Allow once","kind":"allow_once"}],"request_id":"41","status":"pending"},{"connection":1,"method":"session/request_permission","options":[{"optionId":"reject-once","name":"Reject","kind":"reject_once"}],"request_id":"42","status":"pending"}]`
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, filepath.Join(job, "interaction-pending.json"), pending)
	var out, stderr bytes.Buffer
	call := func(input string, args ...string) int {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append([]string{"native", "respond", "--root", root}, args...)
		return Run("formal-gates", args, IO{Stdin: strings.NewReader(input), Stdout: &out, Stderr: &stderr})
	}
	if code := call("", "--question", "1", "--choice", "1"); code != 0 {
		t.Fatal(stderr.String())
	}
	var preview struct {
		Question  map[string]any `json:"question"`
		Selection map[string]any `json:"selection"`
	}
	if err := json.Unmarshal(out.Bytes(), &preview); err != nil || preview.Question["request_id"] != "41" || preview.Selection["choice"] != float64(1) {
		t.Fatal("native confirmation view lost its saved original request/selection", preview, err)
	}
	originalDirectory, err := nativeRequestPayloadDirectory(filepath.Join(root, facade.EngineNamespace, "writer-exclusive"), 1)
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := facade.OriginalPayloadPaths(filepath.Join(originalDirectory, "decisions"))
	if err != nil || len(decisions) != 1 {
		t.Fatal(decisions, err)
	}
	if code := Run("formal-gates", []string{"native", "show", "--root", root, "--decision-file", decisions[0]}, IO{Stdout: &out, Stderr: &stderr}); code == 0 {
		t.Fatal("native show accepted an answer file")
	}

	if _, err := os.Stat(filepath.Join(job, "interaction-answer.json")); !os.IsNotExist(err) {
		t.Fatal("preparation answered before confirmation")
	}
	if code := call("", "--question", "2", "--decision", "1"); code == 0 || !strings.Contains(stderr.String(), "select an original displayed payload") {
		t.Fatal("another request consumed saved decision", code, out.String(), stderr.String())
	}
	mustWriteCLI(t, filepath.Join(job, "interaction-pending.json"), strings.Replace(pending, `"connection":1`, `"connection":2`, 1))
	if code := call("", "--question", "1", "--decision", "1"); code == 0 || !strings.Contains(stderr.String(), "select an original displayed payload") {
		t.Fatal("reconnected native request consumed another connection's payload", code, out.String(), stderr.String())
	}
	mustWriteCLI(t, filepath.Join(job, "interaction-pending.json"), pending)

	if code := call("", "--question", "1", "--decision", "1"); code != 0 {
		t.Fatal(stderr.String())
	}
	var answer map[string]any
	if err := readJSONFileInto(filepath.Join(job, "interaction-answer.json"), &answer); err != nil || answer["request_id"] != "41" || answer["option"] != "allow-once" {
		t.Fatal("original native answer binding lost", answer, err)
	}
	if err := os.Remove(filepath.Join(job, "interaction-answer.json")); err != nil {
		t.Fatal(err)
	}
	pending = `[{"connection":1,"method":"interaction/requestUserInput","questions":[{"question":"Original question"}],"request_id":"text-request","status":"pending"}]`
	mustWriteCLI(t, filepath.Join(job, "interaction-pending.json"), pending)
	text := "保留原文 \"确认\"\r\n尾随空格  "
	if code := call(text, "--prepare-text"); code != 0 {
		t.Fatal(stderr.String())
	}
	directory, err := nativeRequestPayloadDirectory(filepath.Join(root, facade.EngineNamespace, "writer-exclusive"), 1)
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := facade.OriginalPayloadPaths(filepath.Join(directory, "responses", "1"))
	body, err := os.ReadFile(paths[0])
	if err != nil || string(body) != text {
		t.Fatal("original native text bytes changed", err)
	}
	if code := call("", "--text-payload", "1"); code != 0 {
		t.Fatal(stderr.String())
	}
	if code := call("", "--confirm"); code != 0 {
		t.Fatal(stderr.String())
	}
	if err := readJSONFileInto(filepath.Join(job, "interaction-answer.json"), &answer); err != nil || answer["request_id"] != "text-request" {
		t.Fatal(answer, err)
	}
	raw, _ := json.Marshal(answer)
	if !strings.Contains(string(raw), "尾随空格  ") {
		t.Fatal("native answer lost original free-text bytes", answer)
	}
	t.Log("native decision payload stayed within its original request; stdin original text bytes saved and selected before existing confirmed answer owner")
}

func TestOriginalPayloadPositionsRemainStableUnderConcurrentPreparation(t *testing.T) {
	directory := t.TempDir()
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			if _, _, err := facade.SaveOriginalPayload(directory, []byte(strconv.Itoa(i))); err != nil {
				t.Error(err)
			}
		}(i)
	}
	group.Wait()
	paths, err := facade.OriginalPayloadPaths(directory)
	if err != nil || len(paths) != 8 {
		t.Fatal(paths, err)
	}
	originals := map[string]string{}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		originals[path] = string(body)
	}
	_, position, err := facade.SaveOriginalPayload(directory, []byte("additional"))
	if err != nil || position != 9 {
		t.Fatal(position, err)
	}
	after, _ := facade.OriginalPayloadPaths(directory)
	for i, path := range paths {
		body, err := os.ReadFile(after[i])
		if err != nil || after[i] != path || string(body) != originals[path] {
			t.Fatal("original payload position or bytes changed", i, err)
		}
	}
}

func TestNativeOriginalTextSelectionFollowsEachInputQuestion(t *testing.T) {
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	run := filepath.Join(root, facade.EngineNamespace, "writer-exclusive")
	job := filepath.Join(run, "jobs", "inputs")
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, filepath.Join(job, "interaction-pending.json"), `[{"connection":1,"method":"item/tool/requestUserInput","request_id":17,"status":"pending","questions":[{"id":"first","question":"First original question"},{"id":"second","question":"Second original question"}]}]`)
	var out, stderr bytes.Buffer
	call := func(input string, args ...string) {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append([]string{"native", "respond", "--root", root}, args...)
		if code := Run("formal-gates", args, IO{Stdin: strings.NewReader(input), Stdout: &out, Stderr: &stderr}); code != 0 {
			t.Fatal(args, stderr.String())
		}
	}
	first, second := "第一份原文\r\n  ", "第二份原文\n尾随  "
	call(first, "--response-question", "1", "--prepare-text")
	call(second, "--response-question", "2", "--prepare-text")
	call("", "--response-question", "1", "--text-payload", "1", "--response-question", "2", "--text-payload", "1")
	if _, err := os.Stat(filepath.Join(job, "interaction-answer.json")); !os.IsNotExist(err) {
		t.Fatal("text preparation answered before confirmation")
	}
	call("", "--decision", "1")
	var answer struct {
		Request   int    `json:"request_id"`
		Cancel    bool   `json:"cancel"`
		WrittenAt string `json:"written_at"`
		Answers   map[string]struct {
			Answers []string `json:"answers"`
		} `json:"answers"`
	}
	if err := readJSONFileInto(filepath.Join(job, "interaction-answer.json"), &answer); err != nil || answer.Request != 17 || answer.Cancel || answer.WrittenAt == "" || !reflect.DeepEqual(answer.Answers["first"].Answers, []string{first}) || !reflect.DeepEqual(answer.Answers["second"].Answers, []string{second}) {
		t.Fatal("original input-question identities or content crossed", answer, err)
	}
	t.Log("two original text payload position 1 values stayed bound to their separate original input-question IDs in the same native answer")
}

func TestPayloadSaveFailurePreservesCompleteOriginalFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "candidate.json")
	original := []byte("complete original candidate\r\n  ")
	if err := facade.WriteOriginalPayload(path, original, false); err != nil {
		t.Fatal(err)
	}
	if err := facade.WriteOriginalPayload(path, []byte("replacement"), true); !os.IsExist(err) {
		t.Fatal("exclusive save replaced original", err)
	}
	body, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(body, original) {
		t.Fatal("failed publication changed original bytes", err)
	}
	if err := facade.WriteOriginalPayload(directory, []byte("replacement"), false); err == nil {
		t.Fatal("file save replaced a directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "candidate.json" {
		t.Fatal("failed payload save left temporary or partial publication", entries, err)
	}
	if err := saveWorkflowCandidate(path, map[string]any{"revision": uint64(9007199254740993), "text": "原文  "}); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil || !bytes.Contains(body, []byte("9007199254740993")) || !bytes.Contains(body, []byte("原文  ")) {
		t.Fatal("shared exact candidate writer lost original values", err)
	}
	var out bytes.Buffer
	if _, err := printPayloadValue(&out, map[string]any{"revision": uint64(9007199254740993), "text": "原文  "}, "confirmationCandidate", 1); err != nil || !strings.Contains(out.String(), "9007199254740993") {
		t.Fatal("payload display changed original integer", out.String(), err)
	}
	t.Log("failed exclusive/replace publication retained complete original bytes without partial payloads; exact candidate and display preserved original integer and text")
}
