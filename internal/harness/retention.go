package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// The ten terminal-retention judgements below moved from retention.py (G3b).
// Each one is a plain function over its payload: the caller (retention.py)
// keeps every filesystem fact — locking, hashing, the SQLite backup, zip
// reading and writing, process probing and the deletions — and ships ordered
// projections, so Python never re-derives a condition after the call. Value
// payloads (the manifest contents, the marker, proofs) are decoded with
// number fidelity and Python comparison semantics; a refusal is a typed
// business outcome inside the value ({ok:false, refusal:...}) and retention.py
// raises the HarnessError family directly from its text.

var terminalStatuses = map[string]bool{
	"HARNESS_COMPLETE": true,
	"DEMO_COMPLETE":    true,
	"ABORTED":          true,
}

// skipNames are never archived and never pruned; the check runs before the
// symlink judgement, exactly as in the pre-migration inventory walk.
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

func retentionRefusal(refusal string) map[string]any {
	return map[string]any{"ok": false, "refusal": refusal}
}
func retentionOK() map[string]any {
	return map[string]any{"ok": true}
}

// retentionDispatch routes one terminal-retention operation to its judgement.
func retentionDispatch(operation string, payload []byte) (any, error) {
	switch operation {
	case "archive-inventory":
		var q archiveInventoryInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return archiveInventoryOp(q), nil
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
		return pruneBind(q)
	case "prune-plan":
		var q prunePlanInput
		if err := StrictDecode(payload, &q); err != nil {
			return nil, err
		}
		return prunePlan(q)
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

// archiveInventoryOp applies the member admission rule over the walked entries
// (already in sorted-rglob order). Order is preserved verbatim: SKIP wins over
// the symlink refusal, directories are skipped, and only plain files pass.
func archiveInventoryOp(q archiveInventoryInput) map[string]any {
	files := []string{}
	for _, entry := range q.Entries {
		if skipNames[entry.Name] {
			continue
		}
		if entry.Kind == "symlink" {
			return retentionRefusal("ARCHIVE_UNSUPPORTED: symlink " + entry.Name + "; no files deleted")
		}
		if entry.Kind == "dir" {
			continue
		}
		if entry.Kind != "file" {
			return retentionRefusal("ARCHIVE_UNSUPPORTED: non-regular entry " + entry.Name)
		}
		files = append(files, entry.Name)
	}
	return map[string]any{"ok": true, "files": files}
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
	PID   json.RawMessage `json:"pid"`
	Probe string          `json:"probe"`
}

// archiveSettled merges the terminal-set check with job settlement, keeping
// the pre-migration order: the status string first, then each job (request
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
			pid, ok := pythonInteger(process.PID)
			if !ok || pid.Sign() <= 0 {
				return retentionRefusal("RUN_UNSETTLED: invalid process identity")
			}
			switch process.Probe {
			case "permission":
				return retentionRefusal("RUN_UNSETTLED: process cannot be checked")
			case "alive":
				return retentionRefusal("RUN_UNSETTLED: PID " + pid.String() + " still exists; no cleanup")
			}
		}
	}
	return retentionOK()
}

// ---- 4. archive-index-plan ---------------------------------------------------

type archiveIndexPlanInput struct {
	DestinationExists     bool                       `json:"destination_exists"`
	SourceDirectory       string                     `json:"source_directory"`
	RunID                 string                     `json:"run_id"`
	Revision              string                     `json:"revision"`
	RuntimeIdentity       string                     `json:"runtime_identity"`
	Status                json.RawMessage            `json:"status"`
	Files                 map[string]json.RawMessage `json:"files"`
	DatabaseLogicalSHA256 string                     `json:"database_logical_sha256"`
	CheckpointsProof      json.RawMessage            `json:"checkpoints_proof"`
}

// archiveIndexPlan refuses an existing destination and assembles the archive
// index. The Python caller writes canonical(index), and canonical JSON sorts
// keys, so the response's map encoding loses nothing. The document order of
// the pre-migration literal was format, source_directory, run_id, revision,
// runtime_identity, status, files, database_logical_sha256.
func archiveIndexPlan(q archiveIndexPlanInput) (map[string]any, error) {
	status, err := decodeValue(q.Status)
	if err != nil {
		return nil, fmt.Errorf("archive-index-plan: malformed status: %w", err)
	}
	files := make(map[string]any, len(q.Files)+1)
	for name, raw := range q.Files {
		value, err := decodeValue(raw)
		if err != nil {
			return nil, fmt.Errorf("archive-index-plan: malformed proof for %s: %w", name, err)
		}
		files[name] = value
	}
	proof, err := decodeValue(q.CheckpointsProof)
	if err != nil {
		return nil, fmt.Errorf("archive-index-plan: malformed checkpoints proof: %w", err)
	}
	if q.DestinationExists {
		return retentionRefusal("ARCHIVE_EXISTS: use a new destination; not overwritten"), nil
	}
	files["checkpoints.sqlite"] = proof
	index := map[string]any{
		"format":                  "formal-gates-archive-v1",
		"source_directory":        q.SourceDirectory,
		"run_id":                  q.RunID,
		"revision":                q.Revision,
		"runtime_identity":        q.RuntimeIdentity,
		"status":                  status,
		"files":                   files,
		"database_logical_sha256": q.DatabaseLogicalSHA256,
	}
	return map[string]any{"ok": true, "index": index}, nil
}

// ---- 5. archive-verify ---------------------------------------------------

type archiveVerifyInput struct {
	Names        []string        `json:"names"`
	Index        json.RawMessage `json:"index"`
	MemberProofs json.RawMessage `json:"member_proofs"`
}

// archiveVerify is the standalone verification in two phases over one
// operation. Without `index` it only rejects duplicate zip members; with it,
// the manifest rules run in the pre-migration order: format and terminal
// status, the exact member set, then per-name safety and content in the
// manifest's files order. Python has already produced the wrapped KeyError
// for a missing status/files key before this second phase is called.
func archiveVerify(q archiveVerifyInput) (map[string]any, error) {
	names := uniqueNames(q.Names)
	if len(q.Index) == 0 {
		if len(names) != len(q.Names) {
			return retentionRefusal("ARCHIVE_INVALID: duplicate members"), nil
		}
		return retentionOK(), nil
	}
	decoded, err := decodeOrdered(q.Index)
	if err != nil {
		return nil, fmt.Errorf("archive-verify: malformed index: %w", err)
	}
	index, _ := decoded.(*orderedObject)
	if index == nil {
		index = &orderedObject{values: map[string]any{}}
	}
	// Python's index.get('format') and index['status']['status']: the format
	// default only covers the absent key; a non-string status name is simply
	// not a member of the terminal set.
	if !pythonEqual(index.values["format"], "formal-gates-archive-v1") {
		return retentionRefusal("ARCHIVE_INVALID: wrong format or nonterminal run"), nil
	}
	status, _ := index.values["status"].(*orderedObject)
	var statusName any
	if status != nil {
		statusName = status.values["status"]
	}
	if !terminalStatusValue(statusName) {
		return retentionRefusal("ARCHIVE_INVALID: wrong format or nonterminal run"), nil
	}
	files, _ := index.values["files"].(*orderedObject)
	expected := map[string]bool{"archive-manifest.json": true}
	fileNames := []string{}
	if files != nil {
		for _, name := range files.keys {
			expected["run/"+name] = true
			fileNames = append(fileNames, name)
		}
	}
	if len(names) != len(expected) {
		return retentionRefusal("ARCHIVE_INVALID: missing or extra members"), nil
	}
	for _, name := range names {
		if !expected[name] {
			return retentionRefusal("ARCHIVE_INVALID: missing or extra members"), nil
		}
	}
	var proofs map[string]any
	if len(q.MemberProofs) > 0 {
		value, err := decodeValue(q.MemberProofs)
		if err != nil {
			return nil, fmt.Errorf("archive-verify: malformed member proofs: %w", err)
		}
		proofs, _ = value.(map[string]any)
	}
	if files != nil {
		for _, name := range files.keys {
			if unsafeMemberPath(name) {
				return retentionRefusal("ARCHIVE_INVALID: unsafe member path"), nil
			}
			if !pythonEqual(proofs[name], files.values[name]) {
				return retentionRefusal("ARCHIVE_INVALID: content differs: " + name), nil
			}
		}
	}
	return map[string]any{"ok": true, "index": index.toPlain()}, nil
}

// ---- 6. archive-stale ---------------------------------------------------

type archiveStaleInput struct {
	Current                   json.RawMessage `json:"current"`
	Files                     json.RawMessage `json:"files"`
	AllowMissing              bool            `json:"allow_missing"`
	DatabaseLogicalSHA256     json.RawMessage `json:"database_logical_sha256"`
	LiveDatabaseLogicalSHA256 string          `json:"live_database_logical_sha256"`
}

// archiveStale judges source drift. `current` is the live inventory (hashed by
// Python); `files` and `database_logical_sha256` are the index expectations.
// The checkpoints.sqlite comparison is logical (iterdump hash), not the byte
// proof, so Python ships the live logical hash alongside; both sides stay
// facts the sqlite reader in Python produced.
func archiveStale(q archiveStaleInput) (map[string]any, error) {
	current, files, err := decodeStaleSides(q.Current, q.Files)
	if err != nil {
		return nil, err
	}
	indexDB, err := decodeValue(q.DatabaseLogicalSHA256)
	if err != nil {
		return nil, fmt.Errorf("archive-stale: malformed logical hash: %w", err)
	}
	if refusal := judgeStaleness(current, files, q.AllowMissing, indexDB, q.LiveDatabaseLogicalSHA256); refusal != "" {
		return retentionRefusal(refusal), nil
	}
	return retentionOK(), nil
}

// decodeStaleSides decodes the live inventory (document order kept, the
// "source differs" naming follows it) and the index expectation map.
func decodeStaleSides(currentRaw, filesRaw []byte) (*orderedObject, map[string]any, error) {
	current, err := decodeOrderedObject(currentRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("archive-stale: malformed current inventory: %w", err)
	}
	files, err := decodePlainObject(filesRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("archive-stale: malformed expected files: %w", err)
	}
	return current, files, nil
}

// judgeStaleness keeps the pre-migration rule order: new files first, then
// (unless missing entries are allowed) set equality, then per-name comparison
// in current's iteration order with the checkpoints.sqlite logical special
// case. An empty refusal means the source still matches the index.
func judgeStaleness(current *orderedObject, files map[string]any, allowMissing bool, indexDB any, liveDB string) string {
	for _, name := range current.keys {
		if _, ok := files[name]; !ok {
			return "ARCHIVE_STALE: new files appeared; no deletion"
		}
	}
	if !allowMissing && len(current.keys) != len(files) {
		return "ARCHIVE_STALE: source files missing"
	}
	for _, name := range current.keys {
		var equal bool
		if name == "checkpoints.sqlite" {
			equal = pythonEqual(liveDB, indexDB)
		} else {
			equal = pythonEqual(current.values[name], files[name])
		}
		if !equal {
			return "ARCHIVE_STALE: source differs: " + name
		}
	}
	return ""
}

// ---- 7. archive-projection ---------------------------------------------------

type archiveProjectionInput struct {
	Index        json.RawMessage `json:"index"`
	Archive      string          `json:"archive"`
	ArchiveProof json.RawMessage `json:"archive_proof"`
	Marker       json.RawMessage `json:"marker"`
	Directory    string          `json:"directory"`
}

// archiveProjection serves both status readers in two phases over one
// operation (the archive-verify pattern). Without `index` it is the pre-check
// archived_status runs BEFORE opening the zip: a present marker whose
// archive_proof no longer matches the retained bytes refuses ARCHIVE_CHANGED
// before any zip work, exactly as the pre-migration comparison did. With the
// verified index, the plain projection (no marker) or the full tombstone
// path runs: ARCHIVE_CHANGED, then the run_id/revision/source_directory
// binding (ARCHIVE_BINDING), then cleanup_complete joining the projection
// with Python's marker.get default. Python has already touched the marker
// keys, so a missing key keeps today's raw KeyError there.
func archiveProjection(q archiveProjectionInput) (map[string]any, error) {
	markerPresent := len(q.Marker) > 0 && string(q.Marker) != "null"
	var marker map[string]any
	var proof any
	if markerPresent {
		markerValue, err := decodeOrdered(q.Marker)
		if err != nil {
			return nil, fmt.Errorf("archive-projection: malformed marker: %w", err)
		}
		marker, _ = plainValue(markerValue).(map[string]any)
		if marker == nil {
			marker = map[string]any{}
		}
		proof, err = decodeValue(q.ArchiveProof)
		if err != nil {
			return nil, fmt.Errorf("archive-projection: malformed archive proof: %w", err)
		}
		if !pythonEqual(proof, marker["archive_proof"]) {
			return retentionRefusal("ARCHIVE_CHANGED: retained archive missing or altered"), nil
		}
	}
	if len(q.Index) == 0 {
		// Pre-check phase: no index yet, nothing else to judge.
		return retentionOK(), nil
	}
	indexValue, err := decodeValue(q.Index)
	if err != nil {
		return nil, fmt.Errorf("archive-projection: malformed index: %w", err)
	}
	index, _ := indexValue.(map[string]any)
	if index == nil {
		index = map[string]any{}
	}
	status, _ := index["status"].(map[string]any)
	if status == nil {
		status = map[string]any{}
	}
	files, _ := index["files"].(map[string]any)
	projection := make(map[string]any, len(status)+6)
	for key, value := range status {
		projection[key] = value
	}
	projection["archived"] = true
	projection["archive"] = q.Archive
	projection["archive_verified"] = true
	projection["source_directory"] = index["source_directory"]
	projection["file_count"] = len(files)
	projection["model_calls"] = 0
	if !markerPresent {
		return map[string]any{"ok": true, "status": projection}, nil
	}
	if !pythonEqual(projection["run_id"], marker["run_id"]) ||
		!pythonEqual(projection["revision"], marker["revision"]) ||
		!pythonEqual(projection["source_directory"], q.Directory) {
		return retentionRefusal("ARCHIVE_BINDING: tombstone does not match archive"), nil
	}
	cleanup, ok := marker["cleanup_complete"] // marker.get(..., False)
	if !ok {
		cleanup = false
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
	Directory       string          `json:"directory"`
	RunID           string          `json:"run_id"`
	Revision        string          `json:"revision"`
	SourceDirectory json.RawMessage `json:"source_directory"`
	IndexRunID      json.RawMessage `json:"index_run_id"`
	IndexRevision   json.RawMessage `json:"index_revision"`
}

// pruneBind requires the archive index to name exactly this run, revision and
// directory. The index fields are values from a possibly foreign manifest, so
// they compare under Python equality (a non-string can never match).
func pruneBind(q pruneBindInput) (map[string]any, error) {
	source, err := decodeValue(q.SourceDirectory)
	if err != nil {
		return nil, fmt.Errorf("prune-bind: malformed source directory: %w", err)
	}
	runID, err := decodeValue(q.IndexRunID)
	if err != nil {
		return nil, fmt.Errorf("prune-bind: malformed index run id: %w", err)
	}
	revision, err := decodeValue(q.IndexRevision)
	if err != nil {
		return nil, fmt.Errorf("prune-bind: malformed index revision: %w", err)
	}
	if !pythonEqual(source, q.Directory) || !pythonEqual(runID, q.RunID) || !pythonEqual(revision, q.Revision) {
		return retentionRefusal("ARCHIVE_BINDING: wrong run/revision"), nil
	}
	return retentionOK(), nil
}

// ---- 10. prune-plan ---------------------------------------------------

type prunePlanInput struct {
	Marker                    json.RawMessage `json:"marker"`
	Archive                   string          `json:"archive"`
	ArchiveProof              json.RawMessage `json:"archive_proof"`
	Current                   json.RawMessage `json:"current"`
	Files                     json.RawMessage `json:"files"`
	DatabaseLogicalSHA256     json.RawMessage `json:"database_logical_sha256"`
	LiveDatabaseLogicalSHA256 string          `json:"live_database_logical_sha256"`
	AllowMissing              bool            `json:"allow_missing"`
	RunID                     string          `json:"run_id"`
	Revision                  string          `json:"revision"`
	TerminalStatus            string          `json:"terminal_status"`
	Directory                 string          `json:"directory"`
}

// prunePlan keeps the pre-migration order: the marker binding first (only
// when the marker is truthy, matching `if marker and ...`), then the same
// staleness rules as archive-stale, then the removal plan — everything not in
// KEEP, deepest path first, ties by name. The report and the tombstone plan
// are facts assembled here; the deletions and the atomic marker write stay in
// Python.
func prunePlan(q prunePlanInput) (map[string]any, error) {
	markerPresent := len(q.Marker) > 0 && string(q.Marker) != "null"
	var marker any
	if markerPresent {
		decoded, err := decodeOrdered(q.Marker)
		if err != nil {
			return nil, fmt.Errorf("prune-plan: malformed marker: %w", err)
		}
		marker = decoded
		if pythonTruthy(marker) {
			markerMap, _ := plainValue(marker).(map[string]any)
			if markerMap == nil {
				markerMap = map[string]any{}
			}
			proof, err := decodeValue(q.ArchiveProof)
			if err != nil {
				return nil, fmt.Errorf("prune-plan: malformed archive proof: %w", err)
			}
			if !pythonEqual(markerMap["archive"], q.Archive) || !pythonEqual(markerMap["archive_proof"], proof) {
				return retentionRefusal("ARCHIVE_BINDING: use the original cleanup archive"), nil
			}
		}
	}
	current, files, err := decodeStaleSides(q.Current, q.Files)
	if err != nil {
		return nil, err
	}
	indexDB, err := decodeValue(q.DatabaseLogicalSHA256)
	if err != nil {
		return nil, fmt.Errorf("prune-plan: malformed logical hash: %w", err)
	}
	if refusal := judgeStaleness(current, files, q.AllowMissing, indexDB, q.LiveDatabaseLogicalSHA256); refusal != "" {
		return retentionRefusal(refusal), nil
	}
	remove := []string{}
	for _, name := range current.keys {
		if !keepNames[name] {
			remove = append(remove, name)
		}
	}
	sort.Slice(remove, func(i, j int) bool {
		depth := strings.Count(remove[i], "/") - strings.Count(remove[j], "/")
		if depth != 0 {
			return depth > 0
		}
		return remove[i] < remove[j]
	})
	report := map[string]any{
		"status":          "PRUNE_PREVIEW",
		"run_id":          q.RunID,
		"terminal_status": q.TerminalStatus,
		"archive":         q.Archive,
		"files_to_remove": len(remove),
		"bytes_to_remove": sumSizes(current, remove),
		"model_calls":     0,
	}
	var plan any
	if !markerPresent {
		proof, err := decodeValue(q.ArchiveProof)
		if err != nil {
			return nil, fmt.Errorf("prune-plan: malformed archive proof: %w", err)
		}
		plan = map[string]any{
			"format":           "formal-gates-prune-v1",
			"run_id":           q.RunID,
			"revision":         q.Revision,
			"archive":          q.Archive,
			"archive_proof":    proof,
			"cleanup_complete": false,
		}
	}
	return map[string]any{"ok": true, "report": report, "remove": remove, "marker": plan}, nil
}

// ---- shared value and path helpers -----------------------------------------

// orderedObject is a JSON object decoded with number fidelity whose key order
// survives, so iterations name the same entry Python's ordered dicts named.
type orderedObject struct {
	keys   []string
	values map[string]any
}

// decodeOrdered parses a JSON value keeping object key order. Duplicate keys
// keep their first position with their last value, matching Python's dict.
func decodeOrdered(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return orderedValue(decoder)
}

func orderedValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil // string, json.Number, bool or nil
	}
	switch delim {
	case '{':
		object := &orderedObject{values: map[string]any{}}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string")
			}
			value, err := orderedValue(decoder)
			if err != nil {
				return nil, err
			}
			if _, exists := object.values[key]; !exists {
				object.keys = append(object.keys, key)
			}
			object.values[key] = value
		}
		if _, err := decoder.Token(); err != nil { // consume '}'
			return nil, err
		}
		return object, nil
	case '[':
		list := []any{}
		for decoder.More() {
			value, err := orderedValue(decoder)
			if err != nil {
				return nil, err
			}
			list = append(list, value)
		}
		if _, err := decoder.Token(); err != nil { // consume ']'
			return nil, err
		}
		return list, nil
	}
	return nil, fmt.Errorf("unexpected delimiter %v", delim)
}

// toPlain converts ordered objects into plain maps for the answer value; the
// transport encodes maps with sorted keys and canonical JSON sorts anyway.
func (o *orderedObject) toPlain() map[string]any {
	plain := make(map[string]any, len(o.keys))
	for _, key := range o.keys {
		plain[key] = plainValue(o.values[key])
	}
	return plain
}

// plainValue recursively replaces ordered objects with plain maps.
func plainValue(value any) any {
	switch typed := value.(type) {
	case *orderedObject:
		return typed.toPlain()
	case []any:
		list := make([]any, len(typed))
		for i, item := range typed {
			list[i] = plainValue(item)
		}
		return list
	}
	return value
}

func decodeOrderedObject(raw []byte) (*orderedObject, error) {
	decoded, err := decodeOrdered(raw)
	if err != nil {
		return nil, err
	}
	object, _ := decoded.(*orderedObject)
	if object == nil {
		object = &orderedObject{values: map[string]any{}}
	}
	return object, nil
}

func decodePlainObject(raw []byte) (map[string]any, error) {
	value, err := decodeValue(raw)
	if err != nil {
		return nil, err
	}
	object, _ := value.(map[string]any)
	if object == nil {
		object = map[string]any{}
	}
	return object, nil
}

// pythonEqual reproduces Python's == over decoded JSON values: type-strict
// for null/bool/string, numerically exact across int and float literals, and
// order-independent and recursive for containers.
func pythonEqual(a, b any) bool {
	a, b = plainValue(a), plainValue(b)
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		ra, oka := numberRat(x)
		rb, okb := numberRat(y)
		return oka && okb && ra.Cmp(rb) == 0
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !pythonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			other, ok := y[key]
			if !ok || !pythonEqual(value, other) {
				return false
			}
		}
		return true
	}
	return false
}

// pythonTruthy reproduces Python truthiness; prune's marker binding gate was
// `if marker and ...`, so an empty marker object skips the check exactly as
// before.
func pythonTruthy(value any) bool {
	switch typed := plainValue(value).(type) {
	case nil:
		return false
	case bool:
		return typed
	case json.Number:
		r, ok := numberRat(typed)
		return ok && r.Sign() != 0
	case string:
		return typed != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	}
	return true
}

// numberRat maps a JSON number literal onto the exact rational Python would
// compare: integer literals stay arbitrary precision, float literals go
// through the float64 the parser produced.
func numberRat(n json.Number) (*big.Rat, bool) {
	literal := n.String()
	if !strings.ContainsAny(literal, ".eE") {
		integer, ok := new(big.Int).SetString(literal, 10)
		if !ok {
			return nil, false
		}
		return new(big.Rat).SetInt(integer), true
	}
	value, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		return nil, false
	}
	return new(big.Rat).SetFloat64(value), true
}

// pythonInteger reports the int a pid literal must be: a JSON integer
// literal, positive. Note: CPython's isinstance(True, int) let a JSON true
// through the pre-migration check; Go refuses it — an intentional divergence
// from mechanical CPython emulation, ruled by the owner.
func pythonInteger(raw []byte) (*big.Int, bool) {
	literal := string(raw)
	if literal == "" {
		return nil, false
	}
	if strings.ContainsAny(literal, ".eE") {
		return nil, false
	}
	value, ok := new(big.Int).SetString(literal, 10)
	if !ok {
		return nil, false
	}
	return value, true
}

// terminalStatusValue is set membership with Python semantics: only a string
// can be a member, any other value is simply not terminal.
func terminalStatusValue(value any) bool {
	name, ok := value.(string)
	return ok && terminalStatuses[name]
}

func uniqueNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if !seen[name] {
			seen[name] = true
			unique = append(unique, name)
		}
	}
	return unique
}

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

// sumSizes adds the removal sizes in remove order, the way Python's sum()
// did. Inventory sizes are integer literals; a float falls back to float64
// accumulation in the same order.
func sumSizes(current *orderedObject, remove []string) any {
	total := new(big.Int)
	mixed := false
	floatSum := 0.0
	for _, name := range remove {
		number, ok := proofSize(current.values[name])
		if !ok {
			continue // sizes are proof fields; a non-number never reaches here
		}
		literal := number.String()
		if !mixed && !strings.ContainsAny(literal, ".eE") {
			if add, ok := new(big.Int).SetString(literal, 10); ok {
				total.Add(total, add)
				continue
			}
		}
		if !mixed {
			mixed = true
			floatSum, _ = new(big.Float).SetInt(total).Float64()
		}
		if value, err := strconv.ParseFloat(literal, 64); err == nil {
			floatSum += value
		}
	}
	if !mixed {
		return json.Number(total.String())
	}
	return floatSum
}

// proofSize reads the size field out of one inventory proof value.
func proofSize(value any) (json.Number, bool) {
	if proof, ok := plainValue(value).(map[string]any); ok {
		number, ok := proof["size"].(json.Number)
		return number, ok
	}
	return "", false
}
