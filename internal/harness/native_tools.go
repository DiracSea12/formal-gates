package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
)

const nativeWorkspaceTool = "workspace"
const nativeSemanticTool = "submit_semantics"
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
	Content       string   `json:"content,omitempty"`
}

type nativeWorkspaceTools struct {
	directory, workspace, action, requestDigest, evidence string
	readOnly                                              bool
	facts                                                 []nativeToolFact
	services                                              []ExecutionServices
	material                                              *worker.Material
	semanticSchema                                        map[string]any
	materials                                             string
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
	t := &nativeWorkspaceTools{directory: directory, workspace: resolveNativePath(workspace), readOnly: readOnly, requestDigest: textOf(proof["sha256"]), evidence: filepath.Join(directory, "tool-facts.jsonl")}
	if materials, _ := getOf(request, "materials").(string); materials != "" {
		if !filepath.IsAbs(materials) {
			return nil, fmt.Errorf("current materials require their original absolute directory")
		}
		t.materials = resolveNativePath(materials)
		relative, err := filepath.Rel(t.workspace, t.materials)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("current materials must belong to their issued workspace")
		}
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
	return []string{nativeToolCallsFile, nativeToolContextFile}, nil
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
	return nil
}

func (t *nativeWorkspaceTools) publish(f nativeToolFact) (int, error) {
	f.Action, f.Workspace, f.RequestDigest = t.action, t.workspace, t.requestDigest
	f.Job = t.directory
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
	number, err := t.publish(nativeToolFact{Kind: "read", Paths: []string{relative}, Content: string(body)})
	lines := strings.SplitAfter(string(body), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	value := map[string]any{"read": number, "lines": lines}
	if strings.HasSuffix(relative, "_test.go") {
		if symbols, parseErr := coverage.GoTestSymbols(relative, body); parseErr == nil {
			choices := make([]map[string]any, 0, len(symbols))
			for index, symbol := range symbols {
				choices = append(choices, map[string]any{"testSymbol": index + 1, "name": symbol})
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
	if t.material == nil || t.material.Input.Handler != "engine.gate.v2" || t.material.Input.Validation == nil || t.material.Input.Validation.Gate == nil {
		return false
	}
	for _, owned := range t.material.Input.Validation.Gate.TestOwnedPaths {
		if filepath.Clean(relative) == filepath.Clean(filepath.FromSlash(owned)) {
			return true
		}
	}
	return false
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
	if t.materials != "" {
		parent := filepath.Dir(t.materials)
		inside, _ := filepath.Rel(parent, resolved)
		current, _ := filepath.Rel(t.materials, resolved)
		if inside != "." && inside != ".." && !strings.HasPrefix(inside, ".."+string(filepath.Separator)) && (current == ".." || strings.HasPrefix(current, ".."+string(filepath.Separator))) {
			return "", fmt.Errorf("only the current role materials are available")
		}
	}
	return resolved, nil
}

func (t *nativeWorkspaceTools) writable(path string) error {
	if t.readOnly {
		return fmt.Errorf("issued action is read-only")
	}
	if t.materials != "" {
		relative, _ := filepath.Rel(filepath.Dir(t.materials), path)
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("current task materials are read-only")
		}
	}
	return nil
}

type nativeWorkspaceArguments struct {
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
	TargetListing int    `json:"targetListing,omitempty"`
	TargetEntry   int    `json:"targetEntry,omitempty"`
}

func (t *nativeWorkspaceTools) invoke(name string, raw json.RawMessage) (any, error) {
	request, err := readJSONFile(filepath.Join(t.directory, "native-request.json"))
	if err != nil {
		return nil, err
	}
	release, err := admitExecution(t.directory, objectOrEmpty(request), t.services)
	if err != nil {
		return nil, err
	}
	if release != nil {
		defer release()
	}
	if err := t.refreshFacts(); err != nil {
		return nil, err
	}
	switch name {
	case nativeWorkspaceTool:
		return t.workspaceOperation(raw)
	case nativeSemanticTool:
		return t.submitSemantics(raw)
	default:
		return nil, fmt.Errorf("unknown native tool")
	}
}

func (t *nativeWorkspaceTools) call(raw json.RawMessage) (any, error) {
	return t.invoke(nativeWorkspaceTool, raw)
}

func (t *nativeWorkspaceTools) workspaceOperation(raw json.RawMessage) (any, error) {
	var a nativeWorkspaceArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&a); err != nil {
		return nil, err
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
		return map[string]any{"path": document.Path, "contentDigest": document.ContentDigest, "from": from, "to": to, "lines": lines[from-1 : to]}, nil
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
		rows, err := vcs.NewNative(resource.Path, input.VCS, nil).At(resource.Path, resource.Handle).CompareSnapshots(base, head)
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
		return map[string]any{"changes": readable}, nil
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
			if t.materials != "" && path == filepath.Dir(t.materials) && entry.Name() != filepath.Base(t.materials) {
				continue
			}
			name := filepath.Join(relative, entry.Name())
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
			if string(body) != source.Content {
				return nil, fmt.Errorf("source changed after the selected read")
			}
			lines := strings.SplitAfter(source.Content, "\n")
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
		if err := t.writable(path); err != nil {
			return nil, err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if string(body) != fact.Content {
			return nil, fmt.Errorf("file changed after the selected read; read its current contents")
		}
		lines := strings.SplitAfter(fact.Content, "\n")
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
		if err := os.WriteFile(path, []byte(result), info.Mode().Perm()); err != nil {
			return nil, err
		}
		return map[string]any{"written": true}, nil
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
		if err := t.writable(path); err != nil {
			return nil, err
		}
		if a.Action == "mkdir" {
			return map[string]any{"created": true}, os.Mkdir(path, 0755)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		_, err = file.WriteString(a.Content)
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		return map[string]any{"written": true}, closeErr
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
		if err := t.writable(target); err != nil {
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
			if err := t.writable(source); err != nil {
				return nil, err
			}
			return map[string]any{"moved": true}, os.Rename(source, target)
		}
		body, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
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
	case "remove":
		if t.readOnly || a.Listing == 0 {
			return nil, fmt.Errorf("removal requires a writable action and published entry")
		}
		path, err := t.path(relative, false)
		if err != nil {
			return nil, err
		}
		if err := t.writable(path); err != nil {
			return nil, err
		}
		return map[string]any{"removed": true}, os.Remove(path)
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
		if err := worker.ValidateSemanticBody(t.material.Input, body); err != nil {
			return nil, err
		}
	}
	for i, fact := range t.facts {
		if fact.Kind == "semantics" && fact.Job == t.directory && fact.RequestDigest == t.requestDigest {
			if fact.Content != string(body) {
				return nil, fmt.Errorf("current invocation already submitted different semantics")
			}
			return map[string]any{"submitted": true, "fact": i + 1}, nil
		}
	}
	number, err := t.publish(nativeToolFact{Kind: "semantics", Content: string(body)})
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
	return []any{nativeWorkspaceToolSchema(t.readOnly, t.material), map[string]any{"name": nativeSemanticTool, "description": "Submit this invocation's semantic result once. The program binds the role, identities and receipt; final messages only finish or explain.", "inputSchema": schema}}
}

func nativeWorkspaceToolSchema(readOnly bool, materials ...*worker.Material) map[string]any {
	var material *worker.Material
	if len(materials) > 0 {
		material = materials[0]
	}
	actions := []any{"list", "read", "search"}
	properties := map[string]any{"query": map[string]any{"type": "string", "minLength": 1}, "listing": map[string]any{"type": "integer", "minimum": 1}, "entry": map[string]any{"type": "integer", "minimum": 1}}
	description := "Search performs recursive literal content matching in the selected directory (omit listing/entry for root), publishing original listing/read numbers and matching lines. Select existing files by original listing and entry numbers. Numbers remain bound across continuation and resume."
	if material != nil && material.Input.RequirementDocument.Content != "" {
		actions = append(actions, "requirement")
		description += " Requirement reads the bound original confirmed document, optionally by from/to lines."
		properties["from"] = map[string]any{"type": "integer", "minimum": 1}
		properties["to"] = map[string]any{"type": "integer", "minimum": 0}
	}
	if material != nil && material.Input.Validation != nil && (material.Input.Validation.Gate != nil || material.Input.Validation.ReviewArtifact != nil) {
		actions = append(actions, "comparison")
		description += " Comparison reads the issued native versions."
	}
	if !readOnly {
		actions = append(actions, "replace", "copy-range", "create", "mkdir", "remove", "copy", "move")
		description += " Replace uses an original read and from/to line range (empty: 1/0). Copy-range takes sourceRead/sourceFrom/sourceTo and target read/from/to; to=from-1 inserts original bytes. Copy/move use source listing/entry and targetListing/targetEntry (omit for root); omit name to retain the original basename."
		for _, key := range []string{"read", "from", "sourceRead", "sourceFrom", "sourceTo", "targetListing", "targetEntry"} {
			properties[key] = map[string]any{"type": "integer", "minimum": 1}
		}
		properties["to"] = map[string]any{"type": "integer", "minimum": 0}
		properties["name"] = map[string]any{"type": "string"}
		properties["content"] = map[string]any{"type": "string"}
	}
	description += " Read publishes Go testSymbols from the original full source for result selection."
	properties["action"] = map[string]any{"type": "string", "enum": actions}
	return map[string]any{"name": nativeWorkspaceTool, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": []any{"action"}, "additionalProperties": false}}
}

// NativeTools serves only MCP's handshake, discovery and tool-call methods.
// The signed native request supplies physical execution parameters.
func NativeTools(directory string, in io.Reader, out, errout io.Writer, services ...ExecutionServices) int {
	tools, err := newNativeWorkspaceTools(directory, services...)
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	decoder := json.NewDecoder(in)
	encoder := json.NewEncoder(out)
	for {
		var rawRequest json.RawMessage
		var request struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := decoder.Decode(&rawRequest); err == io.EOF {
			return 0
		} else if err != nil {
			fmt.Fprintln(errout, err)
			return 1
		}
		if err := json.Unmarshal(rawRequest, &request); err != nil {
			fmt.Fprintln(errout, err)
			return 1
		}
		if len(request.ID) == 0 {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			err = json.Unmarshal(request.Params, &params)
			result = map[string]any{"protocolVersion": params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "formal-gates", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": tools.schemas()}
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			err = json.Unmarshal(request.Params, &params)
			if err == nil && params.Name != nativeWorkspaceTool && params.Name != nativeSemanticTool {
				err = fmt.Errorf("unknown native tool")
			}
			var value any
			if err == nil {
				value, err = tools.invoke(params.Name, params.Arguments)
			}
			text := ""
			if err != nil {
				text = err.Error()
			} else {
				body, marshalErr := json.Marshal(value)
				err = marshalErr
				text = string(body)
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": err != nil}
			err = nil
			if err := tools.recordCall(rawRequest, result); err != nil {
				fmt.Fprintln(errout, err)
				return 1
			}
		default:
			err = fmt.Errorf("unsupported MCP method %s", request.Method)
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}
		if err != nil {
			delete(response, "result")
			response["error"] = map[string]any{"code": -32601, "message": err.Error()}
			err = nil
		}
		if err := encoder.Encode(response); err != nil {
			fmt.Fprintln(errout, err)
			return 1
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
