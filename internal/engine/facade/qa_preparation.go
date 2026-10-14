package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
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
	for _, operation := range p.QA.Preparation.Operations {
		if operation.Kind != coverage.OperationArgv || len(operation.Argv) != 1 || operation.Stdin != "" {
			return "", nil
		}
		if !strings.ContainsAny(operation.Argv[0], `/\`) {
			return "", nil
		}
	}
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
		identity, _, err := preparationPathIdentity(filepath.Join(workspace.Path, filepath.FromSlash(path)))
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
		program := operation.Argv[0]
		path := program
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace.Path, p.QA.Directory, path)
		}
		identity, script, err := preparationPathIdentity(path)
		if err != nil {
			return "", err
		}
		if script {
			return "", nil
		}
		tools[program] = identity
	}
	step := *p.QA.Preparation
	step.ID = ""
	return preparationDigest(struct {
		Inputs, Tools map[string]string
		Environment   string
		Step          coverage.SharedStep
	}{inputs, tools, preparationDigest(environment), step}), nil
}

func preparationPathIdentity(path string) (identity string, script bool, err error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "missing", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		return "directory", false, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	return preparationDigest(struct {
		Mode fs.FileMode
		Body []byte
	}{info.Mode(), body}), len(body) >= 2 && body[0] == '#' && body[1] == '!', nil
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
			artifacts[filepath.ToSlash(key)], _, err = preparationPathIdentity(path)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("declared preparation artifact %s: %w", name, err)
		}
	}
	return artifacts, nil
}

func preparationCaseIntent(s *protocol.State, p protocol.CheckParameters, ordinal int) (protocol.HostActionIntent, bool, error) {
	qa := *p.QA
	qa.PreparationOrdinal = ordinal
	p.QA = &qa
	for _, intent := range s.CommonPreparationIntents(p) {
		var issued protocol.CheckParameters
		if err := decodeNativeParams(intent, &issued); err != nil {
			return protocol.HostActionIntent{}, false, err
		}
		if issued.Binding == p.Binding && issued.QA.CaseID == p.QA.CaseID && issued.QA.Source == p.QA.Source {
			return intent, true, nil
		}
	}
	return protocol.HostActionIntent{}, false, nil
}

// reusePreparation observes the original admitted job and preserves all of its
// execution identities. A preparing or unknown original job never grants a launch.
func (f *Facade) reusePreparation(s *protocol.State, intent protocol.HostActionIntent, p protocol.CheckParameters, input string) (nativeResult, bool, bool, error) {
	prefixSource := ""
	if input != "" && p.QA.PreparationOrdinal > 0 {
		prefix, found, err := preparationCaseIntent(s, p, 0)
		if err != nil || !found {
			return nativeResult{}, false, false, err
		}
		receipt, recorded := s.HostActionReceipts[prefix.ActionID]
		if !recorded {
			return nativeResult{}, false, true, nil
		}
		var evidence protocol.CheckEvidence
		if err := decodeReceiptEvidence(receipt, &evidence); err != nil {
			return nativeResult{}, false, false, err
		}
		prefixSource = evidence.PreparationSourceAction
		if prefixSource == "" {
			return nativeResult{}, false, false, nil
		}
	}
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
		if input != "" && request["qaPreparationInput"] != input {
			continue
		}
		receipt, recorded := s.HostActionReceipts[original.ActionID]
		if !recorded {
			return nativeResult{}, false, true, nil
		}
		if input == "" {
			continue
		}
		var prior protocol.CheckEvidence
		if err := decodeReceiptEvidence(receipt, &prior); err != nil {
			return nativeResult{}, false, false, err
		}
		if prior.Error != "" || prior.PreparationSourceAction != "" || prior.PreparationInput != input {
			continue
		}
		var originalParams protocol.CheckParameters
		if err := decodeNativeParams(original, &originalParams); err != nil {
			return nativeResult{}, false, false, err
		}
		if prefixSource != "" {
			prefix, found, err := preparationCaseIntent(s, originalParams, 0)
			if err != nil {
				return nativeResult{}, false, false, err
			}
			if !found || prefix.ActionID != prefixSource {
				continue
			}
		}
		last := len(p.QA.Preparation.Operations) - 1
		final, found, err := preparationCaseIntent(s, originalParams, last)
		if err != nil {
			return nativeResult{}, false, false, err
		}
		if !found {
			continue
		}
		finalReceipt, recorded := s.HostActionReceipts[final.ActionID]
		if !recorded {
			return nativeResult{}, false, true, nil
		}
		var complete protocol.CheckEvidence
		if err := decodeReceiptEvidence(finalReceipt, &complete); err != nil {
			return nativeResult{}, false, false, err
		}
		if finalReceipt.Status != protocol.HostActionStatusExecuted || complete.Error != "" || complete.PreparationSourceAction != "" || complete.PreparationInput != input {
			continue
		}
		qa := *p.QA
		qa.PreparationOrdinal = last
		finished := p
		finished.QA = &qa
		artifacts, err := preparationArtifacts(finished)
		if err != nil || preparationDigest(artifacts) != preparationDigest(complete.PreparationArtifacts) {
			continue
		}
		invoked := false
		observed, done, err := f.checkEvidence(s, final, false, &invoked)
		if err != nil || !done {
			return nativeResult{}, false, true, err
		}
		if observed.Check == nil || preparationDigest(*observed.Check) != preparationDigest(complete) {
			return nativeResult{}, false, false, fmt.Errorf("shared preparation final evidence differs")
		}
		if final.ActionID == original.ActionID {
			prior.PreparationSourceAction = original.ActionID
			return nativeResult{Check: &prior}, true, true, nil
		}
		observed, done, err = f.checkEvidence(s, original, false, &invoked)
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
