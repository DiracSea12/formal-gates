package harness

// The archiving composition (plan §10, K5 line "归档"): after a terminal
// judgment, the run walks the accepted retention judgement path — settlement
// (terminal status, every job receipt present, every recorded process dead),
// the destination rule, the member admission over the run's own inventory,
// and the archive index skeleton. Archiving is not an activity transition:
// nothing in the kernel's rows changes, and the physical archive (the SQLite
// backup and the zip) stays with the accepted retention path this
// composition deliberately does not duplicate.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"formal-gates/internal/coordination"
)

// ArchivePlan is the walked retention path's result: what may be archived
// and the index skeleton that describes it.
type ArchivePlan struct {
	StatusName string
	FileCount  int
	Files      []string
	Skeleton   map[string]any
}

// Archive walks the retention judgement path for the run, using the last
// terminal judgment Drive reached. A run without one (still active, waiting,
// blocked) is handed to the settlement judgement as the non-terminal name it
// is — and refused there, exactly as the accepted rule words it.
func (c *Compose) Archive(destination string) (ArchivePlan, error) {
	status := c.terminalStatus
	if status == "" {
		status = "ACTIVE"
	}
	jobs, err := c.jobSettlementFacts()
	if err != nil {
		return ArchivePlan{}, err
	}
	settled := archiveSettled(archiveSettledInput{StatusName: status, Jobs: jobs})
	if settled["ok"] != true {
		return ArchivePlan{}, fmt.Errorf("compose: %s", textOf(settled["refusal"]))
	}
	destinationVerdict := archiveDestination(archiveDestinationInput{
		Directory: c.cfg.Dir, Destination: destination})
	if destinationVerdict["ok"] != true {
		return ArchivePlan{}, fmt.Errorf("compose: %s", textOf(destinationVerdict["refusal"]))
	}
	files, err := c.archiveInventory()
	if err != nil {
		return ArchivePlan{}, err
	}
	statusJSON, err := json.Marshal(map[string]any{"status": status, "run_id": c.cfg.RunID})
	if err != nil {
		return ArchivePlan{}, err
	}
	index := archiveIndexPlanInput{
		SourceDirectory: c.cfg.Dir,
		RunID:           c.cfg.RunID,
		Revision:        "",
		RuntimeIdentity: c.binary,
		Status:          statusJSON,
		// The logical database hash belongs to the physical archiving step
		// (the SQLite backup the accepted retention path owns); the K5
		// composition walks the judgements only and leaves it empty.
		DatabaseLogicalSHA256: "",
	}
	plan, err := archiveIndexPlan(index)
	if err != nil {
		return ArchivePlan{}, err
	}
	if plan["ok"] != true {
		return ArchivePlan{}, fmt.Errorf("compose: archive index plan refused")
	}
	return ArchivePlan{
		StatusName: status,
		FileCount:  len(files),
		Files:      files,
		Skeleton:   objectOrEmpty(plan["skeleton"]),
	}, nil
}

// jobSettlementFacts projects every supervised job of the run into the
// settlement judgement's input: request and receipt presence, and each
// recorded process's liveness probe.
func (c *Compose) jobSettlementFacts() ([]settledJob, error) {
	var jobs []settledJob
	jobRoot := filepath.Join(c.cfg.Dir, "jobs")
	taskDirs, err := os.ReadDir(jobRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, taskDir := range taskDirs {
		if !taskDir.IsDir() {
			continue
		}
		invDirs, err := os.ReadDir(filepath.Join(jobRoot, taskDir.Name()))
		if err != nil {
			return nil, err
		}
		for _, invDir := range invDirs {
			if !invDir.IsDir() {
				continue
			}
			turns, err := latestTurnDir(filepath.Join(jobRoot, taskDir.Name(), invDir.Name()))
			if err != nil {
				return nil, err
			}
			if turns == "" {
				continue
			}
			job, err := settledJobFacts(turns)
			if err != nil {
				return nil, err
			}
			job.Name = filepath.Join(taskDir.Name(), invDir.Name())
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// settledJobFacts reads one turn directory's settlement facts.
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
		pidNumber, ok := getOf(value, "pid").(json.Number)
		if !ok {
			job.Processes = append(job.Processes, settledProcess{PIDValid: false})
			continue
		}
		pid, err := pidNumber.Int64()
		if err != nil || pid <= 0 {
			job.Processes = append(job.Processes, settledProcess{PIDValid: false})
			continue
		}
		spid := pid
		job.Processes = append(job.Processes, settledProcess{
			PID:      &spid,
			PIDValid: true,
			// The judgement treats an alive recorded process as unsettled
			// regardless of which record named it.
			Probe: processLiveness(int(pid)),
		})
	}
	return job, nil
}

// processLiveness is the liveness fact the settlement judgement consumes:
// the recorded PID is dead or alive; an uncheckable one reports alive-with-
// doubt as "permission", the judgement's cannot-check refusal.
func processLiveness(pid int) string {
	switch {
	case coordination.ProcessAlive(pid):
		return "alive"
	default:
		return "dead"
	}
}

// archiveInventory walks the run directory and applies the member admission
// judgement over batches: the walk's order, the skip names, and the symlink
// refusal all stay the accepted rule's.
func (c *Compose) archiveInventory() ([]string, error) {
	var files []string
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
		// The judgement returns its files as a []string here (no bridge
		// crossing happened); accept both spellings.
		switch names := verdict["files"].(type) {
		case []string:
			files = append(files, names...)
		case []any:
			for _, name := range names {
				files = append(files, textOf(name))
			}
		}
		batch = batch[:0]
		return nil
	}
	root := c.cfg.Dir
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
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
		batch = append(batch, archiveInventoryEntry{Name: filepath.ToSlash(rel), Kind: kind})
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
	return files, nil
}
