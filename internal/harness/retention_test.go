package harness

import (
	"encoding/json"
	"strings"
	"testing"
)

// mustRetention* helpers decode a JSON payload the way the bridge would
// (StrictDecode, unknown fields rejected) and run the judgement under test.
func mustRetentionDispatch(t *testing.T, operation, payload string) map[string]any {
	t.Helper()
	result, err := retentionDispatch(operation, []byte(payload))
	if err != nil {
		t.Fatalf("%s: dispatch failed: %v", operation, err)
	}
	answer, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("%s: answer is not an object: %T", operation, result)
	}
	return answer
}

func wantRefusal(t *testing.T, answer map[string]any, refusal string) {
	t.Helper()
	if answer["ok"] != false {
		t.Fatalf("expected a refusal, got %v", answer)
	}
	if answer["refusal"] != refusal {
		t.Fatalf("refusal wording wrong: got %q want %q", answer["refusal"], refusal)
	}
}

func wantOK(t *testing.T, answer map[string]any) {
	t.Helper()
	if answer["ok"] != true {
		t.Fatalf("expected ok, got %v", answer)
	}
}

// repeatedRows builds n copies of one JSON row.
func repeatedRows(n int, row string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(row)
	}
	b.WriteByte(']')
	return b.String()
}

// ---- archive-inventory --------------------------------------------------------

func TestArchiveInventorySkipBeatsSymlinkAndKeepsWalkedOrder(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-inventory",
		`{"entries":[{"name":"runner.lock","kind":"symlink"},
		 {"name":"checkpoints.sqlite-wal","kind":"other"},
		 {"name":"archive.json","kind":"symlink"},
		 {"name":"zeta.txt","kind":"file"},
		 {"name":"alpha","kind":"dir"},
		 {"name":"a/child.txt","kind":"file"}]}`)
	wantOK(t, res)
	files := res["files"].([]string)
	if len(files) != 2 || files[0] != "zeta.txt" || files[1] != "a/child.txt" {
		t.Fatalf("walked order must be kept, got %v", files)
	}
}

func TestArchiveInventoryRefusals(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-inventory", `{"entries":[{"name":"outside","kind":"symlink"}]}`)
	wantRefusal(t, res, "ARCHIVE_UNSUPPORTED: symlink outside; no files deleted")
	res = mustRetentionDispatch(t, "archive-inventory",
		`{"entries":[{"name":"jobs/x","kind":"symlink"},{"name":"fifo","kind":"other"}]}`)
	wantRefusal(t, res, "ARCHIVE_UNSUPPORTED: symlink jobs/x; no files deleted")
	res = mustRetentionDispatch(t, "archive-inventory", `{"entries":[{"name":"fifo","kind":"other"}]}`)
	wantRefusal(t, res, "ARCHIVE_UNSUPPORTED: non-regular entry fifo")
}

// ---- chunk limit ---------------------------------------------------------------

func TestBatchedOperationsRejectOversizedEntryBatches(t *testing.T) {
	inventoryRow := `{"name":"f-0000","kind":"file"}`
	verifyRow := `{"name":"a.txt","actual":{"sha256":"aa","size":1},"expected":{"sha256":"aa","size":1}}`
	staleRow := `{"name":"a.txt","kind":"file","actual_proof":{"sha256":"aa","size":1},"expected_proof":{"sha256":"aa","size":1}}`
	batches := []struct {
		operation string
		prefix    string
		row       string
	}{
		{"archive-inventory", `{"entries":`, inventoryRow},
		{"archive-verify", `{"duplicates":false,"format":"formal-gates-archive-v1","status_name":"ABORTED",
		                   "names_match":true,"entries":`, verifyRow},
		{"archive-stale", `{"entries":`, staleRow},
	}
	for _, batch := range batches {
		if _, err := retentionDispatch(batch.operation,
			[]byte(batch.prefix+repeatedRows(retentionChunk+1, batch.row)+`}`)); err == nil {
			t.Fatalf("%s must reject a batch beyond the chunk limit", batch.operation)
		}
		if _, err := retentionDispatch(batch.operation,
			[]byte(batch.prefix+repeatedRows(retentionChunk, batch.row)+`}`)); err != nil {
			t.Fatalf("%s: a batch at the limit is legal: %v", batch.operation, err)
		}
	}
}

// ---- archive-destination --------------------------------------------------------

func TestArchiveDestinationComponentContainment(t *testing.T) {
	cases := []struct {
		directory, destination string
		refusal                string
	}{
		{"/runs/r", "/runs/r/archives/x.zip", "ARCHIVE_PATH: output must be outside the run"},
		{"/runs/r", "/runs/r", "ARCHIVE_PATH: output must be outside the run"},
		{"/runs/r", "/runs/r-2/x.zip", ""},
		{"/runs/r", "/elsewhere/x.zip", ""},
	}
	for _, c := range cases {
		res := mustRetentionDispatch(t, "archive-destination",
			`{"directory":"`+c.directory+`","destination":"`+c.destination+`"}`)
		if c.refusal == "" {
			wantOK(t, res)
			continue
		}
		wantRefusal(t, res, c.refusal)
	}
}

// ---- archive-settled --------------------------------------------------------

func TestArchiveSettledStatusFirst(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-settled",
		`{"status_name":"PAUSED","jobs":[{"name":"j1","has_request":true,"has_exit":false,"processes":[]}]}`)
	wantRefusal(t, res, "RUN_NOT_TERMINAL: active or paused run cannot be archived/pruned")
	for _, status := range []string{"HARNESS_COMPLETE", "DEMO_COMPLETE", "ABORTED"} {
		res := mustRetentionDispatch(t, "archive-settled", `{"status_name":"`+status+`","jobs":[]}`)
		wantOK(t, res)
	}
}

func TestArchiveSettledJobBranchesInOrder(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-settled",
		`{"status_name":"ABORTED","jobs":[{"name":"native-op","has_request":true,"has_exit":false,
		  "processes":[{"pid":7,"pid_valid":true,"probe":"alive"}]}]}`)
	wantRefusal(t, res, "RUN_UNSETTLED: native-op has no exit receipt; no cleanup")
	// An invalid identity never reaches the probe verdicts; a null pid is
	// what the wrapper sends for records that held no integer at all (a JSON
	// true is normalized away the same way — the owner-ruled bool exclusion
	// lives in the wrapper's pid_valid fact).
	for _, pid := range []string{`0`, `-5`, `null`} {
		res := mustRetentionDispatch(t, "archive-settled",
			`{"status_name":"ABORTED","jobs":[{"name":"j1","has_request":false,"has_exit":false,
			  "processes":[{"pid":`+pid+`,"pid_valid":false,"probe":"dead"}]}]}`)
		wantRefusal(t, res, "RUN_UNSETTLED: invalid process identity")
	}
	// pid_valid without a pid cannot name a live process either.
	res = mustRetentionDispatch(t, "archive-settled",
		`{"status_name":"ABORTED","jobs":[{"name":"j1","has_request":true,"has_exit":true,
		  "processes":[{"pid":null,"pid_valid":true,"probe":"alive"}]}]}`)
	wantRefusal(t, res, "RUN_UNSETTLED: invalid process identity")
	res = mustRetentionDispatch(t, "archive-settled",
		`{"status_name":"ABORTED","jobs":[{"name":"j1","has_request":true,"has_exit":true,
		  "processes":[{"pid":111,"pid_valid":true,"probe":"dead"},{"pid":4242,"pid_valid":true,"probe":"alive"}]}]}`)
	wantRefusal(t, res, "RUN_UNSETTLED: PID 4242 still exists; no cleanup")
	res = mustRetentionDispatch(t, "archive-settled",
		`{"status_name":"ABORTED","jobs":[{"name":"j1","has_request":true,"has_exit":true,
		  "processes":[{"pid":999,"pid_valid":true,"probe":"permission"}]}]}`)
	wantRefusal(t, res, "RUN_UNSETTLED: process cannot be checked")
	res = mustRetentionDispatch(t, "archive-settled",
		`{"status_name":"ABORTED","jobs":[{"name":"j1","has_request":true,"has_exit":true,
		  "processes":[{"pid":111,"pid_valid":true,"probe":"dead"}]}]}`)
	wantOK(t, res)
}

// ---- archive-index-plan --------------------------------------------------------

func TestArchiveIndexPlanAssemblesSkeleton(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-index-plan",
		`{"source_directory":"/runs/r","run_id":"r1","revision":"rev0","runtime_identity":"ident",
		  "status":{"status":"ABORTED","nested":[1,2.5]},"database_logical_sha256":"logical"}`)
	wantOK(t, res)
	skeleton := res["skeleton"].(map[string]any)
	if skeleton["format"] != "formal-gates-archive-v1" || skeleton["source_directory"] != "/runs/r" ||
		skeleton["run_id"] != "r1" || skeleton["revision"] != "rev0" ||
		skeleton["runtime_identity"] != "ident" || skeleton["database_logical_sha256"] != "logical" {
		t.Fatalf("skeleton assembly wrong: %v", skeleton)
	}
	// The status value passes through as raw JSON, number fidelity included.
	if string(skeleton["status"].(json.RawMessage)) != `{"status":"ABORTED","nested":[1,2.5]}` {
		t.Fatalf("status must pass through verbatim, got %s", skeleton["status"])
	}
	// No files field: the proofs join the skeleton on the Python side.
	if _, has := skeleton["files"]; has {
		t.Fatalf("skeleton must not carry files, got %v", skeleton)
	}
}

// ---- archive-verify --------------------------------------------------------

const verifyFactsOK = `"duplicates":false,"format":"formal-gates-archive-v1",
	"status_name":"ABORTED","names_match":true,`

func TestArchiveVerifyFactsRefusals(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-verify",
		`{"duplicates":true,"format":"formal-gates-archive-v1","status_name":"ABORTED",
		  "names_match":true,"entries":[]}`)
	wantRefusal(t, res, "ARCHIVE_INVALID: duplicate members")
	for _, facts := range []string{
		// A missing or non-string format/status normalizes to null.
		`"duplicates":false,"format":null,"status_name":"ABORTED","names_match":true`,
		`"duplicates":false,"format":"other-v0","status_name":"ABORTED","names_match":true`,
		`"duplicates":false,"format":"formal-gates-archive-v1","status_name":"PAUSED","names_match":true`,
		`"duplicates":false,"format":"formal-gates-archive-v1","status_name":null,"names_match":true`,
	} {
		res := mustRetentionDispatch(t, "archive-verify", "{"+facts+`,"entries":[]}`)
		wantRefusal(t, res, "ARCHIVE_INVALID: wrong format or nonterminal run")
	}
	res = mustRetentionDispatch(t, "archive-verify",
		`{"duplicates":false,"format":"formal-gates-archive-v1","status_name":"ABORTED","names_match":false,
		  "entries":[]}`)
	wantRefusal(t, res, "ARCHIVE_INVALID: missing or extra members")
}

func TestArchiveVerifyFactsBeatEntries(t *testing.T) {
	// A failing fact wins even when the same batch carries differing content.
	res := mustRetentionDispatch(t, "archive-verify",
		`{"duplicates":false,"format":"other-v0","status_name":"ABORTED","names_match":false,
		  "entries":[{"name":"a.txt","actual":{"sha256":"aa","size":10},"expected":{"sha256":"bb","size":20}}]}`)
	wantRefusal(t, res, "ARCHIVE_INVALID: wrong format or nonterminal run")
}

func TestArchiveVerifyUnsafePathBeforeContentPerEntry(t *testing.T) {
	for _, name := range []string{"/etc/passwd", "a/../b"} {
		res := mustRetentionDispatch(t, "archive-verify",
			"{ "+verifyFactsOK+`"entries":[{"name":"`+name+`",
			  "actual":{"sha256":"aa","size":10},"expected":{"sha256":"aa","size":10}}]}`)
		wantRefusal(t, res, "ARCHIVE_INVALID: unsafe member path")
	}
	// A backslash anywhere in the member name is unsafe; in JSON source it is
	// written as an escaped backslash.
	res := mustRetentionDispatch(t, "archive-verify",
		"{ "+verifyFactsOK+`"entries":[{"name":"win\\name",
		  "actual":{"sha256":"aa","size":10},"expected":{"sha256":"aa","size":10}}]}`)
	wantRefusal(t, res, "ARCHIVE_INVALID: unsafe member path")
	// The first entry decides: its wrong content is reported even though a
	// later entry has an unsafe path.
	res = mustRetentionDispatch(t, "archive-verify",
		"{ "+verifyFactsOK+`"entries":[{"name":"good.txt","actual":{"sha256":"bb","size":20},"expected":{"sha256":"aa","size":10}},
		  {"name":"bad\\path","actual":{"sha256":"aa","size":10},"expected":{"sha256":"aa","size":10}}]}`)
	wantRefusal(t, res, "ARCHIVE_INVALID: content differs: good.txt")
	// Typed comparison: a size difference alone refuses.
	res = mustRetentionDispatch(t, "archive-verify",
		"{ "+verifyFactsOK+`"entries":[{"name":"a.txt","actual":{"sha256":"aa","size":10},"expected":{"sha256":"aa","size":11}}]}`)
	wantRefusal(t, res, "ARCHIVE_INVALID: content differs: a.txt")
}

func TestArchiveVerifyFullPassReturnsOKOnly(t *testing.T) {
	// Success returns no index: the caller keeps its own parsed manifest.
	res := mustRetentionDispatch(t, "archive-verify",
		"{ "+verifyFactsOK+`"entries":[{"name":"z.txt","actual":{"sha256":"aa","size":10},"expected":{"sha256":"aa","size":10}},
		  {"name":"a.bin","actual":{"sha256":"cc","size":3},"expected":{"sha256":"cc","size":3}}]}`)
	wantOK(t, res)
	if len(res) != 1 {
		t.Fatalf("a passing verify carries only ok, got %v", res)
	}
}

// ---- archive-stale --------------------------------------------------------

func TestArchiveStaleFactsRulesInOrder(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-stale",
		`{"has_new":true,"has_missing":true,"allow_missing":true}`)
	wantRefusal(t, res, "ARCHIVE_STALE: new files appeared; no deletion")
	res = mustRetentionDispatch(t, "archive-stale",
		`{"has_new":false,"has_missing":true,"allow_missing":false}`)
	wantRefusal(t, res, "ARCHIVE_STALE: source files missing")
	res = mustRetentionDispatch(t, "archive-stale",
		`{"has_new":false,"has_missing":true,"allow_missing":true}`)
	wantOK(t, res)
}

func TestArchiveStaleEntriesInOrder(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-stale",
		`{"entries":[{"name":"z.txt","kind":"file",
		  "actual_proof":{"sha256":"bb","size":20},"expected_proof":{"sha256":"aa","size":10}},
		 {"name":"a.txt","kind":"file",
		  "actual_proof":{"sha256":"aa","size":10},"expected_proof":{"sha256":"aa","size":10}}]}`)
	wantRefusal(t, res, "ARCHIVE_STALE: source differs: z.txt")
	// The database row compares the logical-hash strings, never the byte
	// proofs that ride along unused.
	res = mustRetentionDispatch(t, "archive-stale",
		`{"entries":[{"name":"checkpoints.sqlite","kind":"database",
		  "actual_proof":{"sha256":"live","size":9},"expected_proof":{"sha256":"backup","size":9},
		  "actual_db":"logical-live","expected_db":"logical-index"}]}`)
	wantRefusal(t, res, "ARCHIVE_STALE: source differs: checkpoints.sqlite")
	res = mustRetentionDispatch(t, "archive-stale",
		`{"entries":[{"name":"checkpoints.sqlite","kind":"database",
		  "actual_proof":{"sha256":"live","size":9},"expected_proof":{"sha256":"backup","size":9},
		  "actual_db":"logical","expected_db":"logical"}]}`)
	wantOK(t, res)
	// A row with the pair its kind does not name cannot compare equal.
	res = mustRetentionDispatch(t, "archive-stale",
		`{"entries":[{"name":"f","kind":"file"}]}`)
	wantRefusal(t, res, "ARCHIVE_STALE: source differs: f")
	if _, err := retentionDispatch("archive-stale",
		[]byte(`{"entries":[{"name":"x","kind":"socket"}]}`)); err == nil {
		t.Fatal("an unknown kind is a bridge failure, not a verdict")
	}
}

// ---- archive-projection --------------------------------------------------------

func TestArchiveProjectionPreCheckPhaseFiresChangedWithoutStatus(t *testing.T) {
	// Without a status the op is the pre-check archived_status runs before
	// opening the zip: ARCHIVE_CHANGED needs no verified manifest.
	res := mustRetentionDispatch(t, "archive-projection",
		`{"archive":"/keep/run.zip","archive_proof":{"sha256":"bb","size":20},
		  "marker":{"archive_proof":{"sha256":"aa","size":10}}}`)
	wantRefusal(t, res, "ARCHIVE_CHANGED: retained archive missing or altered")
	res = mustRetentionDispatch(t, "archive-projection",
		`{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		  "marker":{"archive_proof":{"sha256":"aa","size":10}}}`)
	wantOK(t, res)
	res = mustRetentionDispatch(t, "archive-projection", `{"archive":"/keep/run.zip"}`)
	wantOK(t, res)
}

const projectionPayload = `"source_directory":"/runs/r","file_count":2,"archive":"/keep/run.zip",
	"archive_proof":{"sha256":"aa","size":10},"status":{"status":"ABORTED","run_id":"r1","revision":"rev0"},`

func TestArchiveProjectionWithoutMarker(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-projection",
		"{"+projectionPayload+`"marker":null,"directory":""}`)
	wantOK(t, res)
	status := res["status"].(map[string]any)
	if status["archived"] != true || status["archive_verified"] != true ||
		status["archive"] != "/keep/run.zip" || status["source_directory"] != "/runs/r" ||
		status["file_count"] != 2 || status["model_calls"] != 0 || status["run_id"] != "r1" {
		t.Fatalf("projection wrong: %v", status)
	}
	if _, has := status["cleanup_complete"]; has {
		t.Fatalf("no marker must not add cleanup_complete, got %v", status)
	}
}

func TestArchiveProjectionBindingTriple(t *testing.T) {
	for _, marker := range []string{
		`{"run_id":"other","revision":"rev0"}`,
		`{"run_id":"r1","revision":"rev1"}`,
		`{"run_id":null,"revision":"rev0"}`,
	} {
		res := mustRetentionDispatch(t, "archive-projection",
			"{"+projectionPayload+`"marker":`+marker+`,"directory":"/runs/r"}`)
		wantRefusal(t, res, "ARCHIVE_BINDING: tombstone does not match archive")
	}
	res := mustRetentionDispatch(t, "archive-projection",
		"{"+projectionPayload+`"marker":{"run_id":"r1","revision":"rev0"},"directory":"/elsewhere"}`)
	wantRefusal(t, res, "ARCHIVE_BINDING: tombstone does not match archive")
}

func TestArchiveProjectionCleanupCompleteVariants(t *testing.T) {
	cases := []struct {
		marker string
		want   any
	}{
		{`{"run_id":"r1","revision":"rev0"}`, false},
		{`{"run_id":"r1","revision":"rev0","cleanup_complete":true}`, true},
		{`{"run_id":"r1","revision":"rev0","cleanup_complete":7}`, jsonNumber("7")},
	}
	for _, c := range cases {
		res := mustRetentionDispatch(t, "archive-projection",
			"{"+projectionPayload+`"marker":`+c.marker+`,"directory":"/runs/r"}`)
		wantOK(t, res)
		if got := res["status"].(map[string]any)["cleanup_complete"]; got != c.want {
			t.Fatalf("cleanup_complete wrong: got %v want %v", got, c.want)
		}
	}
}

// ---- prune-admission --------------------------------------------------------

func TestPruneAdmissionRulesAndPriority(t *testing.T) {
	cases := []struct{ payload, refusal string }{
		{`{"runs_dir":"/harness/runs","directory":"/harness/runs/r1","directory_name":"r1",
		   "run_id":"r1","archive":"/keep/run.zip"}`, ""},
		{`{"runs_dir":"/harness/runs","directory":"/harness/runs/r1","directory_name":"r1",
		   "run_id":"r1","archive":"/harness/runs/r1/x.zip"}`, "ARCHIVE_PATH: archive must be outside the run"},
		{`{"runs_dir":"/harness/runs","directory":"/harness/runs/r1","directory_name":"r1",
		   "run_id":"r1","archive":"/harness/runs/r1"}`, "ARCHIVE_PATH: archive must be outside the run"},
		{`{"runs_dir":"/harness/runs","directory":"/harness/runs/r1","directory_name":"r1",
		   "run_id":"r1","archive":"/harness/runs/r1-2/x.zip"}`, ""},
		{`{"runs_dir":"/harness/runs","directory":"/elsewhere/r1","directory_name":"r1",
		   "run_id":"r1","archive":"/keep/run.zip"}`,
			"PRUNE_PATH: only this installation's run directories are eligible"},
		{`{"runs_dir":"/harness/runs","directory":"/harness/runs/r1","directory_name":"r1",
		   "run_id":"other","archive":"/keep/run.zip"}`,
			"PRUNE_PATH: only this installation's run directories are eligible"},
		{`{"runs_dir":"/harness/runs","directory":"/elsewhere/r1","directory_name":"r1",
		   "run_id":"other","archive":"/elsewhere/r1/x.zip"}`,
			"PRUNE_PATH: only this installation's run directories are eligible"},
	}
	for _, c := range cases {
		res := mustRetentionDispatch(t, "prune-admission", c.payload)
		if c.refusal == "" {
			wantOK(t, res)
			continue
		}
		wantRefusal(t, res, c.refusal)
	}
}

// ---- prune-bind --------------------------------------------------------

func TestPruneBindTriple(t *testing.T) {
	cases := []struct{ payload, refusal string }{
		{`{"directory":"/harness/runs/r1","run_id":"r1","revision":"rev0",
		   "source_directory":"/harness/runs/r1","index_run_id":"r1","index_revision":"rev0"}`, ""},
		{`{"directory":"/harness/runs/r1","run_id":"r1","revision":"rev0",
		   "source_directory":"/elsewhere","index_run_id":"r1","index_revision":"rev0"}`,
			"ARCHIVE_BINDING: wrong run/revision"},
		{`{"directory":"/harness/runs/r1","run_id":"r1","revision":"rev0",
		   "source_directory":"/harness/runs/r1","index_run_id":"other","index_revision":"rev0"}`,
			"ARCHIVE_BINDING: wrong run/revision"},
		{`{"directory":"/harness/runs/r1","run_id":"r1","revision":"rev0",
		   "source_directory":"/harness/runs/r1","index_run_id":"r1","index_revision":"rev1"}`,
			"ARCHIVE_BINDING: wrong run/revision"},
		// The wrapper normalizes a non-string identity to null; a null never
		// matches the run's own strings.
		{`{"directory":"/harness/runs/r1","run_id":"r1","revision":"rev0",
		   "source_directory":"/harness/runs/r1","index_run_id":null,"index_revision":"rev0"}`,
			"ARCHIVE_BINDING: wrong run/revision"},
	}
	for _, c := range cases {
		res := mustRetentionDispatch(t, "prune-bind", c.payload)
		if c.refusal == "" {
			wantOK(t, res)
			continue
		}
		wantRefusal(t, res, c.refusal)
	}
}

// ---- prune-marker --------------------------------------------------------

func TestPruneMarkerBinding(t *testing.T) {
	base := `"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		"marker_archive":"/keep/run.zip","marker_archive_proof":{"sha256":"aa","size":10}}`
	res := mustRetentionDispatch(t, "prune-marker", "{"+base)
	wantOK(t, res)
	res = mustRetentionDispatch(t, "prune-marker",
		`{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		  "marker_archive":"/other/run.zip","marker_archive_proof":{"sha256":"aa","size":10}}`)
	wantRefusal(t, res, "ARCHIVE_BINDING: use the original cleanup archive")
	res = mustRetentionDispatch(t, "prune-marker",
		`{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		  "marker_archive":"/keep/run.zip","marker_archive_proof":{"sha256":"bb","size":20}}`)
	wantRefusal(t, res, "ARCHIVE_BINDING: use the original cleanup archive")
}

// ---- prune-plan --------------------------------------------------------

func prunePlanPayload(markerExists bool) string {
	marker := "null"
	if markerExists {
		marker = "true"
	}
	return `"entries":[{"name":"manifest.json","size":10},{"name":"a/b/z.txt","size":20},
		 {"name":"runner.lock","size":10},{"name":"a/y.txt","size":20},
		 {"name":"b.txt","size":10},{"name":"a/b.txt","size":20},{"name":"archive.json","size":10}],
		"run_id":"r1","revision":"rev0","terminal_status":"ABORTED",
		"archive":"/keep/run.zip","marker_exists":` + marker + "}"
}

func TestPrunePlanKeepFilterAndRemovalSort(t *testing.T) {
	res := mustRetentionDispatch(t, "prune-plan", "{"+prunePlanPayload(false))
	wantOK(t, res)
	remove := res["remove"].([]string)
	// Depth descending, then same-depth alphabetical; KEEP names never move.
	want := []string{"a/b/z.txt", "a/b.txt", "a/y.txt", "b.txt"}
	if len(remove) != len(want) {
		t.Fatalf("remove wrong: %v", remove)
	}
	for i := range want {
		if remove[i] != want[i] {
			t.Fatalf("remove order wrong: got %v want %v", remove, want)
		}
	}
	report := res["report"].(map[string]any)
	if report["files_to_remove"] != 4 || report["bytes_to_remove"] != int64(70) ||
		report["terminal_status"] != "ABORTED" || report["run_id"] != "r1" ||
		report["archive"] != "/keep/run.zip" || report["model_calls"] != 0 ||
		report["status"] != "PRUNE_PREVIEW" {
		t.Fatalf("report counters wrong: %v", report)
	}
	plan := res["marker"].(map[string]any)
	if plan["format"] != "formal-gates-prune-v1" || plan["cleanup_complete"] != false ||
		plan["run_id"] != "r1" || plan["revision"] != "rev0" || plan["archive"] != "/keep/run.zip" {
		t.Fatalf("tombstone plan wrong: %v", plan)
	}
	// The plan carries no archive_proof: the caller owns the hashing and
	// merges the proof when the tombstone is written.
	if _, has := plan["archive_proof"]; has {
		t.Fatalf("plan must not carry archive_proof, got %v", plan)
	}
}

func TestPrunePlanExistingMarkerHasNoNewPlan(t *testing.T) {
	res := mustRetentionDispatch(t, "prune-plan", "{"+prunePlanPayload(true))
	wantOK(t, res)
	if res["marker"] != nil {
		t.Fatalf("an existing marker must not gain a plan, got %v", res["marker"])
	}
}

// ---- shared helpers --------------------------------------------------------

func TestPathHelpers(t *testing.T) {
	if !pathWithin("/a/b", "/a/b/c") || !pathWithin("/a/b", "/a/b") {
		t.Fatalf("containment must include equality")
	}
	if pathWithin("/a/b", "/a/bc") {
		t.Fatalf("component containment is not a string prefix")
	}
	if parentDirectory("/harness/runs/r1") != "/harness/runs" {
		t.Fatalf("parent wrong: %s", parentDirectory("/harness/runs/r1"))
	}
	if !unsafeMemberPath("/etc/passwd") || !unsafeMemberPath("a/../b") ||
		!unsafeMemberPath(`win\name`) || unsafeMemberPath("run/a.txt") {
		t.Fatalf("member path safety rule wrong")
	}
}
