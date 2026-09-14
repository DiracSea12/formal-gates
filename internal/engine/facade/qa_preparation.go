package facade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

// Shared preparation is an observation of qa.prepare, not another task or
// result ledger. Text-only steps are handed back for interpretation.
func (f *Facade) prepareShared(ctx context.Context, run Run, op protocol.TaskOperation, result *protocol.TaskOperationReceipt) error {
	// Text-only instructions do not change executable preparation inputs.
	steps := op.Preparation[:0:0]
	for _, step := range op.Preparation {
		if step.Command != nil {
			step.Procedure = ""
			steps = append(steps, step)
		}
	}
	op.Preparation = steps
	var executable bool
	independent := false
	for _, step := range op.Preparation {
		executable = executable || step.Command != nil
		independent = independent || step.Independent
	}
	if !executable {
		return nil
	}
	if err := f.createOperationResources(op); err != nil {
		return err
	}
	source := f.root
	repair := op.Request.Binding.Task.Step == "repair"
	if repair {
		source = run.Regular.Preparation.Resources[op.Request.Binding.PrerequisiteDigest]
	}
	var view gitprep.View
	var err error
	if repair {
		view, err = gitprep.WorkingView(source, op.Request.Binding.Candidate)
	} else {
		view, err = gitprep.Snapshot(source, op.Request.Binding.Candidate)
	}
	if err != nil {
		return err
	}
	// Bind the inherited tool environment without persisting its values (which
	// can contain credentials). Actual executable content covers in-place updates.
	var env []string
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		switch name {
		case "CODEX_THREAD_ID", "CODEX_SESSION_ID", "_", "SHLVL", "PWD", "OLDPWD":
			continue
		}
		env = append(env, entry)
	}
	sort.Strings(env)
	tools := map[string]string{}
	for _, step := range op.Preparation {
		if step.Command == nil {
			continue
		}
		program := step.Command.Executable
		if strings.ContainsAny(program, `/\`) && !filepath.IsAbs(program) {
			found := false
			for _, entry := range view.Entries {
				if entry.Path == filepath.ToSlash(filepath.Clean(program)) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("PREREQUISITE_FAILED: preparation tool %s is absent from candidate inputs", program)
			}
			continue // source-relative executable content is already in the view
		}
		path, err := exec.LookPath(program)
		if err != nil {
			return fmt.Errorf("PREREQUISITE_FAILED: preparation tool %s: %w", program, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		tools[program] = hex.EncodeToString(sum[:])
	}
	keyInput := struct {
		View        string
		Steps       any
		Environment []string
		Tools       map[string]string
		Independent string
	}{view.Digest, op.Preparation, env, tools, ""}
	if independent {
		keyInput.Independent = op.ID
	}
	// Each completed prefix is an existing shared preparation receipt. Later
	// private or independent steps branch from it instead of rebuilding it.
	var prefix protocol.TaskOperationReceipt
	if len(op.Preparation) > 1 {
		prior := op
		prior.Preparation = op.Preparation[:len(op.Preparation)-1]
		if err := f.prepareShared(ctx, run, prior, &prefix); err != nil {
			return err
		}
	}
	result.InputDigest = gitprep.Digest(keyInput)
	result.SharedWorkspace = filepath.Join(f.stateDir, "resources", "qa-shared", strings.TrimPrefix(result.InputDigest, "sha256:"))
	// Register the owned path before work begins. Recovery can reconcile the
	// original preparation without invoking a partially observed command again.
	job := f.taskOperationDir(op.ID)
	if err := writeJSONAtomic(filepath.Join(job, "preparation-input.json"), result); err != nil {
		return err
	}
	var unlock func()
	for {
		unlock, err = coordination.AcquirePath(result.SharedWorkspace+".lock", "shared QA preparation")
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "lock held:") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer unlock()
	marker := filepath.Join(result.SharedWorkspace, ".qa-prepared.json")
	if data, err := os.ReadFile(marker); err == nil {
		var observed protocol.TaskOperationReceipt
		if json.Unmarshal(data, &observed) != nil || observed.InputDigest != result.InputDigest {
			return fmt.Errorf("INTENT_MISMATCH: shared preparation binding differs")
		}
		actual, err := sharedArtifacts(result.SharedWorkspace, op)
		if err != nil {
			return err
		}
		if !maps.Equal(actual, observed.Artifacts) {
			return fmt.Errorf("EXTERNAL_CHANGED: shared preparation artifacts differ")
		}
		result.Artifacts = actual
		result.PreparationReused = true
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(result.SharedWorkspace); err == nil {
		return fmt.Errorf("UNKNOWN: shared preparation has no completion observation: %s", result.SharedWorkspace)
	} else if !os.IsNotExist(err) {
		return err
	}
	if repair {
		_, err = gitprep.WorkingCopy(f.root, source, result.SharedWorkspace, op.Request.Binding.Candidate)
	} else {
		err = gitprep.Workspace(f.root, result.SharedWorkspace, op.Request.Binding.Candidate)
	}
	if err != nil {
		return err
	}
	if repair {
		copied, err := gitprep.WorkingView(result.SharedWorkspace, op.Request.Binding.Candidate)
		if err != nil || copied.Digest != view.Digest {
			return fmt.Errorf("EXTERNAL_CHANGED: repair inputs changed during shared preparation: %v", err)
		}
	}
	if prefix.SharedWorkspace != "" {
		if err := copyPreparationPrefix(prefix.SharedWorkspace, result.SharedWorkspace); err != nil {
			return err
		}
	}
	for i, step := range op.Preparation {
		if prefix.SharedWorkspace != "" && i < len(op.Preparation)-1 {
			continue
		}
		if step.Command == nil {
			continue
		}
		stepJob := filepath.Join(f.taskOperationDir(op.ID), "preparation", fmt.Sprint(i))
		if err := os.MkdirAll(stepJob, 0700); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, step.Command.Executable, step.Command.Args...)
		cmd.Dir = result.SharedWorkspace
		cmd.Env = f.operationEnvironment(op, result.SharedWorkspace)
		observation := protocol.TaskOperationReceipt{}
		code, err := runOperationProcess(cmd, stepJob, &observation)
		if err != nil {
			return err
		}
		if code != step.Command.ExpectedExitCode {
			return fmt.Errorf("PREREQUISITE_FAILED: shared step %s exit %d, expected %d; evidence %s", step.ID, code, step.Command.ExpectedExitCode, stepJob)
		}
	}
	result.Artifacts, err = sharedArtifacts(result.SharedWorkspace, op)
	if err != nil {
		return err
	}
	return writeJSONAtomic(marker, result)
}

// Only declared delivered files are checked, not the whole mutable build tree.
func sharedArtifacts(root string, op protocol.TaskOperation) (map[string]string, error) {
	result := map[string]string{}
	for _, step := range op.Preparation {
		for _, name := range step.Artifacts {
			if _, err := gitprep.Paths([]string{name}); err != nil {
				return nil, err
			}
			data, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				return nil, fmt.Errorf("PREREQUISITE_FAILED: artifact %s: %w", name, err)
			}
			sum := sha256.Sum256(data)
			result[name] = hex.EncodeToString(sum[:])
		}
	}
	return result, nil
}

// Keep the new worktree's Git binding and copy the actual prepared tree,
// including ignored build products. Declared artifacts alone need not include
// intermediate files consumed by the next preparation step.
func copyPreparationPrefix(source, target string) error {
	entries, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != ".git" {
			if err := os.RemoveAll(filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == ".git" || rel == ".qa-prepared.json" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		destination := filepath.Join(target, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(destination, info.Mode().Perm())
		}
		if err := os.RemoveAll(destination); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, destination)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}
