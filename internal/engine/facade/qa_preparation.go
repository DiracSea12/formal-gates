package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func preparationDigest(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Preparation inputs include actual source bytes and uncommitted inputs.
// Declared untracked outputs are products, never replacements for tracked input.
func (f *Facade) qaPreparationInput(s *protocol.State, p protocol.CheckParameters, request map[string]any) (string, error) {
	workspace := s.NativeResources[p.WorkspaceID]
	native := f.vcs.At(workspace.Path, workspace.Handle)
	entries, err := native.TreeEntries(p.SourceIdentity)
	if err != nil {
		return "", err
	}
	paths := []string{}
	for path := range entries {
		if path != "." {
			paths = append(paths, path)
		}
	}
	changes, err := native.WorkingChanges()
	if err != nil {
		return "", err
	}
	for _, path := range changes {
		if _, tracked := entries[path]; tracked {
			continue
		}
		output := slices.ContainsFunc(p.QA.OutputArtifacts, func(name string) bool {
			return path == name || strings.HasPrefix(path, name+"/")
		})
		if !output {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	inputs := map[string]string{}
	for _, path := range paths {
		identity, err := preparationPathIdentity(filepath.Join(workspace.Path, filepath.FromSlash(path)))
		if err != nil {
			return "", err
		}
		inputs[path] = identity
	}
	environment := map[string]string{}
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		switch name {
		case "_", "SHLVL", "PWD", "OLDPWD", "CODEX_THREAD_ID", "CODEX_SESSION_ID":
			continue
		}
		environment[name] = value
	}
	for name, value := range request["environment"].(map[string]any) {
		if strings.HasPrefix(name, "FORMAL_GATES_QA_") || name == "TMPDIR" || name == "TMP" || name == "TEMP" {
			delete(environment, name)
			continue
		}
		environment[name], _ = value.(string)
	}
	tools := map[string]string{}
	for _, operation := range p.QA.Preparation.Operations {
		programs := operation.Argv[:0:0]
		if operation.Kind == coverage.OperationArgv {
			programs = append(programs, operation.Argv[0])
		} else {
			programs = append(programs, "/bin/sh")
			// Available executable words also cover the usual shell toolchain
			// (for example, go in a reviewed "go build" command).
			for _, word := range strings.Fields(operation.Command) {
				programs = append(programs, strings.Trim(word, ";|&\"'"))
			}
		}
		for _, program := range programs {
			path := program
			if !filepath.IsAbs(path) && strings.ContainsAny(path, `/\`) {
				path = filepath.Join(workspace.Path, path)
			} else if !filepath.IsAbs(path) {
				path, err = exec.LookPath(program)
				if err != nil {
					continue
				}
			}
			if operation.Kind == coverage.OperationShell {
				info, err := os.Stat(path)
				if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
					continue
				}
			}
			identity, err := preparationPathIdentity(path)
			if err != nil {
				return "", err
			}
			tools[program] = identity
		}
	}
	step := *p.QA.Preparation
	step.ID = ""
	return preparationDigest(struct {
		Inputs, Tools map[string]string
		Environment   string
		Step          coverage.SharedStep
		Ordinal       int
	}{inputs, tools, preparationDigest(environment), step, p.QA.PreparationOrdinal}), nil
}

func preparationPathIdentity(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "directory", nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return preparationDigest(struct {
		Mode fs.FileMode
		Body []byte
	}{info.Mode(), body}), nil
}

func preparationArtifacts(p protocol.CheckParameters) (map[string]string, error) {
	if p.QA.PreparationOrdinal != len(p.QA.Preparation.Operations)-1 {
		return nil, nil
	}
	artifacts := map[string]string{}
	for _, name := range p.QA.Preparation.Artifacts {
		root := filepath.Join(p.QA.WorkspaceRoot, filepath.FromSlash(name))
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			key, err := filepath.Rel(p.QA.WorkspaceRoot, path)
			if err != nil {
				return err
			}
			artifacts[filepath.ToSlash(key)], err = preparationPathIdentity(path)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("declared preparation artifact %s: %w", name, err)
		}
	}
	return artifacts, nil
}

// reusePreparation observes the original admitted job and preserves all of its
// execution identities. A preparing or unknown original job never grants a launch.
func (f *Facade) reusePreparation(s *protocol.State, intent protocol.HostActionIntent, p protocol.CheckParameters, input string) (nativeResult, bool, bool, error) {
	for _, original := range s.CommonPreparationIntents(p) {
		if original.ActionID == intent.ActionID {
			continue
		}
		directory := filepath.Join(f.stateDir, "checks", strings.ReplaceAll(original.ActionID, ":", "-"))
		var request map[string]any
		if err := readCheckDocument(filepath.Join(directory, "request.json"), &request); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nativeResult{}, false, false, err
		}
		if request["qaPreparationInput"] != input {
			continue
		}
		receipt, recorded := s.HostActionReceipts[original.ActionID]
		if !recorded {
			return nativeResult{}, false, true, nil
		}
		var prior protocol.CheckEvidence
		if err := decodeReceiptEvidence(receipt, &prior); err != nil {
			return nativeResult{}, false, false, err
		}
		if prior.Error != "" || prior.PreparationSourceAction != "" || prior.PreparationInput != input {
			continue
		}
		artifacts, err := preparationArtifacts(p)
		if err != nil || preparationDigest(artifacts) != preparationDigest(prior.PreparationArtifacts) {
			continue
		}
		invoked := false
		observed, done, err := f.checkEvidence(s, original, false, &invoked)
		if err != nil || !done {
			return nativeResult{}, false, true, err
		}
		if observed.Check == nil || preparationDigest(*observed.Check) != preparationDigest(prior) {
			return nativeResult{}, false, false, fmt.Errorf("shared preparation original evidence differs")
		}
		prior.PreparationSourceAction = original.ActionID
		return nativeResult{Check: &prior}, true, true, nil
	}
	return nativeResult{}, false, false, nil
}
