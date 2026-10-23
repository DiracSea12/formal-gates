package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
)

const nativeWorkspaceTool = "workspace"
const nativeSemanticTool = "submit_semantics"
const nativeAllowedMCPTools = "mcp__formal-gates__" + nativeWorkspaceTool + ",mcp__formal-gates__" + nativeSemanticTool
const nativeToolCallsFile = "native-tool-calls.jsonl"
const nativeToolContextFile = "native-tool-context.json"

// Published listings and reads are tool evidence. Their positions retain the
// original paths and bytes across native process restarts; they are not run state.
type nativeToolFact struct {
	Action        string   `json:"action"`
	Workspace     string   `json:"workspace"`
	Job           string   `json:"job"`
	RequestDigest string   `json:"requestDigest"`
	Kind          string   `json:"kind"`
	Paths         []string `json:"paths,omitempty"`
	Content       []byte   `json:"content,omitempty"`
}

type nativeWorkspaceTools struct {
	runtimeFiles                                          vcs.RuntimeFiles
	directory, workspace, action, requestDigest, evidence string
	provider                                              string
	readOnly                                              bool
	facts                                                 []nativeToolFact
	services                                              []ExecutionServices
	material                                              *worker.Material
	semanticSchema                                        map[string]any
	release                                               func()
}

type nativeToolContext struct {
	Action        string           `json:"action"`
	Workspace     string           `json:"workspace"`
	RequestDigest string           `json:"requestDigest"`
	Facts         []nativeToolFact `json:"facts"`
}

func verifyNativeToolFact(fact nativeToolFact, directory, action, workspace string) error {
	if fact.Action != action || fact.Workspace != workspace {
		return fmt.Errorf("published native tool facts belong to another action/workspace")
	}
	owner, originalOwner := directory, fact.Job
	if action != "" {
		owner, originalOwner = filepath.Dir(directory), filepath.Dir(fact.Job)
	}
	if resolveNativePath(owner) != resolveNativePath(originalOwner) {
		return fmt.Errorf("published native tool facts belong to another job owner")
	}
	original, err := fileProofOf(filepath.Join(fact.Job, "native-request.json"))
	if err != nil || textOf(original["sha256"]) != fact.RequestDigest {
		return fmt.Errorf("published native tool facts differ from their original request")
	}
	request, err := readJSONFile(filepath.Join(fact.Job, "native-request.json"))
	if err != nil {
		return err
	}
	if resolveNativePath(textOf(getOf(request, "workspace"))) != workspace {
		return fmt.Errorf("published tool target differs from original workspace")
	}
	if action != "" {
		if textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "actionId")) != action {
			return fmt.Errorf("published native tool fact has no original issued action")
		}
	}
	return nil
}

func newNativeWorkspaceTools(directory string, services ...ExecutionServices) (*nativeWorkspaceTools, error) {
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, err
	}
	workspace := textOf(getOf(request, "workspace"))
	readOnly, ok := getOf(request, "read_only").(bool)
	if !ok || !filepath.IsAbs(workspace) {
		return nil, fmt.Errorf("native tools require the original workspace and read-only policy")
	}
	proof, err := fileProofOf(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, err
	}
	t := &nativeWorkspaceTools{provider: textOf(getOf(request, "provider")), directory: directory, workspace: resolveNativePath(workspace), readOnly: readOnly, requestDigest: textOf(proof["sha256"]), evidence: filepath.Join(directory, "tool-facts.jsonl")}
	if raw, err := json.Marshal(getOf(request, "runtime_files")); err != nil {
		return nil, err
	} else if err := json.Unmarshal(raw, &t.runtimeFiles); err != nil {
		return nil, err
	}
	t.services = services
	t.semanticSchema = objectOrEmpty(getOf(request, "schema"))
	if getOf(request, "engine") != nil {
		materialPath := textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "path"))
		body, err := os.ReadFile(materialPath)
		if err != nil {
			return nil, err
		}
		var material worker.Material
		if err := json.Unmarshal(body, &material); err != nil {
			return nil, err
		}
		if material.ReadOnly != readOnly {
			return nil, fmt.Errorf("native tools policy differs from issued material")
		}
		t.action = material.Input.Pending.ActionID
		t.material = &material
		if material.Input.Validation != nil && material.Input.Validation.Workspace != nil && resolveNativePath(material.Input.Validation.Workspace.Path) != t.workspace {
			return nil, fmt.Errorf("native tools workspace differs from issued material")
		}
		// The native action directory is shared by its material and signed resumes.
		t.evidence = filepath.Join(filepath.Dir(directory), "tool-facts.jsonl")
	}
	return t, t.refreshFacts()
}

func (t *nativeWorkspaceTools) refreshFacts() error {
	body, err := os.ReadFile(t.evidence)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	facts := []nativeToolFact{}
	scan := bufio.NewScanner(bytes.NewReader(body))
	scan.Buffer(make([]byte, 4096), int(^uint(0)>>1))
	for scan.Scan() {
		var fact nativeToolFact
		if err := json.Unmarshal(scan.Bytes(), &fact); err != nil {
			return err
		}
		if err := verifyNativeToolFact(fact, t.directory, t.action, t.workspace); err != nil {
			return err
		}
		facts = append(facts, fact)
	}
	if err := scan.Err(); err != nil {
		return err
	}
	t.facts = facts
	return nil
}

func (t *nativeWorkspaceTools) recordCall(request json.RawMessage, result any) error {
	context := nativeToolContext{Action: t.action, Workspace: t.workspace, RequestDigest: t.requestDigest, Facts: t.facts}
	if err := writeAtomicJSON(filepath.Join(t.directory, nativeToolContextFile), context); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"request": request, "result": result})
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(t.directory, nativeToolCallsFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(append(body, '\n'))
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func nativeToolEvidenceFiles(directory string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(directory, nativeToolCallsFile)); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	files := []string{nativeToolCallsFile, nativeToolContextFile}
	facts, err := nativeToolFacts(directory)
	if err != nil {
		return nil, err
	}
	for index, fact := range facts {
		if fact.Kind != "edit" {
			continue
		}
		checks, _, err := editFactChecks(fact)
		if err != nil {
			return nil, err
		}
		for check := range checks {
			relative, err := filepath.Rel(directory, editCheckDirectory(fact, index+1, check))
			if err != nil {
				return nil, err
			}
			for _, name := range []string{"request.json", "exit.json", "stdout.log", "stderr.log"} {
				files = append(files, filepath.Join(relative, name))
			}
		}
	}
	return files, nil
}

func verifyNativeToolEvidence(directory string) error {
	files, err := nativeToolEvidenceFiles(directory)
	if err != nil || len(files) == 0 {
		return err
	}
	body, err := os.ReadFile(filepath.Join(directory, nativeToolContextFile))
	if err != nil {
		return err
	}
	var context nativeToolContext
	if err := json.Unmarshal(body, &context); err != nil {
		return err
	}
	proof, err := fileProofOf(filepath.Join(directory, "native-request.json"))
	if err != nil || textOf(proof["sha256"]) != context.RequestDigest {
		return fmt.Errorf("native tool context differs from original request")
	}
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return err
	}
	if resolveNativePath(textOf(getOf(request, "workspace"))) != context.Workspace {
		return fmt.Errorf("native tool context differs from original workspace")
	}
	if context.Action != "" && textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "actionId")) != context.Action {
		return fmt.Errorf("native tool context differs from original action")
	}
	for _, fact := range context.Facts {
		if err := verifyNativeToolFact(fact, directory, context.Action, context.Workspace); err != nil {
			return err
		}
	}
	return verifyEditCheckFacts(context.Facts)
}

func (t *nativeWorkspaceTools) publish(f nativeToolFact) (int, error) {
	f.Action, f.Workspace, f.RequestDigest = t.action, t.workspace, t.requestDigest
	f.Job = t.directory
	// Fact paths are repository-relative; store them with canonical slash
	// separators so assembled references validate on any host filesystem.
	for i, p := range f.Paths {
		f.Paths[i] = filepath.ToSlash(p)
	}
	body, err := json.Marshal(f)
	if err != nil {
		return 0, err
	}
	file, err := os.OpenFile(t.evidence, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	_, err = file.Write(append(body, '\n'))
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return 0, err
	}
	if closeErr != nil {
		return 0, closeErr
	}
	t.facts = append(t.facts, f)
	return len(t.facts), nil
}

func (t *nativeWorkspaceTools) publishRead(relative string, body []byte) (map[string]any, error) {
	number, err := t.publish(nativeToolFact{Kind: "read", Paths: []string{relative}, Content: body})
	lines := strings.SplitAfter(string(body), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	value := map[string]any{"read": number, "lines": lines}
	if strings.HasSuffix(relative, "_test.go") {
		if symbols, parseErr := coverage.GoTestSymbols(relative, body); parseErr == nil {
			choices := make([]map[string]any, 0, len(symbols))
			for index, symbol := range symbols {
				choices = append(choices, map[string]any{"symbol": index + 1, "name": symbol})
			}
			value["testSymbols"] = choices
		}
	}
	return value, err
}

func (t *nativeWorkspaceTools) fact(number int, kind string) (nativeToolFact, error) {
	if number < 1 || number > len(t.facts) || t.facts[number-1].Kind != kind {
		return nativeToolFact{}, fmt.Errorf("select an original published %s fact", kind)
	}
	return t.facts[number-1], nil
}

func (t *nativeWorkspaceTools) testOwned(relative string) bool {
	if t.material == nil || t.material.Input.Handler != "engine.gate.v3" || t.material.Input.Validation == nil || t.material.Input.Validation.Gate == nil {
		return false
	}
	for _, owned := range t.material.Input.Validation.Gate.TestOwnedPaths {
		if filepath.Clean(relative) == filepath.Clean(filepath.FromSlash(owned)) {
			return true
		}
	}
	return false
}

// Workspace access excludes the engine namespace and this action's native evidence owner.
func (t *nativeWorkspaceTools) visible(relative, resolved string) bool {
	resolvedRelative, err := filepath.Rel(t.workspace, resolved)
	if err != nil || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) {
		return false
	}
	if vcs.RuntimePath(relative) || vcs.RuntimePath(resolvedRelative) || t.runtimeFiles.ContainsFile(resolved) {
		return false
	}
	evidenceRelative, err := filepath.Rel(resolveNativePath(filepath.Dir(t.evidence)), resolved)
	return err == nil && (evidenceRelative == ".." || strings.HasPrefix(evidenceRelative, ".."+string(filepath.Separator)))
}

func (t *nativeWorkspaceTools) path(relative string, followLeaf bool) (string, error) {
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(filepath.Clean(relative), ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target must belong to the issued workspace")
	}
	path := filepath.Join(t.workspace, relative)
	var resolved string
	var err error
	if followLeaf {
		resolved, err = filepath.EvalSymlinks(path)
	} else {
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(path))
		resolved, err = filepath.Join(parent, filepath.Base(path)), parentErr
	}
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(t.workspace, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target resolves outside the issued workspace")
	}
	if t.testOwned(relative) || t.testOwned(rel) {
		return "", fmt.Errorf("test-owned content is outside the issued gate duty")
	}
	if !t.visible(relative, resolveNativePath(path)) {
		return "", fmt.Errorf("runtime content is outside the issued workspace duty")
	}
	return resolved, nil
}

type nativeWorkspaceArguments struct {
	Material      int    `json:"material,omitempty"`
	Action        string `json:"action"`
	Query         string `json:"query,omitempty"`
	Listing       int    `json:"listing,omitempty"`
	Entry         int    `json:"entry,omitempty"`
	Read          int    `json:"read,omitempty"`
	SourceRead    int    `json:"sourceRead,omitempty"`
	SourceFrom    int    `json:"sourceFrom,omitempty"`
	SourceTo      int    `json:"sourceTo,omitempty"`
	From          int    `json:"from,omitempty"`
	To            int    `json:"to,omitempty"`
	Name          string `json:"name,omitempty"`
	Content       string `json:"content,omitempty"`
	Executable    *bool  `json:"executable,omitempty"`
	TargetListing int    `json:"targetListing,omitempty"`
	TargetEntry   int    `json:"targetEntry,omitempty"`
}

// Execution admission covers qualification and local mutation. Owned commands
// and their observation run after releasing the original write window.
func (t *nativeWorkspaceTools) enterExecutionWindow() error {
	if t.release != nil {
		return nil
	}
	request, err := readJSONFile(filepath.Join(t.directory, "native-request.json"))
	if err != nil {
		return err
	}
	t.release, err = admitExecution(t.directory, objectOrEmpty(request), t.services)
	return err
}

func (t *nativeWorkspaceTools) leaveExecutionWindow() {
	if t.release != nil {
		t.release()
		t.release = nil
	}
}

func (t *nativeWorkspaceTools) invoke(name string, raw json.RawMessage) (any, error) {
	if err := t.enterExecutionWindow(); err != nil {
		return nil, err
	}
	defer t.leaveExecutionWindow()
	if err := t.refreshFacts(); err != nil {
		return nil, err
	}
	var checks []any
	var err error
	for index, fact := range t.facts {
		if fact.Kind == "edit" {
			checks, err = t.completeEditChecks(index+1, "")
			if err != nil {
				return nil, err
			}
		}
	}
	if err := t.enterExecutionWindow(); err != nil {
		return nil, err
	}
	var value any
	switch name {
	case nativeWorkspaceTool:
		value, err = t.workspaceOperation(raw)
	case nativeSemanticTool:
		value, err = t.submitSemantics(raw)
	default:
		return nil, fmt.Errorf("unknown native tool")
	}
	if err == nil && len(checks) > 0 {
		result := value.(map[string]any)
		if _, current := result["checks"]; !current {
			result["checks"] = checks
		}
	}
	return value, err
}

func (t *nativeWorkspaceTools) workspaceOperation(raw json.RawMessage) (any, error) {
	var a nativeWorkspaceArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&a); err != nil {
		return nil, err
	}
	if a.Executable != nil && a.Action != "create" && a.Action != "executable" {
		return nil, fmt.Errorf("executable applies to create or an original selected file")
	}
	relative := "."
	if a.Listing != 0 {
		fact, err := t.fact(a.Listing, "listing")
		if err != nil {
			return nil, err
		}
		if a.Entry < 1 || a.Entry > len(fact.Paths) {
			return nil, fmt.Errorf("select an entry from the original listing")
		}
		relative = fact.Paths[a.Entry-1]
	}
	switch a.Action {
	case "requirement":
		if t.material == nil {
			return nil, fmt.Errorf("action has no confirmed requirement material")
		}
		document := t.material.Input.RequirementDocument
		kind := "requirement"
		var paths []string
		if a.Material != 0 {
			if a.Material < 1 || a.Material > len(t.material.Input.Materials) {
				return nil, fmt.Errorf("select a confirmed material number")
			}
			material := t.material.Input.Materials[a.Material-1]
			document.Path, document.Content, document.ContentDigest = material.Path, material.Content, material.Revision
			if document.Path != t.material.Input.RequirementDocument.Path {
				kind, paths = "material", []string{document.Path}
			}
		}
		lines := strings.SplitAfter(document.Content, "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		from, to := a.From, a.To
		if from == 0 {
			from = 1
		}
		if to == 0 {
			to = len(lines)
		}
		if from < 1 || to < from || to > len(lines) {
			return nil, fmt.Errorf("requirement line range is outside the bound original document")
		}
		number, err := t.publish(nativeToolFact{Kind: kind, Paths: paths, Content: []byte(document.Content)})
		return map[string]any{"read": number, "path": document.Path, "contentDigest": document.ContentDigest, "from": from, "to": to, "lines": lines[from-1 : to]}, err
	case "comparison":
		if t.material == nil || t.material.Input.Validation == nil || t.material.Input.Validation.Workspace == nil {
			return nil, fmt.Errorf("action has no issued native comparison")
		}
		input := t.material.Input
		validation := input.Validation
		base, head := "", ""
		if validation.Gate != nil {
			base, head = validation.Gate.BaseSnapshot, validation.Gate.Candidate.Identity
		} else if validation.ReviewArtifact != nil {
			base, head = validation.ReviewBase, validation.ReviewArtifact.Identity
		}
		if base == "" || head == "" {
			return nil, fmt.Errorf("action has no issued native comparison")
		}
		resource := validation.Workspace
		native := vcs.NewNative(resource.Path, input.VCS, nil).At(resource.Path, resource.Handle)
		compare := func(base, head string) ([]any, error) {
			rows, err := native.CompareSnapshots(base, head)
			if err != nil {
				return nil, err
			}
			readable := make([]any, 0, len(rows))
			for _, row := range rows {
				if t.testOwned(row.Path) {
					readable = append(readable, map[string]any{"path": row.Path, "kind": row.Kind, "testOwned": true})
					continue
				}
				value := map[string]any{"path": row.Path, "kind": row.Kind, "baseMode": row.BaseMode, "mode": row.Mode, "before": row.BaseContent, "after": row.Content}
				for _, side := range []struct {
					key, mode string
					body      []byte
				}{{"beforeRead", row.BaseMode, row.BaseContent}, {"afterRead", row.Mode, row.Content}} {
					if side.mode == "" || side.mode == "000000" {
						continue
					}
					read, err := t.publishRead(row.Path, side.body)
					if err != nil {
						return nil, err
					}
					value[side.key] = read["read"]
				}
				if utf8.Valid(row.BaseContent) {
					value["before"] = string(row.BaseContent)
				}
				if utf8.Valid(row.Content) {
					value["after"] = string(row.Content)
				}
				for _, side := range []struct{ key, raw string }{{"baseMetadata", row.BaseMetadata}, {"metadata", row.Metadata}} {
					if side.raw == "" {
						continue
					}
					var metadata map[string]any
					if err := json.Unmarshal([]byte(side.raw), &metadata); err != nil {
						return nil, err
					}
					delete(metadata, "content")
					if len(metadata) > 0 {
						value[side.key] = metadata
					}
				}
				readable = append(readable, value)
			}
			return readable, nil
		}
		changes, err := compare(base, head)
		if err != nil {
			return nil, err
		}
		result := map[string]any{"changes": changes}
		if validation.Gate != nil && len(validation.Gate.Slices) > 0 {
			children := make([]any, 0, len(validation.Gate.Slices))
			for _, span := range validation.Gate.Slices {
				rows, err := compare(span.BaseSnapshot, span.Snapshot)
				if err != nil {
					return nil, err
				}
				children = append(children, map[string]any{"slice": span.SliceID, "changes": rows})
			}
			result["slices"] = children
		}
		return result, nil
	case "search":
		if a.Query == "" {
			return nil, fmt.Errorf("search requires nonempty literal query text")
		}
		path, err := t.path(relative, true)
		if err != nil {
			return nil, err
		}
		type matchFile struct {
			path  string
			body  []byte
			lines []map[string]any
		}
		var files []matchFile
		err = filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(t.workspace, path)
			if err != nil {
				return err
			}
			visible, err := t.path(rel, true)
			if err != nil {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			info, err := os.Stat(visible)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			body, err := os.ReadFile(visible)
			if err != nil {
				return err
			}
			if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
				return nil
			}
			var lines []map[string]any
			for i, line := range strings.SplitAfter(string(body), "\n") {
				if strings.Contains(line, a.Query) {
					lines = append(lines, map[string]any{"line": i + 1, "text": line})
				}
			}
			if len(lines) > 0 {
				files = append(files, matchFile{rel, body, lines})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		paths := make([]string, len(files))
		for i, file := range files {
			paths[i] = file.path
		}
		listing, err := t.publish(nativeToolFact{Kind: "listing", Paths: paths})
		if err != nil {
			return nil, err
		}
		matches := []map[string]any{}
		for i, file := range files {
			read, err := t.publishRead(file.path, file.body)
			if err != nil {
				return nil, err
			}
			for _, line := range file.lines {
				line["listing"], line["entry"], line["read"], line["path"] = listing, i+1, read["read"], file.path
				matches = append(matches, line)
			}
		}
		return map[string]any{"listing": listing, "matches": matches}, nil
	case "list":
		path, err := t.path(relative, true)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		paths := make([]string, 0, len(entries))
		choices := make([]any, 0, len(entries))
		for _, entry := range entries {
			name := filepath.Join(relative, entry.Name())
			if !t.visible(name, resolveNativePath(filepath.Join(t.workspace, name))) {
				continue
			}
			paths = append(paths, name)
			choice := map[string]any{"entry": len(paths), "name": entry.Name(), "directory": entry.IsDir()}
			if t.testOwned(name) {
				choice["testOwned"] = true
			}
			choices = append(choices, choice)
		}
		number, err := t.publish(nativeToolFact{Kind: "listing", Paths: paths})
		return map[string]any{"listing": number, "entries": choices}, err
	case "read":
		if a.Listing == 0 {
			return nil, fmt.Errorf("read selects a published directory entry")
		}
		path, err := t.path(relative, true)
		if err != nil {
			return nil, err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return t.publishRead(relative, body)
	case "replace", "copy-range":
		if a.Action == "copy-range" {
			if a.Content != "" {
				return nil, fmt.Errorf("copy-range takes original read bytes, not supplied content")
			}
			source, err := t.fact(a.SourceRead, "read")
			if err != nil {
				return nil, err
			}
			path, err := t.path(source.Paths[0], true)
			if err != nil {
				return nil, err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(body, source.Content) {
				return nil, fmt.Errorf("source changed after the selected read")
			}
			lines := strings.SplitAfter(string(source.Content), "\n")
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			if a.SourceFrom < 1 || a.SourceTo < a.SourceFrom || a.SourceTo > len(lines) {
				return nil, fmt.Errorf("select source lines from the original read")
			}
			a.Content = strings.Join(lines[a.SourceFrom-1:a.SourceTo], "")
		}
		if t.readOnly {
			return nil, fmt.Errorf("issued action is read-only")
		}
		fact, err := t.fact(a.Read, "read")
		if err != nil {
			return nil, err
		}
		path, err := t.path(fact.Paths[0], true)
		if err != nil {
			return nil, err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(body, fact.Content) {
			return nil, fmt.Errorf("file changed after the selected read; read its current contents")
		}
		lines := strings.SplitAfter(string(fact.Content), "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		empty := len(lines) == 0 && a.From == 1 && a.To == 0
		if !empty && (a.From < 1 || a.To < a.From-1 || a.From > len(lines)+1 || a.To > len(lines)) {
			return nil, fmt.Errorf("select a line range from the original read")
		}
		result := a.Content
		if !empty {
			result = strings.Join(lines[:a.From-1], "") + a.Content + strings.Join(lines[a.To:], "")
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		return t.edit(raw, func() (any, error) {
			return map[string]any{"written": true}, os.WriteFile(path, []byte(result), info.Mode().Perm())
		})
	case "executable":
		if a.Executable == nil || (a.Listing == 0 && a.Read == 0) || a.Content != "" || a.Name != "" {
			return nil, fmt.Errorf("executable selects an original file and a boolean")
		}
		if a.Read != 0 {
			fact, err := t.fact(a.Read, "read")
			if err != nil {
				return nil, err
			}
			relative = fact.Paths[0]
		}
		path, err := t.path(relative, true)
		if err != nil {
			return nil, err
		}
		if t.readOnly {
			return nil, fmt.Errorf("issued action is read-only")
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("executable selects a regular file")
		}
		mode := info.Mode() &^ 0111
		if *a.Executable {
			mode |= 0111
		}
		return t.edit(raw, func() (any, error) { return map[string]any{"executable": *a.Executable}, os.Chmod(path, mode) })
	case "create", "mkdir":
		if t.readOnly {
			return nil, fmt.Errorf("issued action is read-only")
		}
		parent, err := t.path(relative, true)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(parent)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || a.Name == "" || filepath.Base(a.Name) != a.Name || a.Name == "." || a.Name == ".." {
			return nil, fmt.Errorf("new name must be a filename in the selected directory")
		}
		path, err := t.path(filepath.Join(relative, a.Name), false)
		if err != nil {
			return nil, err
		}
		if a.Action == "mkdir" {
			return t.edit(raw, func() (any, error) {
				return map[string]any{"created": true}, os.Mkdir(path, 0755)
			})
		}
		mode := os.FileMode(0644)
		if a.Executable != nil && *a.Executable {
			mode |= 0111
		}
		return t.edit(raw, func() (any, error) {
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return nil, err
			}
			_, err = file.WriteString(a.Content)
			closeErr := file.Close()
			if err != nil {
				return nil, err
			}
			return map[string]any{"written": true}, closeErr
		})
	case "copy", "move":
		if t.readOnly {
			return nil, fmt.Errorf("issued action is read-only")
		}
		if a.Listing == 0 {
			return nil, fmt.Errorf("file transfer selects an original published entry")
		}
		if a.Name == "" {
			a.Name = filepath.Base(relative)
		}
		if a.Content != "" || filepath.Base(a.Name) != a.Name || a.Name == "." || a.Name == ".." {
			return nil, fmt.Errorf("file transfer supplies only a new filename and original selections")
		}
		targetRelative := "."
		if a.TargetListing != 0 {
			fact, err := t.fact(a.TargetListing, "listing")
			if err != nil {
				return nil, err
			}
			if a.TargetEntry < 1 || a.TargetEntry > len(fact.Paths) {
				return nil, fmt.Errorf("target selects an entry from the original listing")
			}
			targetRelative = fact.Paths[a.TargetEntry-1]
		} else if a.TargetEntry != 0 {
			return nil, fmt.Errorf("target entry requires its original listing")
		}
		targetDirectory, err := t.path(targetRelative, true)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(targetDirectory)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("target must be a workspace directory")
		}
		target, err := t.path(filepath.Join(targetRelative, a.Name), false)
		if err != nil {
			return nil, err
		}
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("target already exists")
		}
		source, err := t.path(relative, a.Action == "copy")
		if err != nil {
			return nil, err
		}
		info, err = os.Lstat(source)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() && !(a.Action == "move" && info.Mode()&os.ModeSymlink != 0) {
			return nil, fmt.Errorf("file transfer selects a file, not a directory")
		}
		if a.Action == "move" {
			return t.edit(raw, func() (any, error) {
				return map[string]any{"moved": true}, os.Rename(source, target)
			})
		}
		body, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		return t.edit(raw, func() (any, error) {
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
			if err != nil {
				return nil, err
			}
			_, err = file.Write(body)
			closeErr := file.Close()
			if err != nil {
				return nil, err
			}
			return map[string]any{"copied": true}, closeErr
		})
	case "remove":
		if t.readOnly || a.Listing == 0 {
			return nil, fmt.Errorf("removal requires a writable action and published entry")
		}
		path, err := t.path(relative, false)
		if err != nil {
			return nil, err
		}
		return t.edit(raw, func() (any, error) {
			return map[string]any{"removed": true}, os.Remove(path)
		})
	default:
		return nil, fmt.Errorf("unknown workspace action")
	}
}

// Semantic submissions are original tool evidence, scoped to this invocation.
func (t *nativeWorkspaceTools) submitSemantics(raw json.RawMessage) (any, error) {
	value, err := decodeUniqueKeys(raw)
	if err != nil {
		return nil, err
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("semantic submission requires an object")
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if t.material != nil {
		if err := worker.ValidateSemanticBody(t.material.Input, body, workerLocationFacts(t.facts)); err != nil {
			return nil, err
		}
	}
	for i, fact := range t.facts {
		if fact.Kind == "semantics" && fact.Job == t.directory && fact.RequestDigest == t.requestDigest {
			if !bytes.Equal(fact.Content, body) {
				return nil, fmt.Errorf("current invocation already submitted different semantics")
			}
			return map[string]any{"submitted": true, "fact": i + 1}, nil
		}
	}
	number, err := t.publish(nativeToolFact{Kind: "semantics", Content: body})
	return map[string]any{"submitted": true, "fact": number}, err
}

func (t *nativeWorkspaceTools) schemas() []any {
	schema := t.semanticSchema
	if t.material != nil && t.material.ResultSchema != nil {
		schema = t.material.ResultSchema
	}
	if len(schema) == 0 {
		schema = map[string]any{"type": "object"}
	}
	if t.provider == "zcode" {
		schema = zcodeSemanticToolSchema(schema)
	}
	workspace := nativeWorkspaceToolSchema(t.readOnly, t.material)
	if t.provider == "zcode" {
		workspace["inputSchema"] = zcodeSemanticToolSchema(objectOrEmpty(workspace["inputSchema"]))
	}
	return []any{workspace, map[string]any{"name": nativeSemanticTool, "description": "Submit this invocation's semantic result once. The program binds the role, identities and receipt; final messages only finish or explain.", "inputSchema": schema}}
}

func nativeWorkspaceToolSchema(readOnly bool, materials ...*worker.Material) map[string]any {
	var material *worker.Material
	if len(materials) > 0 {
		material = materials[0]
	}
	positive := map[string]any{"type": "integer", "minimum": 1}
	fields := map[string]any{"listing": positive, "entry": positive, "read": positive, "sourceRead": positive, "sourceFrom": positive, "sourceTo": positive, "from": positive, "to": map[string]any{"type": "integer", "minimum": 0}, "targetListing": positive, "targetEntry": positive, "material": positive, "query": map[string]any{"type": "string", "minLength": 1}, "name": map[string]any{"type": "string", "minLength": 1}, "content": map[string]any{"type": "string"}, "executable": map[string]any{"type": "boolean"}}
	alternatives := []any{}
	add := func(action string, names, required []string) {
		properties := map[string]any{"action": map[string]any{"type": "string", "const": action}}
		for _, name := range names {
			properties[name] = fields[name]
		}
		demands := []string{"action"}
		demands = append(demands, required...)
		alternatives = append(alternatives, map[string]any{"type": "object", "properties": properties, "required": demands, "additionalProperties": false})
	}
	add("list", nil, nil)
	add("list", []string{"listing", "entry"}, []string{"listing", "entry"})
	add("read", []string{"listing", "entry"}, []string{"listing", "entry"})
	add("search", []string{"query"}, []string{"query"})
	add("search", []string{"query", "listing", "entry"}, []string{"query", "listing", "entry"})
	description := "Choose one action and only its declared fields. Search uses literal query text. Existing files select original listing/entry numbers. Published reads and numbers keep their original bytes and identities across continuation and resume."
	if material != nil && material.Input.RequirementDocument.Content != "" {
		add("requirement", []string{"material", "from", "to"}, nil)
		description += " Requirement reads the frozen confirmed text: omit material for the formal requirement or select its issued material number, optionally from/to lines. It publishes semantic source evidence, without physical file authority."
	}
	if material != nil && material.Input.Validation != nil && (material.Input.Validation.Gate != nil || material.Input.Validation.ReviewArtifact != nil) {
		add("comparison", nil, nil)
		description += " Comparison reads issued native versions."
	}
	if !readOnly {
		add("replace", []string{"read", "from", "to", "content"}, []string{"read", "from", "to", "content"})
		add("copy-range", []string{"read", "from", "to", "sourceRead", "sourceFrom", "sourceTo"}, []string{"read", "from", "to", "sourceRead", "sourceFrom", "sourceTo"})
		add("create", []string{"name", "content", "executable"}, []string{"name", "content"})
		add("create", []string{"listing", "entry", "name", "content", "executable"}, []string{"listing", "entry", "name", "content"})
		add("mkdir", []string{"name"}, []string{"name"})
		add("mkdir", []string{"listing", "entry", "name"}, []string{"listing", "entry", "name"})
		add("remove", []string{"listing", "entry"}, []string{"listing", "entry"})
		for _, action := range []string{"copy", "move"} {
			add(action, []string{"listing", "entry", "name"}, []string{"listing", "entry"})
			add(action, []string{"listing", "entry", "name", "targetListing", "targetEntry"}, []string{"listing", "entry", "targetListing", "targetEntry"})
		}
		add("executable", []string{"read", "executable"}, []string{"read", "executable"})
		add("executable", []string{"listing", "entry", "executable"}, []string{"listing", "entry", "executable"})
		description += " Replace selects an original physical read; from/to selects its line range and to=from-1 inserts bytes. Copy-range selects original physical source lines. Create may set executable. Copy/move may retain the original name and target root. Executable changes only execution bits of the selected physical file."
	}
	description += " Physical reads publish original Go test symbol numbers."
	return map[string]any{"name": nativeWorkspaceTool, "description": description, "inputSchema": map[string]any{"type": "object", "oneOf": alternatives}}
}

// NativeTools serves only MCP's handshake, discovery and tool-call methods.
// The signed native request supplies physical execution parameters.
func NativeTools(directory string, in io.Reader, out, errout io.Writer, services ...ExecutionServices) int {
	tools, err := newNativeWorkspaceTools(directory, services...)
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	return serveNativeTools(in, out, errout, tools.schemas(), func(name string, arguments json.RawMessage) (any, error) {
		if name != nativeWorkspaceTool && name != nativeSemanticTool {
			return nil, fmt.Errorf("unknown native tool")
		}
		return tools.invoke(name, arguments)
	}, tools.recordCall)
}

func serveNativeTools(in io.Reader, out, errout io.Writer, schemas []any, call func(string, json.RawMessage) (any, error), record func(json.RawMessage, any) error) int {
	return ServeTools(in, out, errout, schemas, func(name string, arguments json.RawMessage, _ func(any) error, _ json.RawMessage) (any, error) {
		return call(name, arguments)
	}, record)
}

// ServeTools keeps confirmation on the requesting host's MCP connection. The
// callback may execute only after that connection accepts the displayed
// object. Ordinary requests queue behind the active call and run in arrival
// order; only a pending host confirmation refuses new calls, which also
// flushes the queue.
func ServeTools(in io.Reader, out, errout io.Writer, schemas []any, call func(string, json.RawMessage, func(any) error, json.RawMessage) (any, error), record func(json.RawMessage, any) error) int {
	return serveTools(context.Background(), in, out, errout, schemas,
		func(_ context.Context, name string, arguments json.RawMessage, confirm func(any) error, metadata json.RawMessage) (any, error) {
			return call(name, arguments, confirm, metadata)
		}, record, true, nil)
}

// mcpFrame is one decoded inbound message on the transport.
type mcpFrame struct {
	Raw       json.RawMessage
	ID        json.RawMessage
	Method    string
	RequestID json.RawMessage
	Result    json.RawMessage
	Error     json.RawMessage
	Params    json.RawMessage
}

// confirmWaiter is one outstanding elicitation request routed by its form ID.
type confirmWaiter struct {
	reply chan mcpFrame
}

// toolCall tracks the single in-flight tools/call so cancellation targets it
// and its result can be joined exactly once.
type toolCall struct {
	id          json.RawMessage
	cancel      context.CancelCauseFunc
	confirmBase int32
}

type toolCallResult struct {
	request json.RawMessage
	value   any
	err     error
}

// ServePinger is the serve's server-initiated ping channel: a supervisor
// probes the host through it while the serve blocks. Ping fails when no
// serve holds the channel, the transport is closed, or the context expires.
type ServePinger struct {
	impl atomic.Pointer[func(context.Context) error]
}

func (p *ServePinger) Ping(ctx context.Context) error {
	if impl := p.impl.Load(); impl != nil {
		return (*impl)(ctx)
	}
	return fmt.Errorf("the serve transport is not running")
}

func (p *ServePinger) bind(impl func(context.Context) error) { p.impl.Store(&impl) }
func (p *ServePinger) unbind()                               { p.impl.Store(nil) }

// ServeToolsWithContext serves the workflow-control MCP surface with caller
// context and a dedicated reader: while one tool callback runs, the
// dispatcher still answers pings, routes notifications/cancelled by the
// exact original request ID, and replies busy to other tool requests instead
// of queueing workflow control calls behind a long configure. A request
// cancellation cancels only that callback's context; closing stdin (EOF)
// cancels the active call so the owner-facing cleanup runs. A non-nil pinger
// stays bound for the serve's lifetime so the caller can probe the host.
func ServeToolsWithContext(ctx context.Context, in io.Reader, out, errout io.Writer, schemas []any, call func(context.Context, string, json.RawMessage, func(any) error, json.RawMessage) (any, error), record func(json.RawMessage, any) error, pinger *ServePinger) int {
	return serveTools(ctx, in, out, errout, schemas, call, record, false, pinger)
}

func serveTools(ctx context.Context, in io.Reader, out, errout io.Writer, schemas []any, call func(context.Context, string, json.RawMessage, func(any) error, json.RawMessage) (any, error), record func(json.RawMessage, any) error, queueOrdinary bool, pinger *ServePinger) int {
	frames := make(chan mcpFrame, 64)
	decodeDone := make(chan error, 1)
	go func() {
		defer close(frames)
		decoder := json.NewDecoder(in)
		for {
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				if err != io.EOF {
					decodeDone <- err
				}
				return
			}
			var frame mcpFrame
			var decoded struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(raw, &decoded); err != nil {
				decodeDone <- err
				return
			}
			frame.ID, frame.Method, frame.Params, frame.Result, frame.Error = decoded.ID, decoded.Method, decoded.Params, decoded.Result, decoded.Error
			if frame.Method == "notifications/cancelled" {
				var cancelled struct {
					RequestID json.RawMessage `json:"requestId"`
				}
				_ = json.Unmarshal(frame.Params, &cancelled)
				frame.RequestID = cancelled.RequestID
			}
			frame.Raw = raw
			select {
			case frames <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()
	var writeMu sync.Mutex
	encoder := json.NewEncoder(out)
	encode := func(value any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return encoder.Encode(value)
	}
	encodeBusy := func(id json.RawMessage) {
		if err := encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32000, "message": "a native confirmation is pending; retry this request after it finishes"}}); err != nil {
			fmt.Fprintln(errout, err)
		}
	}
	formSupported := false
	var confirmationID atomic.Int32
	var confirmMu sync.Mutex
	pendingConfirm := map[string]*confirmWaiter{}
	// Replies arriving before their waiter registers are held here so an
	// already-buffered answer is consumed instead of being dropped. The map
	// is scoped to the active call: stale or never-issued form replies are
	// discarded, not stockpiled for a later prompt.
	earlyReplies := map[string]mcpFrame{}
	// A response owning no known request ID is foreign; while a call is in
	// flight it poisons that call's next confirmation. The flag resets when
	// the call ends.
	foreignReply := false
	transportEOF := false
	var active *toolCall
	configureCalls := map[string]*toolCall{}
	// Ordinary requests queued behind the active call in legacy mode.
	var queued []mcpFrame
	// Server-initiated ping replies route to their own request IDs.
	pendingOutbound := map[string]chan mcpFrame{}
	var outboundID atomic.Int64
	pingRequest := func(reqCtx context.Context) error {
		id := "formal-gates-ping-" + strconv.FormatInt(outboundID.Add(1), 10)
		reply := make(chan mcpFrame, 1)
		confirmMu.Lock()
		if transportEOF {
			confirmMu.Unlock()
			return io.EOF
		}
		pendingOutbound[id] = reply
		confirmMu.Unlock()
		defer func() {
			confirmMu.Lock()
			delete(pendingOutbound, id)
			confirmMu.Unlock()
		}()
		if err := encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": "ping"}); err != nil {
			return err
		}
		select {
		case frame := <-reply:
			if len(frame.Error) != 0 {
				return fmt.Errorf("host ping failed: %s", frame.Error)
			}
			return nil
		case <-reqCtx.Done():
			return reqCtx.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if pinger != nil {
		pinger.bind(pingRequest)
		defer pinger.unbind()
	}
	// confirmNumber reads the sequence suffix of our own form IDs; the
	// boolean answers whether the ID was issued this serve run at all.
	confirmNumber := func(raw json.RawMessage) (int64, bool) {
		var id string
		if json.Unmarshal(raw, &id) != nil {
			return 0, false
		}
		n, err := strconv.ParseInt(strings.TrimPrefix(id, "formal-gates-confirmation-"), 10, 64)
		return n, err == nil
	}
	// confirmAnswer classifies one routed elicitation reply.
	confirmAnswer := func(response mcpFrame) error {
		if len(response.Error) != 0 {
			var message string
			if json.Unmarshal(response.Error, &message) == nil {
				return errors.New(message)
			}
			return fmt.Errorf("native confirmation failed: %s", response.Error)
		}
		var result struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(response.Result, &result); err != nil {
			return fmt.Errorf("native confirmation has no supported host action")
		}
		switch result.Action {
		case "accept":
			return nil
		case "decline", "cancel":
			return fmt.Errorf("native confirmation %s; operation was not executed", result.Action)
		default:
			return fmt.Errorf("native confirmation has no supported host action")
		}
	}
	makeConfirm := func(callCtx context.Context) func(any) error {
		return func(value any) error {
			if !formSupported {
				return fmt.Errorf("NATIVE_CONFIRMATION_UNAVAILABLE: the current host does not support MCP form elicitation")
			}
			message, err := confirmationMessage(value)
			if err != nil {
				return err
			}
			id := "formal-gates-confirmation-" + strconv.Itoa(int(confirmationID.Add(1)))
			waiter := &confirmWaiter{reply: make(chan mcpFrame, 1)}
			confirmMu.Lock()
			pendingConfirm[id] = waiter
			// The host is deciding; anything queued behind this call can
			// never run before it finishes, so queued callers get the busy
			// refusal up front.
			flush := queued
			queued = nil
			confirmMu.Unlock()
			for _, queuedFrame := range flush {
				encodeBusy(queuedFrame.ID)
			}
			defer func() {
				confirmMu.Lock()
				delete(pendingConfirm, id)
				confirmMu.Unlock()
			}()
			if err := encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": "elicitation/create", "params": map[string]any{
				"mode": "form", "message": message,
				"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
			}}); err != nil {
				return err
			}
			confirmMu.Lock()
			switch {
			case callCtx.Err() != nil && !errors.Is(context.Cause(callCtx), io.EOF):
				confirmMu.Unlock()
				return fmt.Errorf("native confirmation cancelled; operation was not executed")
			}
			if early, ok := earlyReplies[id]; ok {
				delete(earlyReplies, id)
				waiter.reply <- early
				delete(pendingConfirm, id)
			} else if foreignReply {
				confirmMu.Unlock()
				return fmt.Errorf("native confirmation response belongs to another request")
			} else if transportEOF {
				confirmMu.Unlock()
				return fmt.Errorf("native confirmation was not received: transport closed")
			}
			confirmMu.Unlock()
			select {
			case response := <-waiter.reply:
				return confirmAnswer(response)
			case <-callCtx.Done():
				// EOF ends the host, but a reply that was already routed to
				// this exact form still counts; an explicit cancellation
				// never honours a late approval.
				if errors.Is(context.Cause(callCtx), io.EOF) {
					select {
					case response := <-waiter.reply:
						return confirmAnswer(response)
					default:
					}
				}
				return fmt.Errorf("native confirmation cancelled; operation was not executed")
			case <-ctx.Done():
				return fmt.Errorf("native confirmation cancelled; operation was not executed")
			}
		}
	}
	callDone := make(chan toolCallResult, 1)
	finishCall := func(outcome toolCallResult) int {
		var result any
		var responseErr error
		text := ""
		if outcome.err != nil {
			text = outcome.err.Error()
		} else {
			body, marshalErr := json.Marshal(outcome.value)
			if marshalErr != nil {
				responseErr = marshalErr
			}
			text = string(body)
		}
		result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": outcome.err != nil || responseErr != nil}
		if record != nil {
			if err := record(outcome.request, result); err != nil {
				fmt.Fprintln(errout, err)
				return 1
			}
		}
		response := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage("null"), "result": result}
		var request struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.Unmarshal(outcome.request, &request)
		response["id"] = request.ID
		if responseErr != nil {
			delete(response, "result")
			response["error"] = map[string]any{"code": -32601, "message": responseErr.Error()}
		}
		if err := encode(response); err != nil {
			fmt.Fprintln(errout, err)
			return 1
		}
		if _, waiting := configureCalls[string(request.ID)]; waiting {
			delete(configureCalls, string(request.ID))
			return -1
		}
		confirmMu.Lock()
		active = nil
		foreignReply = false
		earlyReplies = map[string]mcpFrame{}
		confirmMu.Unlock()
		return -1
	}
	// dispatch answers one request, starting a tool call when free. Callers
	// have already handled ping, cancellations, and ID-less notifications.
	dispatch := func(frame mcpFrame) {
		var result any
		var callErr error
		switch frame.Method {
		case "initialize":
			var params struct {
				ProtocolVersion string                     `json:"protocolVersion"`
				Capabilities    map[string]json.RawMessage `json:"capabilities"`
			}
			callErr = json.Unmarshal(frame.Params, &params)
			var elicitation map[string]json.RawMessage
			formSupported = false
			if raw, ok := params.Capabilities["elicitation"]; ok && json.Unmarshal(raw, &elicitation) == nil && elicitation != nil {
				_, formSupported = elicitation["form"]
				formSupported = formSupported || len(elicitation) == 0
			}
			result = map[string]any{"protocolVersion": params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "formal-gates", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": schemas}
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
				Metadata  json.RawMessage `json:"_meta"`
			}
			callErr = json.Unmarshal(frame.Params, &params)
			if callErr == nil {
				callCtx, cancel := context.WithCancelCause(ctx)
				confirmMu.Lock()
				invocation := &toolCall{id: frame.ID, cancel: cancel, confirmBase: confirmationID.Load()}
				panelWait := !queueOrdinary && params.Name == "configure"
				if panelWait {
					configureCalls[string(frame.ID)] = invocation
				} else {
					active = invocation
					foreignReply = false
					earlyReplies = map[string]mcpFrame{}
				}
				confirmMu.Unlock()
				confirmFunc := makeConfirm(callCtx)
				if panelWait {
					confirmFunc = func(any) error { return fmt.Errorf("panel configuration does not request native confirmation") }
				}
				request := frame.Raw
				go func() {
					value, err := call(callCtx, params.Name, params.Arguments, confirmFunc, params.Metadata)
					cancel(context.Canceled)
					callDone <- toolCallResult{request: request, value: value, err: err}
				}()
				return
			}
		default:
			callErr = fmt.Errorf("unsupported MCP method %s", frame.Method)
		}
		response := map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": result}
		if callErr != nil {
			delete(response, "result")
			response["error"] = map[string]any{"code": -32601, "message": callErr.Error()}
		}
		if err := encode(response); err != nil {
			fmt.Fprintln(errout, err)
		}
	}
	eof := false
	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				frames = nil
				eof = true
				// A dead transport can never deliver a pending confirmation
				// reply; buffered answers were already routed first.
				confirmMu.Lock()
				transportEOF = true
				for id, waiter := range pendingConfirm {
					delete(pendingConfirm, id)
					waiter.reply <- mcpFrame{Error: json.RawMessage(strconv.Quote("native confirmation was not received: transport closed"))}
				}
				for id, waiter := range pendingOutbound {
					delete(pendingOutbound, id)
					waiter <- mcpFrame{Error: json.RawMessage(strconv.Quote("host ping was not received: transport closed"))}
				}
				confirmMu.Unlock()
				// The owner pipe is gone: cancel the active call so owner
				// cleanup (controller close, owned service EOF) runs now.
				for _, waiting := range configureCalls {
					waiting.cancel(io.EOF)
				}
				if active != nil {
					active.cancel(io.EOF)
				}
				if active != nil || len(configureCalls) != 0 {
					continue
				}
				select {
				case err := <-decodeDone:
					fmt.Fprintln(errout, err)
					return 1
				default:
					return 0
				}
			}
			// Responses route to the elicitation that owns the ID. Our own
			// form IDs can arrive before their waiter registers (prebuffered
			// input), so they are held until the waiter asks; any other ID
			// while a confirmation is pending is a foreign reply.
			if frame.Method == "" && len(frame.ID) != 0 {
				var id string
				owned := json.Unmarshal(frame.ID, &id) == nil && strings.HasPrefix(id, "formal-gates-confirmation-")
				if owned {
					n, _ := confirmNumber(frame.ID)
					confirmMu.Lock()
					waiter, ok := pendingConfirm[id]
					switch {
					case ok:
						delete(pendingConfirm, id)
						waiter.reply <- frame
					case active != nil && int64(active.confirmBase) < n && n <= int64(confirmationID.Load())+1:
						// Issued by the current call but not yet awaited.
						earlyReplies[id] = frame
					default:
						// Stale or impossible form reply: ignored for this
						// call and never stockpiled for a later one.
					}
					confirmMu.Unlock()
					continue
				}
				// Our own ping reply resolves its waiter; a stale one is still
				// ours, so it never counts as a foreign response.
				if strings.HasPrefix(id, "formal-gates-ping-") {
					confirmMu.Lock()
					if waiter, waiting := pendingOutbound[id]; waiting {
						delete(pendingOutbound, id)
						waiter <- frame
					}
					confirmMu.Unlock()
					continue
				}
				confirmMu.Lock()
				hadPending := len(pendingConfirm) > 0
				for waitingID, waiter := range pendingConfirm {
					delete(pendingConfirm, waitingID)
					waiter.reply <- mcpFrame{Error: json.RawMessage(strconv.Quote("native confirmation response belongs to another request"))}
				}
				if !hadPending && active != nil {
					foreignReply = true
				}
				confirmMu.Unlock()
				continue
			}
			if frame.Method == "notifications/cancelled" {
				var cancelled string
				if waiting := configureCalls[string(frame.RequestID)]; waiting != nil {
					waiting.cancel(context.Canceled)
				}
				callCancelled := active != nil && bytes.Equal(frame.RequestID, active.id)
				if callCancelled {
					active.cancel(context.Canceled)
				}
				confirmMu.Lock()
				for id, waiter := range pendingConfirm {
					formCancelled := json.Unmarshal(frame.RequestID, &cancelled) == nil && cancelled == id
					if formCancelled || callCancelled {
						delete(pendingConfirm, id)
						waiter.reply <- mcpFrame{Error: json.RawMessage(strconv.Quote("native confirmation cancelled; operation was not executed"))}
					}
				}
				confirmMu.Unlock()
				continue
			}
			if len(frame.ID) == 0 {
				continue
			}
			if frame.Method == "ping" {
				if err := encode(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": map[string]any{}}); err != nil {
					fmt.Fprintln(errout, err)
					return 1
				}
				continue
			}
			if active != nil {
				confirmMu.Lock()
				if queueOrdinary && len(pendingConfirm) == 0 {
					queued = append(queued, frame)
					confirmMu.Unlock()
					continue
				}
				confirmMu.Unlock()
				if err := encode(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "error": map[string]any{"code": -32000, "message": "a native confirmation is pending; retry this request after it finishes"}}); err != nil {
					fmt.Fprintln(errout, err)
					return 1
				}
				continue
			}
			dispatch(frame)
		case outcome := <-callDone:
			if code := finishCall(outcome); code >= 0 {
				return code
			}
			for {
				confirmMu.Lock()
				if len(queued) == 0 {
					confirmMu.Unlock()
					break
				}
				next := queued[0]
				queued = queued[1:]
				confirmMu.Unlock()
				dispatch(next)
				if active != nil {
					break
				}
			}
			if eof && active == nil && len(configureCalls) == 0 {
				select {
				case err := <-decodeDone:
					fmt.Fprintln(errout, err)
					return 1
				default:
					return 0
				}
			}
		case <-ctx.Done():
			for _, waiting := range configureCalls {
				waiting.cancel(context.Canceled)
			}
			if active != nil {
				active.cancel(context.Canceled)
			}
			for active != nil || len(configureCalls) != 0 {
				if code := finishCall(<-callDone); code >= 0 {
					return code
				}
			}
			return 0
		}
	}
}

func nativeToolServer(directory string) (map[string]any, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": "formal-gates", "command": executable, "args": []any{"harness", "native-tools", directory}, "env": []any{}, "isolation": "session"}, nil
}

// The accepted payload must occur in a successful original native tool call.
func verifySemanticCall(directory string, body []byte) error {
	file, err := os.Open(filepath.Join(directory, nativeToolCallsFile))
	if err != nil {
		return err
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 4096), int(^uint(0)>>1))
	for scan.Scan() {
		value, err := decodeUniqueKeys(scan.Bytes())
		if err != nil {
			return err
		}
		request, result := getOf(value, "request"), getOf(value, "result")
		params := getOf(request, "params")
		name := getOf(params, "name")
		success := getOf(result, "isError") == false
		if getOf(request, "method") == "item/tool/call" {
			name, success = getOf(params, "tool"), getOf(result, "success") == true
		}
		if getOf(request, "method") == "workflow/worker" && getOf(params, "operation") == "submit" {
			name, success = nativeSemanticTool, getOf(result, "isError") == false
			ref, _, err := hostWorkerInvocation(directory)
			if err != nil {
				return err
			}
			if getOf(params, "session") != ref.Session {
				return fmt.Errorf("semantic worker caller differs from original child")
			}
		}
		if name != nativeSemanticTool || !success {
			continue
		}
		canonical, err := json.Marshal(getOf(params, "arguments"))
		if err != nil {
			return err
		}
		if bytes.Equal(canonical, body) {
			return nil
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}
	return fmt.Errorf("semantic payload has no successful original native tool call")
}
