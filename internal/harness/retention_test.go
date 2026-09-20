package harness

import (
	"fmt"
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
		  "processes":[{"pid":7,"probe":"alive"}]}]}`)
	wantRefusal(t, res, "RUN_UNSETTLED: native-op has no exit receipt; no cleanup")
	for _, bad := range []string{`0`, `-5`, `3.5`, `"7"`, `null`, `true`} {
		res := mustRetentionDispatch(t, "archive-settled",
			`{"status_name":"ABORTED","jobs":[{"name":"j1","has_request":false,"has_exit":false,
			  "processes":[{"pid":`+bad+`,"probe":"dead"}]}]}`)
		wantRefusal(t, res, "RUN_UNSETTLED: invalid process identity")
	}
	res = mustRetentionDispatch(t, "archive-settled",
		`{"status_name":"ABORTED","jobs":[{"name":"j1","has_request":true,"has_exit":true,
		  "processes":[{"pid":111,"probe":"dead"},{"pid":4242,"probe":"alive"}]}]}`)
	wantRefusal(t, res, "RUN_UNSETTLED: PID 4242 still exists; no cleanup")
	res = mustRetentionDispatch(t, "archive-settled",
		`{"status_name":"ABORTED","jobs":[{"name":"j1","has_request":true,"has_exit":true,
		  "processes":[{"pid":999,"probe":"permission"}]}]}`)
	wantRefusal(t, res, "RUN_UNSETTLED: process cannot be checked")
	res = mustRetentionDispatch(t, "archive-settled",
		`{"status_name":"ABORTED","jobs":[{"name":"j1","has_request":true,"has_exit":true,
		  "processes":[{"pid":111,"probe":"dead"}]}]}`)
	wantOK(t, res)
}

// ---- archive-index-plan --------------------------------------------------------

func TestArchiveIndexPlanExistsRefusalAndAssembly(t *testing.T) {
	base := `"destination_exists":%v,"source_directory":"/runs/r","run_id":"r1","revision":"rev0",
		"runtime_identity":"ident","status":{"status":"ABORTED"},
		"files":{"checkpoints.sqlite":{"sha256":"live","size":9},"a.txt":{"sha256":"aa","size":10}},
		"database_logical_sha256":"logical",
		"checkpoints_proof":{"sha256":"backup","size":9}`
	res := mustRetentionDispatch(t, "archive-index-plan", "{"+fmt.Sprintf(base, true)+"}")
	wantRefusal(t, res, "ARCHIVE_EXISTS: use a new destination; not overwritten")
	res = mustRetentionDispatch(t, "archive-index-plan", "{"+fmt.Sprintf(base, false)+"}")
	wantOK(t, res)
	index := res["index"].(map[string]any)
	files := index["files"].(map[string]any)
	db, _ := files["checkpoints.sqlite"].(map[string]any)
	if db["sha256"] != "backup" {
		t.Fatalf("backup proof must replace the byte proof, got %v", db)
	}
	if index["format"] != "formal-gates-archive-v1" || index["status"].(map[string]any)["status"] != "ABORTED" {
		t.Fatalf("index assembly wrong: %v", index)
	}
}

// ---- archive-verify --------------------------------------------------------

func TestArchiveVerifyPhaseOneDuplicates(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-verify",
		`{"names":["archive-manifest.json","run/a","run/a"]}`)
	wantRefusal(t, res, "ARCHIVE_INVALID: duplicate members")
	res = mustRetentionDispatch(t, "archive-verify", `{"names":["archive-manifest.json","run/a"]}`)
	wantOK(t, res)
}

func TestArchiveVerifyWrongFormatOrNonterminal(t *testing.T) {
	names := `"names":["archive-manifest.json","run/a.txt"],"member_proofs":{"a.txt":{"sha256":"aa","size":10}},`
	for _, index := range []string{
		`{"status":{"status":"ABORTED"},"files":{"a.txt":{"sha256":"aa","size":10}}}`,
		`{"format":"other-v0","status":{"status":"ABORTED"},"files":{"a.txt":{"sha256":"aa","size":10}}}`,
		`{"format":"formal-gates-archive-v1","status":{"status":"PAUSED"},"files":{"a.txt":{"sha256":"aa","size":10}}}`,
		`{"format":"formal-gates-archive-v1","status":{"status":5},"files":{"a.txt":{"sha256":"aa","size":10}}}`,
	} {
		res := mustRetentionDispatch(t, "archive-verify", "{"+names+`"index":`+index+"}")
		wantRefusal(t, res, "ARCHIVE_INVALID: wrong format or nonterminal run")
	}
}

func TestArchiveVerifyMemberSetBeatsSafetyAndContent(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-verify",
		`{"names":["archive-manifest.json","run/a.txt"],
		  "index":{"format":"formal-gates-archive-v1","status":{"status":"ABORTED"},
		           "files":{"../evil":{"sha256":"aa","size":10},"a.txt":{"sha256":"bb","size":20}}},
		  "member_proofs":{"a.txt":{"sha256":"aa","size":10}}}`)
	wantRefusal(t, res, "ARCHIVE_INVALID: missing or extra members")
	res = mustRetentionDispatch(t, "archive-verify",
		`{"names":["archive-manifest.json","run/a.txt","run/extra"],
		  "index":{"format":"formal-gates-archive-v1","status":{"status":"ABORTED"},
		           "files":{"a.txt":{"sha256":"aa","size":10}}},
		  "member_proofs":{"a.txt":{"sha256":"aa","size":10}}}`)
	wantRefusal(t, res, "ARCHIVE_INVALID: missing or extra members")
}

func TestArchiveVerifyUnsafePathBeforeContentPerName(t *testing.T) {
	for _, name := range []string{"/etc/passwd", "a/../b"} {
		member := "run/" + name
		res := mustRetentionDispatch(t, "archive-verify",
			`{"names":["archive-manifest.json","`+member+`"],
			  "index":{"format":"formal-gates-archive-v1","status":{"status":"ABORTED"},
			           "files":{"`+name+`":{"sha256":"aa","size":10}}},
			  "member_proofs":{"`+name+`":{"sha256":"aa","size":10}}}`)
		wantRefusal(t, res, "ARCHIVE_INVALID: unsafe member path")
	}
	// A backslash anywhere in the member name is unsafe; in JSON source it is
	// written as an escaped backslash.
	res := mustRetentionDispatch(t, "archive-verify",
		`{"names":["archive-manifest.json","run/win\\name"],
		  "index":{"format":"formal-gates-archive-v1","status":{"status":"ABORTED"},
		           "files":{"win\\name":{"sha256":"aa","size":10}}},
		  "member_proofs":{"win\\name":{"sha256":"aa","size":10}}}`)
	wantRefusal(t, res, "ARCHIVE_INVALID: unsafe member path")
	// The first manifest entry decides: its wrong content is reported even
	// though a later entry has an unsafe path.
	res = mustRetentionDispatch(t, "archive-verify",
		`{"names":["archive-manifest.json","run/good.txt","run/bad\\path"],
		  "index":{"format":"formal-gates-archive-v1","status":{"status":"ABORTED"},
		           "files":{"good.txt":{"sha256":"aa","size":10},"bad\\path":{"sha256":"aa","size":10}}},
		  "member_proofs":{"good.txt":{"sha256":"bb","size":20},"bad\\path":{"sha256":"aa","size":10}}}`)
	wantRefusal(t, res, "ARCHIVE_INVALID: content differs: good.txt")
}

func TestArchiveVerifyFullPassReturnsIndex(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-verify",
		`{"names":["archive-manifest.json","run/z.txt","run/a.bin"],
		  "index":{"format":"formal-gates-archive-v1","run_id":"r1",
		           "status":{"status":"HARNESS_COMPLETE","run_id":"r1"},
		           "files":{"z.txt":{"sha256":"aa","size":10},"a.bin":{"sha256":"cc","size":3.5}},
		           "database_logical_sha256":"logical"},
		  "member_proofs":{"z.txt":{"sha256":"aa","size":10},"a.bin":{"sha256":"cc","size":3.5}}}`)
	wantOK(t, res)
	index := res["index"].(map[string]any)
	files := index["files"].(map[string]any)
	if _, ok := files["a.bin"].(map[string]any); !ok {
		t.Fatalf("index files must pass through, got %v", files)
	}
	if files["a.bin"].(map[string]any)["size"] != jsonNumber("3.5") {
		t.Fatalf("float literal must keep its number fidelity, got %v", files["a.bin"])
	}
}

// ---- archive-stale --------------------------------------------------------

func TestArchiveStaleRulesInOrder(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-stale",
		`{"current":{"new.txt":{"sha256":"aa","size":10}},"files":{},"allow_missing":true,
		  "database_logical_sha256":"logical","live_database_logical_sha256":null}`)
	wantRefusal(t, res, "ARCHIVE_STALE: new files appeared; no deletion")
	res = mustRetentionDispatch(t, "archive-stale",
		`{"current":{"manifest.json":{"sha256":"aa","size":10}},
		  "files":{"manifest.json":{"sha256":"aa","size":10},"jobs/x.json":{"sha256":"bb","size":20}},
		  "allow_missing":false,"database_logical_sha256":"logical","live_database_logical_sha256":null}`)
	wantRefusal(t, res, "ARCHIVE_STALE: source files missing")
	res = mustRetentionDispatch(t, "archive-stale",
		`{"current":{"manifest.json":{"sha256":"aa","size":10}},
		  "files":{"manifest.json":{"sha256":"aa","size":10},"jobs/x.json":{"sha256":"bb","size":20}},
		  "allow_missing":true,"database_logical_sha256":"logical","live_database_logical_sha256":null}`)
	wantOK(t, res)
	// Iteration order names the first differing entry as walked, not sorted.
	res = mustRetentionDispatch(t, "archive-stale",
		`{"current":{"z.txt":{"sha256":"bb","size":20},"a.txt":{"sha256":"aa","size":10}},
		  "files":{"a.txt":{"sha256":"aa","size":10},"z.txt":{"sha256":"aa","size":10}},
		  "allow_missing":false,"database_logical_sha256":"logical","live_database_logical_sha256":null}`)
	wantRefusal(t, res, "ARCHIVE_STALE: source differs: z.txt")
}

func TestArchiveStaleDatabaseLogicalSpecialCase(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-stale",
		`{"current":{"checkpoints.sqlite":{"sha256":"backup","size":9}},
		  "files":{"checkpoints.sqlite":{"sha256":"backup","size":9}},
		  "allow_missing":false,"database_logical_sha256":"logical-index",
		  "live_database_logical_sha256":"logical-live"}`)
	wantRefusal(t, res, "ARCHIVE_STALE: source differs: checkpoints.sqlite")
	res = mustRetentionDispatch(t, "archive-stale",
		`{"current":{"checkpoints.sqlite":{"sha256":"live","size":9}},
		  "files":{"checkpoints.sqlite":{"sha256":"backup","size":9}},
		  "allow_missing":false,"database_logical_sha256":"logical",
		  "live_database_logical_sha256":"logical"}`)
	wantOK(t, res)
}

// ---- archive-projection --------------------------------------------------------

func TestArchiveProjectionPreCheckPhaseFiresChangedWithoutIndex(t *testing.T) {
	// Without an index the op is the pre-check archived_status runs before
	// opening the zip: ARCHIVE_CHANGED needs no verified manifest.
	res := mustRetentionDispatch(t, "archive-projection",
		`{"archive":"/keep/run.zip","archive_proof":{"sha256":"bb","size":20},
		  "marker":{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		            "run_id":"r1","revision":"rev0"},"directory":"/runs/r"}`)
	wantRefusal(t, res, "ARCHIVE_CHANGED: retained archive missing or altered")
	res = mustRetentionDispatch(t, "archive-projection",
		`{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		  "marker":{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		            "run_id":"r1","revision":"rev0"},"directory":"/runs/r"}`)
	wantOK(t, res)
	res = mustRetentionDispatch(t, "archive-projection",
		`{"archive":"/keep/run.zip","marker":null,"directory":""}`)
	wantOK(t, res)
}

func TestArchiveProjectionWithoutMarker(t *testing.T) {
	res := mustRetentionDispatch(t, "archive-projection",
		`{"index":{"source_directory":"/runs/r",
		          "status":{"status":"ABORTED","run_id":"r1","revision":"rev0"},
		          "files":{"a.txt":{"sha256":"aa","size":10},"checkpoints.sqlite":{"sha256":"bb","size":9}}},
		  "archive":"/keep/run.zip","marker":null,"directory":""}`)
	wantOK(t, res)
	status := res["status"].(map[string]any)
	if status["archived"] != true || status["archive_verified"] != true ||
		status["archive"] != "/keep/run.zip" || status["source_directory"] != "/runs/r" ||
		status["file_count"] != 2 || status["model_calls"] != 0 {
		t.Fatalf("projection wrong: %v", status)
	}
	if _, has := status["cleanup_complete"]; has {
		t.Fatalf("no marker must not add cleanup_complete, got %v", status)
	}
}

func TestArchiveProjectionChangedAndBinding(t *testing.T) {
	index := `"index":{"source_directory":"/runs/r",
	          "status":{"status":"ABORTED","run_id":"r1","revision":"rev0"},
	          "files":{"a.txt":{"sha256":"aa","size":10}}},`
	res := mustRetentionDispatch(t, "archive-projection",
		"{"+index+`"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		  "marker":{"archive":"/keep/run.zip","archive_proof":{"sha256":"bb","size":20},
		            "run_id":"r1","revision":"rev0"},"directory":"/runs/r"}`)
	wantRefusal(t, res, "ARCHIVE_CHANGED: retained archive missing or altered")
	for _, marker := range []string{
		`{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},"run_id":"other","revision":"rev0"}`,
		`{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},"run_id":"r1","revision":"rev1"}`,
	} {
		res := mustRetentionDispatch(t, "archive-projection",
			"{"+index+`"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
			  "marker":`+marker+`,"directory":"/runs/r"}`)
		wantRefusal(t, res, "ARCHIVE_BINDING: tombstone does not match archive")
	}
	res = mustRetentionDispatch(t, "archive-projection",
		"{"+index+`"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		  "marker":{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		            "run_id":"r1","revision":"rev0"},"directory":"/elsewhere"}`)
	wantRefusal(t, res, "ARCHIVE_BINDING: tombstone does not match archive")
}

func TestArchiveProjectionCleanupCompleteVariants(t *testing.T) {
	index := `"index":{"source_directory":"/runs/r",
	          "status":{"status":"ABORTED","run_id":"r1","revision":"rev0"},
	          "files":{"a.txt":{"sha256":"aa","size":10}}},`
	cases := []struct {
		marker string
		want   any
	}{
		{`{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},"run_id":"r1","revision":"rev0"}`, false},
		{`{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},"run_id":"r1","revision":"rev0","cleanup_complete":true}`, true},
		{`{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},"run_id":"r1","revision":"rev0","cleanup_complete":7}`, jsonNumber("7")},
	}
	for _, c := range cases {
		res := mustRetentionDispatch(t, "archive-projection",
			"{"+index+`"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
			  "marker":`+c.marker+`,"directory":"/runs/r"}`)
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
		// A numeric run id is never equal to the string one.
		{`{"directory":"/harness/runs/r1","run_id":"r1","revision":"rev0",
		   "source_directory":"/harness/runs/r1","index_run_id":7,"index_revision":"rev0"}`,
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

// ---- prune-plan --------------------------------------------------------

const prunePlanBase = `"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
	"files":{"manifest.json":{"sha256":"aa","size":10},"a/b/z.txt":{"sha256":"bb","size":20},
	         "runner.lock":{"sha256":"aa","size":10},"a/y.txt":{"sha256":"bb","size":20},
	         "b.txt":{"sha256":"aa","size":10},"a/b.txt":{"sha256":"bb","size":20},
	         "archive.json":{"sha256":"aa","size":10}},
	"database_logical_sha256":"logical","live_database_logical_sha256":null,
	"run_id":"r1","revision":"rev0","terminal_status":"ABORTED","directory":"/harness/runs/r1"`

const prunePlanCurrent = `"current":{"manifest.json":{"sha256":"aa","size":10},
	"a/b/z.txt":{"sha256":"bb","size":20},"runner.lock":{"sha256":"aa","size":10},
	"a/y.txt":{"sha256":"bb","size":20},"b.txt":{"sha256":"aa","size":10},
	"a/b.txt":{"sha256":"bb","size":20},"archive.json":{"sha256":"aa","size":10}}`

func TestPrunePlanKeepFilterAndRemovalSort(t *testing.T) {
	res := mustRetentionDispatch(t, "prune-plan",
		`{"marker":null,"allow_missing":false,`+prunePlanBase+","+prunePlanCurrent+"}")
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
	if report["files_to_remove"] != 4 || report["bytes_to_remove"] != jsonNumber("70") {
		t.Fatalf("report counters wrong: %v", report)
	}
	plan := res["marker"].(map[string]any)
	if plan["format"] != "formal-gates-prune-v1" || plan["cleanup_complete"] != false ||
		plan["run_id"] != "r1" || plan["revision"] != "rev0" {
		t.Fatalf("tombstone plan wrong: %v", plan)
	}
}

func TestPrunePlanMarkerBindingBeforeStaleness(t *testing.T) {
	res := mustRetentionDispatch(t, "prune-plan",
		`{"marker":{"archive":"/other/run.zip","archive_proof":{"sha256":"aa","size":10}},
		  "allow_missing":true,`+prunePlanBase+`,
		  "current":{"surprise.txt":{"sha256":"aa","size":10},"manifest.json":{"sha256":"aa","size":10},
		             "runner.lock":{"sha256":"aa","size":10},"archive.json":{"sha256":"aa","size":10}}}`)
	wantRefusal(t, res, "ARCHIVE_BINDING: use the original cleanup archive")
	res = mustRetentionDispatch(t, "prune-plan",
		`{"marker":{"archive":"/keep/run.zip","archive_proof":{"sha256":"bb","size":20}},
		  "allow_missing":false,`+prunePlanBase+`,
		  "current":{"manifest.json":{"sha256":"aa","size":10},"runner.lock":{"sha256":"aa","size":10},
		             "archive.json":{"sha256":"aa","size":10}}}`)
	wantRefusal(t, res, "ARCHIVE_BINDING: use the original cleanup archive")
}

func TestPrunePlanMatchingMarkerHasNoNewPlan(t *testing.T) {
	res := mustRetentionDispatch(t, "prune-plan",
		`{"marker":{"archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		            "run_id":"r1","revision":"rev0","cleanup_complete":true},
		  "allow_missing":true,`+prunePlanBase+`,
		  "current":{"manifest.json":{"sha256":"aa","size":10},"a/b/z.txt":{"sha256":"bb","size":20},
		             "runner.lock":{"sha256":"aa","size":10},"b.txt":{"sha256":"aa","size":10},
		             "a/b.txt":{"sha256":"bb","size":20},"archive.json":{"sha256":"aa","size":10}}}`)
	wantOK(t, res)
	if res["marker"] != nil {
		t.Fatalf("an existing marker must not gain a plan, got %v", res["marker"])
	}
	remove := res["remove"].([]string)
	if len(remove) != 3 || remove[0] != "a/b/z.txt" || remove[1] != "a/b.txt" || remove[2] != "b.txt" {
		t.Fatalf("remove wrong: %v", remove)
	}
}

func TestPrunePlanEmptyMarkerObjectIsFalsy(t *testing.T) {
	res := mustRetentionDispatch(t, "prune-plan",
		`{"marker":{},"allow_missing":true,`+prunePlanBase+`,
		  "current":{"manifest.json":{"sha256":"aa","size":10},"runner.lock":{"sha256":"aa","size":10},
		             "archive.json":{"sha256":"aa","size":10}}}`)
	wantOK(t, res)
	// `if marker and ...` skips binding for an empty object, and it is not
	// None, so no tombstone plan is attached either.
	if res["marker"] != nil {
		t.Fatalf("empty marker must not gain a plan, got %v", res["marker"])
	}
}

func TestPrunePlanStalenessAndDatabaseSpecialCase(t *testing.T) {
	res := mustRetentionDispatch(t, "prune-plan",
		`{"marker":null,"allow_missing":false,`+prunePlanBase+`,
		  "current":{"new.txt":{"sha256":"aa","size":10},"manifest.json":{"sha256":"aa","size":10},
		             "runner.lock":{"sha256":"aa","size":10},"archive.json":{"sha256":"aa","size":10}}}`)
	wantRefusal(t, res, "ARCHIVE_STALE: new files appeared; no deletion")
	res = mustRetentionDispatch(t, "prune-plan",
		`{"marker":null,"allow_missing":false,
		  "archive":"/keep/run.zip","archive_proof":{"sha256":"aa","size":10},
		  "files":{"checkpoints.sqlite":{"sha256":"backup","size":9}},
		  "database_logical_sha256":"logical-index","live_database_logical_sha256":"logical-live",
		  "run_id":"r1","revision":"rev0","terminal_status":"ABORTED","directory":"/harness/runs/r1",
		  "current":{"checkpoints.sqlite":{"sha256":"backup","size":9}}}`)
	wantRefusal(t, res, "ARCHIVE_STALE: source differs: checkpoints.sqlite")
}

// ---- shared helpers --------------------------------------------------------

func TestPythonIntegerBoolIsRejected(t *testing.T) {
	// Documented divergence: CPython's isinstance(True, int) accepted a JSON
	// true as a pid; Go refuses it rather than emulating CPython.
	if _, ok := pythonInteger([]byte(`true`)); ok {
		t.Fatalf("true must not count as an integer pid")
	}
	for _, literal := range []string{`7`, `4242`, `-0`} {
		if _, ok := pythonInteger([]byte(literal)); !ok {
			t.Fatalf("%s must parse as an integer", literal)
		}
	}
	for _, literal := range []string{`3.5`, `3e0`, `"7"`, `null`, ``, `{}`} {
		if _, ok := pythonInteger([]byte(literal)); ok {
			t.Fatalf("%s must not count as an integer pid", literal)
		}
	}
}

func TestPythonEqualNumberSemantics(t *testing.T) {
	if !pythonEqual(jsonNumber("3"), jsonNumber("3.0")) {
		t.Fatalf("3 == 3.0 under Python equality")
	}
	if pythonEqual(jsonNumber("3"), jsonNumber("3.5")) {
		t.Fatalf("3 != 3.5")
	}
	if pythonEqual("3", jsonNumber("3")) {
		t.Fatalf("a string never equals a number")
	}
	if !pythonEqual(map[string]any{"sha256": "aa", "size": jsonNumber("10")},
		map[string]any{"size": jsonNumber("10.0"), "sha256": "aa"}) {
		t.Fatalf("proof dicts compare order-independently with numeric equality")
	}
}

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
}
