package harness

// The physical archive: after a terminal judgment, the run
// produces a real, verified ZIP outside the run directory — a consistent
// SQLite backup (VACUUM INTO), every admitted member written under the
// retention judgements' admission rules, and a post-write verification
// that re-reads every member and compares its proof. The judgements
// (settled, destination, member admission, index skeleton, verify) are the
// accepted retention.go ones; this file owns the filesystem effects.

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"formal-gates/internal/coordination"

	_ "modernc.org/sqlite" // the same pure-Go driver the kernel uses
)

// ArchiveReport is the result of one successful archive: the real archive
// and its verification.
type ArchiveReport struct {
	Status      string `json:"status"`
	Archive     string `json:"archive"`
	FileCount   int    `json:"file_count"`
	ArchiveSHA  string `json:"archive_sha256"`
	ArchiveSize int64  `json:"archive_size"`
}

// Archive physicalizes a terminal run. A non-terminal run is refused by
// the settlement judgement; an existing destination is never overwritten.
func (c *Compose) Archive(destination string) (ArchiveReport, error) {
	terminal, _, err := c.store.Terminal(context.Background())
	if err != nil {
		return ArchiveReport{}, err
	}
	status := terminal
	if status == "" {
		if seal := c.latestOutcome(taskSeal); seal != nil && seal.Status != "" {
			if _, err := os.Stat(filepath.Join(c.dir, "harness-result.json")); err == nil {
				status = seal.Status
			}
		}
	}
	if status == "" {
		status = "ACTIVE"
	}
	jobs, err := c.jobSettlementFacts()
	if err != nil {
		return ArchiveReport{}, err
	}
	if settled := archiveSettled(archiveSettledInput{StatusName: status, Jobs: jobs}); settled["ok"] != true {
		return ArchiveReport{}, fmt.Errorf("compose: %s", textOf(settled["refusal"]))
	}
	if verdict := archiveDestination(archiveDestinationInput{Directory: c.dir, Destination: destination}); verdict["ok"] != true {
		return ArchiveReport{}, fmt.Errorf("compose: %s", textOf(verdict["refusal"]))
	}
	if _, err := os.Stat(destination); err == nil {
		return ArchiveReport{}, fmt.Errorf("compose: ARCHIVE_EXISTS: use a new destination; not overwritten")
	} else if !os.IsNotExist(err) {
		return ArchiveReport{}, err
	}
	// The member inventory over the run directory, admission-judged in
	// batches; the live database file travels as its backup, and the
	// transient WAL sidecars and the directory lock not at all.
	names, err := c.archiveInventory()
	if err != nil {
		return ArchiveReport{}, err
	}
	backup, err := c.backupDatabase(destination)
	if err != nil {
		return ArchiveReport{}, err
	}
	defer os.Remove(backup)
	proofs := map[string]proofValue{}
	for _, name := range names {
		proof, err := fileProofValue(filepath.Join(c.dir, filepath.FromSlash(name)))
		if err != nil {
			return ArchiveReport{}, err
		}
		proofs[name] = proof
	}
	backupProof, err := fileProofValue(backup)
	if err != nil {
		return ArchiveReport{}, err
	}
	proofs["run.db"] = backupProof
	// The index skeleton plus the member proofs.
	statusJSON, err := json.Marshal(map[string]any{"status": status, "run_id": c.cfg.RunID})
	if err != nil {
		return ArchiveReport{}, err
	}
	plan, err := archiveIndexPlan(archiveIndexPlanInput{
		SourceDirectory: c.dir, RunID: c.cfg.RunID, Revision: c.spec.revision,
		RuntimeIdentity: c.binary, Status: statusJSON,
		DatabaseLogicalSHA256: logicalDatabaseHash(backup),
	})
	if err != nil {
		return ArchiveReport{}, err
	}
	if plan["ok"] != true {
		return ArchiveReport{}, fmt.Errorf("compose: archive index plan refused")
	}
	index := map[string]any{}
	if skeleton, ok := plan["skeleton"].(map[string]any); ok {
		for key, value := range skeleton {
			index[key] = value
		}
	}
	index["files"] = proofs
	indexBody, err := canonicalBody(index)
	if err != nil {
		return ArchiveReport{}, err
	}
	// Write the ZIP beside the destination, verify it, then publish it
	// without the possibility of overwriting anything.
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return ArchiveReport{}, err
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".archive-*")
	if err != nil {
		return ArchiveReport{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := writeZip(temp, c.dir, backup, names, indexBody); err != nil {
		temp.Close()
		return ArchiveReport{}, err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return ArchiveReport{}, err
	}
	if err := temp.Close(); err != nil {
		return ArchiveReport{}, err
	}
	if err := verifyArchive(tempPath, index, proofs); err != nil {
		return ArchiveReport{}, err
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return ArchiveReport{}, err
	}
	proof, err := fileProofValue(destination)
	if err != nil {
		return ArchiveReport{}, err
	}
	return ArchiveReport{
		Status: "ARCHIVED", Archive: destination,
		FileCount: len(proofs), ArchiveSHA: proof.SHA256, ArchiveSize: proof.Size,
	}, nil
}

// archiveInventory walks the run directory and admission-judges every
// member in batches, returning the admitted file names in walk order.
// The live database, its WAL sidecars and the directory lock are excluded
// here and travel (or not) by their own rules above.
func (c *Compose) archiveInventory() ([]string, error) {
	localSkip := map[string]bool{"run.db": true, "run.db-wal": true, "run.db-shm": true, "run.lock": true}
	var admitted []string
	batch := make([]archiveInventoryEntry, 0, retentionChunk)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		verdict, err := archiveInventoryOp(archiveInventoryInput{Entries: batch})
		if err != nil {
			return err
		}
		if verdict["ok"] != true {
			return fmt.Errorf("compose: %s", textOf(verdict["refusal"]))
		}
		// The admission judgement's file list is a []string it built itself
		// (retention.go), not decoded JSON: listOf only accepts the decoded
		// []any shape and would silently drop every admitted member, leaving
		// the archive with nothing but the database backup. Assert the actual
		// type; textOf on a string was the identity, so appending directly is
		// the same result.
		names, _ := verdict["files"].([]string)
		admitted = append(admitted, names...)
		batch = batch[:0]
		return nil
	}
	err := filepath.WalkDir(c.dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(c.dir, path)
		if relErr != nil || rel == "." {
			return nil
		}
		name := filepath.ToSlash(rel)
		if localSkip[name] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		kind := "file"
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			kind = "symlink"
		case entry.IsDir():
			kind = "dir"
		case !entry.Type().IsRegular():
			kind = "other"
		}
		batch = append(batch, archiveInventoryEntry{Name: name, Kind: kind})
		if len(batch) >= retentionChunk {
			return flush()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return admitted, nil
}

// writeZip writes the archive: every member under run/ — the database as
// its verified backup, not the live file — plus the manifest.
func writeZip(out io.Writer, dir, backup string, names []string, index []byte) error {
	writer := zip.NewWriter(out)
	members := append([]string{"run.db"}, names...)
	for _, name := range members {
		source := filepath.Join(dir, filepath.FromSlash(name))
		if name == "run.db" {
			source = backup
		}
		body, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		entry, err := writer.Create("run/" + name)
		if err != nil {
			return err
		}
		if _, err := entry.Write(body); err != nil {
			return err
		}
	}
	manifest, err := writer.Create("archive-manifest.json")
	if err != nil {
		return err
	}
	if _, err := manifest.Write(index); err != nil {
		return err
	}
	return writer.Close()
}

// verifyArchive re-reads the finished ZIP and compares every member
// against the index under the accepted archive-verify judgement — the
// post-write verification the retention path demands.
func verifyArchive(path string, index map[string]any, proofs map[string]proofValue) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("compose: ARCHIVE_INVALID: %w", err)
	}
	defer reader.Close()
	zipNames := map[string]bool{}
	for _, file := range reader.File {
		zipNames[file.Name] = true
	}
	names := make([]string, 0, len(proofs))
	for name := range proofs {
		names = append(names, name)
	}
	sort.Strings(names)
	// The skeleton's status is raw JSON: decode it for the judgement's
	// typed status-name fact.
	statusName := ""
	if raw, ok := index["status"].(json.RawMessage); ok {
		var status struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(raw, &status) == nil {
			statusName = status.Status
		}
	}
	facts := archiveVerifyInput{
		Duplicates: len(reader.File) != len(zipNames),
		Format:     strPtr(textOf(index["format"])),
		StatusName: strPtr(statusName),
		NamesMatch: namesMatch(zipNames, names),
	}
	if verdict, err := archiveVerify(facts); err != nil {
		return err
	} else if verdict["ok"] != true {
		return fmt.Errorf("compose: %s", textOf(verdict["refusal"]))
	}
	for start := 0; start < len(names); start += retentionChunk {
		end := min(start+retentionChunk, len(names))
		entries := make([]verifyEntry, 0, end-start)
		for _, name := range names[start:end] {
			file, err := reader.Open("run/" + name)
			if err != nil {
				return fmt.Errorf("compose: ARCHIVE_INVALID: %w", err)
			}
			actual, readErr := streamProofValue(file)
			file.Close()
			if readErr != nil {
				return readErr
			}
			entries = append(entries, verifyEntry{
				Name: name, Actual: actual, Expected: proofs[name],
			})
		}
		verdict, err := archiveVerify(archiveVerifyInput{
			Duplicates: facts.Duplicates, Format: facts.Format, StatusName: facts.StatusName,
			NamesMatch: facts.NamesMatch, Entries: entries,
		})
		if err != nil {
			return err
		}
		if verdict["ok"] != true {
			return fmt.Errorf("compose: %s", textOf(verdict["refusal"]))
		}
	}
	return nil
}

// namesMatch compares the zip's member set with the manifest's.
func namesMatch(zipNames map[string]bool, files []string) bool {
	if len(zipNames) != len(files)+1 {
		return false
	}
	if !zipNames["archive-manifest.json"] {
		return false
	}
	for _, name := range files {
		if !zipNames["run/"+name] {
			return false
		}
	}
	return true
}

// jobSettlementFacts projects every supervised job of the run into the
// settlement judgement's input.
func (c *Compose) jobSettlementFacts() ([]settledJob, error) {
	var jobs []settledJob
	root := filepath.Join(c.dir, "jobs")
	tasks, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, taskDir := range tasks {
		if !taskDir.IsDir() {
			continue
		}
		walkErr := filepath.WalkDir(filepath.Join(root, taskDir.Name()), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || !entry.IsDir() || !strings.HasPrefix(entry.Name(), "turn-") {
				return nil
			}
			job, err := settledJobFacts(path)
			if err != nil {
				return err
			}
			job.Name = jobRelOf(c.dir, path)
			jobs = append(jobs, job)
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	return jobs, nil
}

// settledJobFacts reads one job directory's settlement facts.
func settledJobFacts(turnDir string) (settledJob, error) {
	var job settledJob
	_, statErr := os.Stat(filepath.Join(turnDir, "request.json"))
	job.HasRequest = statErr == nil
	_, statErr = os.Stat(filepath.Join(turnDir, "exit.json"))
	job.HasExit = statErr == nil
	for _, name := range []string{"process.json", "native-process.json"} {
		value, err := readJSONFile(filepath.Join(turnDir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return job, err
		}
		pid := intOf(getOf(value, "pid"))
		if pid <= 0 {
			job.Processes = append(job.Processes, settledProcess{PIDValid: false})
			continue
		}
		recorded := int64(pid)
		probe := "dead"
		if coordination.ProcessAlive(pid) {
			probe = "alive"
		}
		job.Processes = append(job.Processes, settledProcess{
			PID: &recorded, PIDValid: true, Probe: probe,
		})
	}
	return job, nil
}

// backupDatabase produces a consistent snapshot of the kernel's database
// beside the destination: VACUUM INTO over a fresh read-only connection.
func (c *Compose) backupDatabase(destination string) (string, error) {
	backup := destination + ".db-backup"
	if _, err := os.Stat(backup); err == nil {
		return "", fmt.Errorf("compose: backup path already exists: %s", backup)
	}
	db, err := sql.Open("sqlite", databaseURI(filepath.Join(c.dir, "run.db")))
	if err != nil {
		return "", err
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), "VACUUM INTO ?", backup); err != nil {
		os.Remove(backup)
		return "", fmt.Errorf("compose: database backup failed: %w", err)
	}
	return backup, nil
}

// databaseURI is the read-only URI of one SQLite file.
func databaseURI(path string) string {
	return "file:" + filepath.ToSlash(path) + "?mode=ro"
}

// logicalDatabaseHash hashes the backup's logical contents (schema and
// rows, table-name-ordered, row order as stored) — the drift guard the
// index carries for the database member.
func logicalDatabaseHash(path string) string {
	db, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		return ""
	}
	defer db.Close()
	var hash bytes.Buffer
	rows, err := db.Query("SELECT name, sql FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		return ""
	}
	var tables []string
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			rows.Close()
			return ""
		}
		hash.WriteString(ddl)
		hash.WriteString("\n")
		tables = append(tables, name)
	}
	rows.Close()
	for _, table := range tables {
		body, err := db.Query(`SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `"`)
		if err != nil {
			return ""
		}
		columns, _ := body.Columns()
		cells := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		for body.Next() {
			if err := body.Scan(pointers...); err != nil {
				body.Close()
				return ""
			}
			for _, cell := range cells {
				if cell == nil {
					hash.WriteString("\x00")
					continue
				}
				if raw, ok := cell.([]byte); ok {
					hash.Write(raw)
					continue
				}
				hash.WriteString(fmt.Sprintf("%v", cell))
				hash.WriteString("\x1f")
			}
			hash.WriteString("\n")
		}
		body.Close()
	}
	return digestBytes(hash.Bytes())
}

// fileProofValue computes one file's proof.
func fileProofValue(path string) (proofValue, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return proofValue{}, err
	}
	return proofValue{SHA256: digestBytes(body), Size: int64(len(body))}, nil
}

// streamProofValue computes one stream's proof.
func streamProofValue(stream io.Reader) (proofValue, error) {
	body, err := io.ReadAll(stream)
	if err != nil {
		return proofValue{}, err
	}
	return proofValue{SHA256: digestBytes(body), Size: int64(len(body))}, nil
}

// strPtr takes a string's address.
func strPtr(text string) *string { return &text }
