package harness

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The eleven terminal-retention judgements below are plain functions
// over their payloads: the caller (retention.py)
// keeps every filesystem fact — locking, hashing, the SQLite backup, zip
// reading and writing, process probing and the deletions — and ships ordered
// projections, so Python never re-derives a condition after the call.
//
// Inventories, member proofs and staleness comparisons never cross the bridge
// whole: they travel in batches of at most retentionChunk entries, or as small
// facts (bools, counts, strings), and every ordering is carried by arrays —
// batch order follows the walk, entry order follows the manifest's sorted
// files. Values are strictly typed; a refusal is a typed business outcome
// inside the value ({ok:false, refusal:...}) and retention.py raises the
// HarnessError family directly from its text. The one large payload that
// remains is canonical(index) when the manifest is written, a single copy of
// the index inside the format's capacity envelope.

// retentionChunk bounds the entry count of one batched call
// (archive-inventory, archive-verify, archive-stale). It keeps every request
// far below the bridge's fixed input ceiling; retention.py chunks with the
// same number (its CHUNK constant). prune-plan is deliberately exempt: its
// rows are name+size only (~80 bytes each), so one full call stays within the
// ceiling for runs with tens of thousands of files.
const retentionChunk = 2000

var terminalStatuses = map[string]bool{
	"HARNESS_COMPLETE": true,
	"DEMO_COMPLETE":    true,
	"ABORTED":          true,
}

// skipNames are never archived and never pruned; the check runs before the
// symlink judgement, exactly as the Python inventory walk does.
var skipNames = map[string]bool{
	"runner.lock":            true,
	"checkpoints.sqlite-wal": true,
	"checkpoints.sqlite-shm": true,
	"archive.json":           true,
}

// keepNames survive a prune: the tombstone plus the run's own identity.
var keepNames = map[string]bool{
	"manifest.json":           true,
	"requirement.md":          true,
	"reviewer-base.md":        true,
	"runner.lock":             true,
	"transport-adoption.json": true,
	"harness-result.json":     true,
	"archive.json":            true,
}

// proofValue is the typed {sha256, size} fact Python's hashing produces.
type proofValue struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func retentionRefusal(refusal string) map[string]any {
	return map[string]any{"ok": false, "refusal": refusal}
}
func retentionOK() map[string]any {
	return map[string]any{"ok": true}
}

// chunkLimit rejects an oversized batch: a caller that ships a whole
// inventory in one request is the capacity regression the chunk rule exists
// to prevent, so it fails loudly instead of silently approaching the bridge
// input ceiling.
func chunkLimit(operation string, count int) error {
	if count > retentionChunk {
		return fmt.Errorf("%s: %d entries exceed the %d-entry chunk limit", operation, count, retentionChunk)
	}
	return nil
}

// retentionDispatch routes one terminal-retention operation to its judgement.
func retentionDispatch(operation string, payload []byte) (any, error) {
	switch operation {
	case "archive-inventory":
		var q archiveInventoryInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return archiveInventoryOp(q)
	case "archive-destination":
		var q archiveDestinationInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return archiveDestination(q), nil
	case "archive-settled":
		var q archiveSettledInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return archiveSettled(q), nil
	case "archive-index-plan":
		var q archiveIndexPlanInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return archiveIndexPlan(q)
	case "archive-verify":
		var q archiveVerifyInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return archiveVerify(q)
	case "archive-stale":
		var q archiveStaleInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return archiveStale(q)
	case "archive-projection":
		var q archiveProjectionInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return archiveProjection(q)
	case "prune-admission":
		var q pruneAdmissionInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return pruneAdmission(q), nil
	case "prune-bind":
		var q pruneBindInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return pruneBind(q), nil
	case "prune-marker":
		var q pruneMarkerInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return pruneMarkerOp(q), nil
	case "prune-plan":
		var q prunePlanInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return prunePlanOp(q)
	}
	return nil, fmt.Errorf("unknown retention operation %q", operation)
}

// ---- 1. archive-inventory ---------------------------------------------------

type archiveInventoryInput struct {
	Entries []archiveInventoryEntry `json:"entries"`
}
type archiveInventoryEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// archiveInventoryOp applies the member admission rule over one batch of the
// walked entries (at most retentionChunk, already in sorted-rglob order).
// Order is preserved verbatim: SKIP wins over the symlink refusal, directories
// are skipped, and only plain files pass. The caller concatenates batches, so
// the global order is the batch order and the first refusing batch decides.
func archiveInventoryOp(q archiveInventoryInput) (map[string]any, error) {
	if err := chunkLimit("archive-inventory", len(q.Entries)); err != nil {
		return nil, err
	}
	files := []string{}
	for _, entry := range q.Entries {
		if skipNames[entry.Name] {
			continue
		}
		if entry.Kind == "symlink" {
			return retentionRefusal("ARCHIVE_UNSUPPORTED: symlink " + entry.Name + "; no files deleted"), nil
		}
		if entry.Kind == "dir" {
			continue
		}
		if entry.Kind != "file" {
			return retentionRefusal("ARCHIVE_UNSUPPORTED: non-regular entry " + entry.Name), nil
		}
		files = append(files, entry.Name)
	}
	return map[string]any{"ok": true, "files": files}, nil
}

// ---- 2. archive-destination ---------------------------------------------------

type archiveDestinationInput struct {
	Directory   string `json:"directory"`
	Destination string `json:"destination"`
}

// archiveDestination refuses a destination inside the run. The containment is
// per path COMPONENT (Path.is_relative_to), so /runs/r/archives is inside
// /runs/r while /runs/r-2 is not; equality counts as inside.
func archiveDestination(q archiveDestinationInput) map[string]any {
	if pathWithin(q.Directory, q.Destination) {
		return retentionRefusal("ARCHIVE_PATH: output must be outside the run")
	}
	return retentionOK()
}

// ---- 3. archive-settled ---------------------------------------------------

type archiveSettledInput struct {
	StatusName string       `json:"status_name"`
	Jobs       []settledJob `json:"jobs"`
}
type settledJob struct {
	Name       string           `json:"name"`
	HasRequest bool             `json:"has_request"`
	HasExit    bool             `json:"has_exit"`
	Processes  []settledProcess `json:"processes"`
}
type settledProcess struct {
	// PID is the recorded identity as a plain int (null when the record held
	// no integer at all); PIDValid is the Python-side fact
	// isinstance(pid, int) and not isinstance(pid, bool) and pid > 0. The
	// bool exclusion is the owner's ruling: CPython's isinstance(True, int)
	// quirk is deliberately not emulated on either side of the bridge.
	PID      *int64 `json:"pid"`
	PIDValid bool   `json:"pid_valid"`
	Probe    string `json:"probe"`
}

// archiveSettled merges the terminal-set check with job settlement, keeping
// the Python side's order: the status string first, then each job (request
// without exit), then each recorded process in ('process.json',
// 'native-process.json') order. The liveness probe outcome travels in the
// payload; os.kill itself stays in Python.
func archiveSettled(q archiveSettledInput) map[string]any {
	if !terminalStatuses[q.StatusName] {
		return retentionRefusal("RUN_NOT_TERMINAL: active or paused run cannot be archived/pruned")
	}
	for _, job := range q.Jobs {
		if job.HasRequest && !job.HasExit {
			return retentionRefusal("RUN_UNSETTLED: " + job.Name + " has no exit receipt; no cleanup")
		}
		for _, process := range job.Processes {
			if !process.PIDValid || process.PID == nil {
				return retentionRefusal("RUN_UNSETTLED: invalid process identity")
			}
			switch process.Probe {
			case "permission":
				return retentionRefusal("RUN_UNSETTLED: process cannot be checked")
			case "alive":
				return retentionRefusal("RUN_UNSETTLED: PID " + strconv.FormatInt(*process.PID, 10) + " still exists; no cleanup")
			}
		}
	}
	return retentionOK()
}

// ---- 4. archive-index-plan ---------------------------------------------------

type archiveIndexPlanInput struct {
	SourceDirectory       string          `json:"source_directory"`
	RunID                 string          `json:"run_id"`
	Revision              string          `json:"revision"`
	RuntimeIdentity       string          `json:"runtime_identity"`
	Status                json.RawMessage `json:"status"`
	DatabaseLogicalSHA256 string          `json:"database_logical_sha256"`
}

// archiveIndexPlan assembles the archive index skeleton — every field except
// the files proofs, which the caller joins locally. The destination-exists
// refusal is NOT judged here: archive_run checks it in Python right after the
// terminal verdict, before any inventory or backup work, exactly where the
// Python guard sits.
func archiveIndexPlan(q archiveIndexPlanInput) (map[string]any, error) {
	if _, err := decodeValue(q.Status); err != nil {
		return nil, fmt.Errorf("archive-index-plan: malformed status: %w", err)
	}
	return map[string]any{"ok": true, "skeleton": map[string]any{
		"format":                  "formal-gates-archive-v1",
		"source_directory":        q.SourceDirectory,
		"run_id":                  q.RunID,
		"revision":                q.Revision,
		"runtime_identity":        q.RuntimeIdentity,
		"status":                  q.Status,
		"database_logical_sha256": q.DatabaseLogicalSHA256,
	}}, nil
}

// ---- 5. archive-verify ---------------------------------------------------

type archiveVerifyInput struct {
	// The small facts the Python reader derived from the zip before any
	// member was hashed. Every batch carries them, so the verdict order is
	// idempotent: duplicates, format/terminal status, the exact member set,
	// then this batch's entries.
	Duplicates bool          `json:"duplicates"`
	Format     *string       `json:"format"`
	StatusName *string       `json:"status_name"`
	NamesMatch bool          `json:"names_match"`
	Entries    []verifyEntry `json:"entries"`
}
type verifyEntry struct {
	Name     string     `json:"name"`
	Actual   proofValue `json:"actual"`
	Expected proofValue `json:"expected"`
}

// archiveVerify is the standalone verification over facts plus one batch of
// member entries (manifest files order, which for a canonical manifest is
// sorted order). Per entry, safety runs before the typed content comparison,
// and the first offending entry in the batch decides.
func archiveVerify(q archiveVerifyInput) (map[string]any, error) {
	if err := chunkLimit("archive-verify", len(q.Entries)); err != nil {
		return nil, err
	}
	if q.Duplicates {
		return retentionRefusal("ARCHIVE_INVALID: duplicate members"), nil
	}
	if q.Format == nil || *q.Format != "formal-gates-archive-v1" ||
		q.StatusName == nil || !terminalStatuses[*q.StatusName] {
		return retentionRefusal("ARCHIVE_INVALID: wrong format or nonterminal run"), nil
	}
	if !q.NamesMatch {
		return retentionRefusal("ARCHIVE_INVALID: missing or extra members"), nil
	}
	for _, entry := range q.Entries {
		if unsafeMemberPath(entry.Name) {
			return retentionRefusal("ARCHIVE_INVALID: unsafe member path"), nil
		}
		if entry.Actual != entry.Expected {
			return retentionRefusal("ARCHIVE_INVALID: content differs: " + entry.Name), nil
		}
	}
	return retentionOK(), nil
}

// ---- 6. archive-stale ---------------------------------------------------

type archiveStaleInput struct {
	// Facts phase: has_new/has_missing present, entries absent. A nil HasNew
	// marks the entries phase instead.
	HasNew       *bool               `json:"has_new"`
	HasMissing   *bool               `json:"has_missing"`
	AllowMissing bool                `json:"allow_missing"`
	Entries      []archiveStaleEntry `json:"entries"`
}
type archiveStaleEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// File rows compare the byte proofs; the database row compares the two
	// logical-hash strings (iterdump hash, not the byte proof). Only the pair
	// the kind names is present in a well-formed row.
	ActualProof   *proofValue `json:"actual_proof"`
	ExpectedProof *proofValue `json:"expected_proof"`
	ActualDB      *string     `json:"actual_db"`
	ExpectedDB    *string     `json:"expected_db"`
}

// archiveStale judges source drift in two segments. The facts segment keeps
// the Python side's rule order: new files first, then (unless missing entries
// are allowed) set equality. The entries segment walks the live inventory in
// the caller's current-iteration order and refuses on the first row whose
// proof (or logical database hash) differs from the index expectation.
func archiveStale(q archiveStaleInput) (map[string]any, error) {
	if q.HasNew != nil {
		if *q.HasNew {
			return retentionRefusal("ARCHIVE_STALE: new files appeared; no deletion"), nil
		}
		if !q.AllowMissing && *q.HasMissing {
			return retentionRefusal("ARCHIVE_STALE: source files missing"), nil
		}
		return retentionOK(), nil
	}
	if err := chunkLimit("archive-stale", len(q.Entries)); err != nil {
		return nil, err
	}
	for _, entry := range q.Entries {
		var equal bool
		switch entry.Kind {
		case "database":
			equal = entry.ActualDB != nil && entry.ExpectedDB != nil && *entry.ActualDB == *entry.ExpectedDB
		case "file":
			equal = entry.ActualProof != nil && entry.ExpectedProof != nil && *entry.ActualProof == *entry.ExpectedProof
		default:
			return nil, fmt.Errorf("archive-stale: unknown entry kind %q", entry.Kind)
		}
		if !equal {
			return retentionRefusal("ARCHIVE_STALE: source differs: " + entry.Name), nil
		}
	}
	return retentionOK(), nil
}

// ---- 7. archive-projection ---------------------------------------------------

type archiveProjectionInput struct {
	// Pre-check phase (no status yet): the marker proof comparison
	// archived_status runs BEFORE opening the zip. Full phase: status present.
	Status          json.RawMessage        `json:"status"`
	SourceDirectory string                 `json:"source_directory"`
	FileCount       int                    `json:"file_count"`
	Archive         string                 `json:"archive"`
	ArchiveProof    *proofValue            `json:"archive_proof"`
	Marker          *projectionMarkerInput `json:"marker"`
	Directory       string                 `json:"directory"`
}

// projectionMarkerInput carries the marker fields each phase needs; the
// caller builds it with natural key accesses so a corrupt marker keeps the
// raw KeyError on the Python side.
type projectionMarkerInput struct {
	ArchiveProof    *proofValue     `json:"archive_proof"`
	RunID           *string         `json:"run_id"`
	Revision        *string         `json:"revision"`
	CleanupComplete json.RawMessage `json:"cleanup_complete"`
}

// archiveProjection serves both status readers. Without `status` it is the
// pre-check archived_status runs BEFORE opening the zip: a present marker
// whose archive_proof no longer matches the retained bytes refuses
// ARCHIVE_CHANGED before any zip work. With the verified status it builds the
// plain projection (no marker) or the full tombstone path: the
// run_id/revision/source_directory binding (ARCHIVE_BINDING), then
// cleanup_complete joining the projection with Python's marker.get default.
func archiveProjection(q archiveProjectionInput) (map[string]any, error) {
	if len(q.Status) == 0 {
		if q.Marker != nil && q.Marker.ArchiveProof != nil {
			if q.ArchiveProof == nil || *q.ArchiveProof != *q.Marker.ArchiveProof {
				return retentionRefusal("ARCHIVE_CHANGED: retained archive missing or altered"), nil
			}
		}
		return retentionOK(), nil
	}
	statusValue, err := decodeValue(q.Status)
	if err != nil {
		return nil, fmt.Errorf("archive-projection: malformed status: %w", err)
	}
	status, ok := statusValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("archive-projection: status is not an object")
	}
	projection := make(map[string]any, len(status)+6)
	for key, value := range status {
		projection[key] = value
	}
	projection["archived"] = true
	projection["archive"] = q.Archive
	projection["archive_verified"] = true
	projection["source_directory"] = q.SourceDirectory
	projection["file_count"] = q.FileCount
	projection["model_calls"] = 0
	if q.Marker == nil {
		return map[string]any{"ok": true, "status": projection}, nil
	}
	if q.Marker.RunID == nil || q.Marker.Revision == nil ||
		status["run_id"] != *q.Marker.RunID || status["revision"] != *q.Marker.Revision ||
		q.SourceDirectory != q.Directory {
		return retentionRefusal("ARCHIVE_BINDING: tombstone does not match archive"), nil
	}
	cleanup := any(false) // marker.get('cleanup_complete', False)
	if len(q.Marker.CleanupComplete) > 0 {
		cleanup, err = decodeValue(q.Marker.CleanupComplete)
		if err != nil {
			return nil, fmt.Errorf("archive-projection: malformed cleanup_complete: %w", err)
		}
	}
	projection["cleanup_complete"] = cleanup
	return map[string]any{"ok": true, "status": projection}, nil
}

// ---- 8. prune-admission ---------------------------------------------------

type pruneAdmissionInput struct {
	RunsDir       string `json:"runs_dir"`
	Directory     string `json:"directory"`
	DirectoryName string `json:"directory_name"`
	RunID         string `json:"run_id"`
	Archive       string `json:"archive"`
}

// pruneAdmission keeps the eligibility rules: only this installation's own
// run directories (parent must be the resolved .harness/runs and the leaf name
// must be the run id) and the archive must sit outside the run, per path
// component.
func pruneAdmission(q pruneAdmissionInput) map[string]any {
	if parentDirectory(q.Directory) != q.RunsDir || q.DirectoryName != q.RunID {
		return retentionRefusal("PRUNE_PATH: only this installation's run directories are eligible")
	}
	if pathWithin(q.Directory, q.Archive) {
		return retentionRefusal("ARCHIVE_PATH: archive must be outside the run")
	}
	return retentionOK()
}

// ---- 9. prune-bind ---------------------------------------------------

type pruneBindInput struct {
	Directory       string `json:"directory"`
	RunID           string `json:"run_id"`
	Revision        string `json:"revision"`
	SourceDirectory string `json:"source_directory"`
	IndexRunID      string `json:"index_run_id"`
	IndexRevision   string `json:"index_revision"`
}

// pruneBind requires the archive index to name exactly this run, revision and
// directory. The index fields come from a possibly foreign manifest; the
// caller normalizes a non-string value to null (which decodes as the empty
// string), so a heterogeneous value can never match a real identity.
func pruneBind(q pruneBindInput) map[string]any {
	if q.SourceDirectory != q.Directory || q.IndexRunID != q.RunID || q.IndexRevision != q.Revision {
		return retentionRefusal("ARCHIVE_BINDING: wrong run/revision")
	}
	return retentionOK()
}

// ---- 10. prune-marker ---------------------------------------------------

type pruneMarkerInput struct {
	Archive            string     `json:"archive"`
	ArchiveProof       proofValue `json:"archive_proof"`
	MarkerArchive      string     `json:"marker_archive"`
	MarkerArchiveProof proofValue `json:"marker_archive_proof"`
}

// pruneMarker binds a re-entered prune to the archive the tombstone recorded.
// It runs after the terminal verdict and before any staleness work, exactly
// where the Python `if marker and (...)` comparison sits; the caller
// invokes it only for a truthy marker.
func pruneMarkerOp(q pruneMarkerInput) map[string]any {
	if q.Archive != q.MarkerArchive || q.ArchiveProof != q.MarkerArchiveProof {
		return retentionRefusal("ARCHIVE_BINDING: use the original cleanup archive")
	}
	return retentionOK()
}

// ---- 11. prune-plan ---------------------------------------------------

type prunePlanInput struct {
	// One full call in current-inventory order: name+size rows only, never a
	// proof, so the payload stays around 80 bytes per file.
	Entries        []prunePlanEntry `json:"entries"`
	RunID          string           `json:"run_id"`
	Revision       string           `json:"revision"`
	TerminalStatus string           `json:"terminal_status"`
	Archive        string           `json:"archive"`
	MarkerExists   bool             `json:"marker_exists"`
}
type prunePlanEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// prunePlan builds the removal plan: everything not in KEEP, deepest path
// first, ties by name. The report keys match the Python side's preview; the
// tombstone plan is attached only when no marker exists yet. The plan's
// archive_proof is merged by the caller, which owns the hashing.
func prunePlanOp(q prunePlanInput) (map[string]any, error) {
	sizes := make(map[string]int64, len(q.Entries))
	remove := []string{}
	for _, entry := range q.Entries {
		sizes[entry.Name] = entry.Size
		if !keepNames[entry.Name] {
			remove = append(remove, entry.Name)
		}
	}
	sort.Slice(remove, func(i, j int) bool {
		depth := strings.Count(remove[i], "/") - strings.Count(remove[j], "/")
		if depth != 0 {
			return depth > 0
		}
		return remove[i] < remove[j]
	})
	var bytesToRemove int64
	for _, name := range remove {
		bytesToRemove += sizes[name]
	}
	report := map[string]any{
		"status":          "PRUNE_PREVIEW",
		"run_id":          q.RunID,
		"terminal_status": q.TerminalStatus,
		"archive":         q.Archive,
		"files_to_remove": len(remove),
		"bytes_to_remove": bytesToRemove,
		"model_calls":     0,
	}
	var plan any
	if !q.MarkerExists {
		plan = map[string]any{
			"format":           "formal-gates-prune-v1",
			"run_id":           q.RunID,
			"revision":         q.Revision,
			"archive":          q.Archive,
			"cleanup_complete": false,
		}
	}
	return map[string]any{"ok": true, "report": report, "remove": remove, "marker": plan}, nil
}

// ---- shared path helpers -----------------------------------------

// unsafeMemberPath is the PurePosixPath rule: absolute, a '..' component, or
// a backslash anywhere.
func unsafeMemberPath(name string) bool {
	if strings.HasPrefix(name, "/") {
		return true
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return true
		}
	}
	return strings.Contains(name, `\`)
}

// pathComponents splits an absolute path into its non-empty components.
func pathComponents(path string) []string {
	components := []string{}
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			components = append(components, part)
		}
	}
	return components
}

// pathWithin is Path.is_relative_to over components: equal counts as within,
// a longer string with a new last component (/a/bc vs /a/b) does not.
func pathWithin(directory, candidate string) bool {
	dir := pathComponents(directory)
	cand := pathComponents(candidate)
	if len(cand) < len(dir) {
		return false
	}
	for i, part := range dir {
		if cand[i] != part {
			return false
		}
	}
	return true
}

// parentDirectory is Path.parent for an absolute path.
func parentDirectory(path string) string {
	components := pathComponents(path)
	if len(components) <= 1 {
		return "/"
	}
	return "/" + strings.Join(components[:len(components)-1], "/")
}
