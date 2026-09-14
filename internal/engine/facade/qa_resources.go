package facade

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

func (f *Facade) operationResources(op protocol.TaskOperation) protocol.TaskResources {
	r := protocol.ResourcesForTask(op.ID)
	if op.Resources != nil {
		r = *op.Resources
	}
	r.Root = filepath.Join(f.stateDir, r.Root)
	r.Work = filepath.Join(f.stateDir, r.Work)
	r.Temp = filepath.Join(f.stateDir, r.Temp)
	r.Output = filepath.Join(f.stateDir, r.Output)
	r.Cache = filepath.Join(f.stateDir, r.Cache)
	return r
}

func (f *Facade) createOperationResources(op protocol.TaskOperation) error {
	r := f.operationResources(op)
	for _, p := range []string{r.Temp, r.Output, r.Cache} {
		if err := os.MkdirAll(p, 0700); err != nil {
			return err
		}
	}
	return nil
}

func (f *Facade) operationEnvironment(op protocol.TaskOperation, shared string) []string {
	r := f.operationResources(op)
	values := map[string]string{
		"FORMAL_GATES_QA_RUN":           op.Request.RunID,
		"FORMAL_GATES_QA_CANDIDATE":     op.Request.Binding.Candidate,
		"FORMAL_GATES_QA_TASK":          op.Request.Binding.ActionID,
		"FORMAL_GATES_QA_RESOURCE_ROOT": r.Root,
		"FORMAL_GATES_QA_WORK":          r.Work,
		"FORMAL_GATES_QA_TMP":           r.Temp,
		"FORMAL_GATES_QA_OUTPUT":        r.Output,
		"FORMAL_GATES_QA_CACHE":         r.Cache,
		"FORMAL_GATES_QA_SHARED":        shared,
		"TMPDIR":                        r.Temp, "TMP": r.Temp, "TEMP": r.Temp,
	}
	// Tool caches belong to the project environment. Only the generic QA
	// resource variables and temporary directory are owned by this operation.
	env := []string{}
	for _, entry := range os.Environ() {
		if _, owned := values[strings.SplitN(entry, "=", 2)[0]]; !owned {
			env = append(env, entry)
		}
	}
	for name, value := range values {
		env = append(env, name+"="+value)
	}
	return env
}

// Operation streams and their process/receipt binding are already written
// outside the resource root. Only release after collection and all declared
// consumers in this execution have also collected. Missing consumers retain
// the workspace until terminal reconciliation.
func (f *Facade) releaseCollectedResources(run Run) error {
	if run.Regular == nil || run.Regular.Preparation == nil {
		return nil
	}
	for _, x := range run.Executions {
		if !x.Finished {
			continue
		}
		collected := func(id string) bool {
			op := latestTaskOperation(x, "qa.collect", id)
			return op != nil && op.Receipt != nil && op.Receipt.Status == "COMPLETED"
		}
		for _, op := range x.Operations {
			if op.Request.Kind != "qa.prepare" || op.Resources == nil || !collected(op.Request.CaseID) {
				continue
			}
			keep := false
			for _, pending := range x.Operations {
				if pending.Request.CaseID == op.Request.CaseID && pending.Receipt == nil {
					keep = true
				}
			}
			for _, qa := range run.Regular.Preparation.QA {
				if qa == nil || qa.Design == nil {
					keep = true
					continue
				}
				for _, c := range qa.Design.Manifest.Cases {
					for _, dep := range c.Dependencies {
						if dep == op.Request.CaseID && !collected(c.CaseID) {
							keep = true
						}
					}
				}
			}
			if keep {
				continue
			}
			r := f.operationResources(op)
			if err := f.saveQAOutput(op); err != nil {
				return fmt.Errorf("CLEANUP_PENDING: %s: %w", op.ID, err)
			}
			if err := f.removeQAWorkspace(r.Work); err != nil {
				return fmt.Errorf("CLEANUP_PENDING: %s: %w", op.ID, err)
			}
			if err := os.RemoveAll(r.Root); err != nil {
				return fmt.Errorf("CLEANUP_PENDING: %s: %w", op.ID, err)
			}
		}
	}
	return nil
}

func (f *Facade) removeQAWorkspace(path string) error {
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return gitprep.Cleanup(f.root, path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.RemoveAll(path)
}

// Reconcile the intent-owned roots, including prepare interrupted before a
// receipt. Shared worktrees must be deregistered before removing their parent.
func (f *Facade) cleanupQAResources(run Run) error {
	for _, x := range run.Executions {
		for _, op := range x.Operations {
			if op.Request.Kind != "qa.prepare" {
				continue
			}
			if err := f.saveQAOutput(op); err != nil {
				return err
			}
			if err := f.removeQAWorkspace(f.operationResources(op).Work); err != nil {
				return err
			}
		}
	}
	shared := filepath.Join(f.stateDir, "resources", "qa-shared")
	entries, err := os.ReadDir(shared)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if err := f.removeQAWorkspace(filepath.Join(shared, entry.Name())); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{"qa-operations", "qa-shared", "qa-cache"} {
		if err := os.RemoveAll(filepath.Join(f.stateDir, "resources", name)); err != nil {
			return err
		}
	}
	return nil
}

// Preserve only command evidence and accepted operation receipts, not the
// preparation scene or auxiliary native invocation material.
func (f *Facade) cleanupTaskOperationFiles() error {
	root := filepath.Join(f.stateDir, "artifacts", "task-operations")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if !entry.IsDir() {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		files, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, file := range files {
			switch file.Name() {
			case "stdout.log", "stderr.log", "process.json", "observed.json", "preparation":
				continue
			}
			if err := os.RemoveAll(filepath.Join(path, file.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// The designated output directory is the explicit delivery surface. Copy only
// those files; work/tmp/source and credentials are never archived wholesale.
// Atomic per-file replacement makes interrupted preservation retryable.
func (f *Facade) saveQAOutput(op protocol.TaskOperation) error {
	source := f.operationResources(op).Output
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	destination := filepath.Join(f.stateDir, "artifacts", "qa-output", strings.TrimPrefix(op.ID, "hact:task:"))
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("output is not a regular delivery file: %s", path)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.CreateTemp(filepath.Dir(target), ".delivery-*")
		if err != nil {
			return err
		}
		tmp := out.Name()
		defer os.Remove(tmp)
		_, copyErr := io.Copy(out, in)
		if err = out.Close(); copyErr != nil {
			return copyErr
		}
		if err != nil {
			return err
		}
		return os.Rename(tmp, target)
	})
}

// Prepare commands use the same process observation as targets. Abort stops
// those owned subprocesses before waiting for the prepare operation lock.
func (f *Facade) stopPreparationProcesses(job string) error {
	paths, err := filepath.Glob(filepath.Join(job, "preparation", "*", "process.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var p struct {
			PID     int  `json:"pid"`
			Stopped bool `json:"stopped"`
		}
		if err := json.Unmarshal(data, &p); err != nil || p.PID <= 0 {
			return fmt.Errorf("UNKNOWN: invalid preparation process observation: %s", path)
		}
		if p.Stopped || !codexProcessAlive(p.PID) {
			continue
		}
		if err := stopCodexProcess(p.PID); err != nil {
			return err
		}
		deadline := time.Now().Add(3 * time.Second)
		for codexProcessAlive(p.PID) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if codexProcessAlive(p.PID) {
			return fmt.Errorf("HOST_ACTION_RUNNING: preparation process stopping")
		}
	}
	return nil
}

// Native invocation requests may own short transport directories outside the
// run tree. Read their recorded paths, never infer a platform or a temp layout.
func cleanupNativeInvocationResources(job string) error {
	data, err := os.ReadFile(filepath.Join(job, "session-request.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var request struct {
		Resources []string `json:"resources"`
	}
	if err := json.Unmarshal(data, &request); err != nil {
		return err
	}
	for _, path := range request.Resources {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}
