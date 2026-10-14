package vcs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Native entries preserve repository bytes and all versioned metadata. Their
// canonical encoding is also used by Entry and the existing validation views.
type nativeEntry struct {
	Resolve    map[string]string `json:"resolve,omitempty"`
	Action     string            `json:"action,omitempty"`
	Layers     []nativeEntry     `json:"layers,omitempty"`
	Kind       string            `json:"kind"`
	Content    []byte            `json:"content,omitempty"`
	Properties map[string][]byte `json:"properties,omitempty"`
	Filetype   string            `json:"filetype,omitempty"`
	Origin     string            `json:"origin,omitempty"`
	View       []string          `json:"view,omitempty"`
	LineEnd    string            `json:"lineEnd,omitempty"`
	Charset    string            `json:"charset,omitempty"`
	StreamSpec string            `json:"streamSpec,omitempty"`
}

func entryValue(entry nativeEntry) string { b, _ := json.Marshal(entry); return string(b) }
func hashValue(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func entryMode(value string) string {
	if value == "" {
		return "000000"
	}
	var e nativeEntry
	_ = json.Unmarshal([]byte(value), &e)
	if e.Kind == "dir" {
		return "040000"
	}
	if e.Properties["svn:special"] != nil || strings.HasPrefix(e.Filetype, "symlink") {
		return "120000"
	}
	if e.Properties["svn:executable"] != nil || filetypeExecutable(e.Filetype) {
		return "100755"
	}
	return "100644"
}
func entryObject(value string) string {
	if value == "" {
		return ""
	}
	var e nativeEntry
	_ = json.Unmarshal([]byte(value), &e)
	return hashValue(e.Content)
}
func entryChanges(base, current map[string]string) []Change {
	paths := make([]string, 0, len(base)+len(current))
	for path := range base {
		paths = append(paths, path)
	}
	for path := range current {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	var changes []Change
	for _, path := range paths {
		a, b := base[path], current[path]
		if a == b {
			continue
		}
		kind := "M"
		if a == "" {
			kind = "A"
		}
		if b == "" {
			kind = "D"
		}
		changes = append(changes, Change{Path: path, Kind: kind, BaseMode: entryMode(a), Mode: entryMode(b), BaseObject: entryObject(a), Object: entryObject(b), BaseMetadata: a, Metadata: b})
	}
	return changes
}

// IsDirectoryContainer identifies a newly versioned, property-free directory
// needed to hold a declared descendant. The directory remains in native evidence;
// directory properties, removals and independent empty directories need a declaration.
func (c Change) IsDirectoryContainer(paths []string) bool {
	return c.Kind == "A" && c.BaseMetadata == "" && c.Metadata == entryValue(nativeEntry{Kind: "dir"}) &&
		slices.ContainsFunc(paths, func(path string) bool { return strings.HasPrefix(path, c.Path+"/") })
}

func (p *Provider) nativeChanges(base, current string) ([]Change, error) {
	a, err := p.nativeEntries(base)
	if err != nil {
		return nil, err
	}
	b, err := p.nativeEntries(current)
	if err != nil {
		return nil, err
	}
	return entryChanges(a, b), nil
}
func (p *Provider) nativeDigest(identity string) (string, error) {
	entries, err := p.nativeEntries(identity)
	if err != nil {
		return "", err
	}
	return hashValue(entries), nil
}

// Pending distinguishes mutable native evidence from an immutable snapshot.
// Git's index remains addressable; SVN/P4 fingerprints are observation only.
type Pending struct {
	Fingerprint           string
	Changes               []Change
	Clean, Tracked, Equal bool
	Prefix                bool
}

func (p *Provider) Pending(base, expected string) (Pending, error) {
	if p.name == "git" {
		tree, err := p.StagedTreeDigest()
		if err != nil {
			return Pending{}, err
		}
		changes, err := p.Changes(base, tree)
		if err != nil {
			return Pending{}, err
		}
		status, err := p.Status()
		if err != nil {
			return Pending{}, err
		}
		tracked, err := p.StagedOnly()
		if err != nil {
			return Pending{}, err
		}
		equal := false
		if expected != "" {
			digest, e := p.TreeDigest(expected)
			if e != nil {
				return Pending{}, e
			}
			equal = digest == tree
		}
		return Pending{Fingerprint: tree, Changes: changes, Clean: status == "", Tracked: tracked, Equal: equal, Prefix: true}, nil
	}
	if p.name == "svn" {
		return p.svnPending(base, expected)
	}
	return p.p4Pending(base, expected)
}

func (p *Provider) WorkspaceExists(path string, h WorkspaceHandle) (bool, error) {
	if p.name == "git" {
		trees, err := p.Worktrees()
		return slices.ContainsFunc(trees, func(t Worktree) bool { return SamePath(t.Path, path) }), err
	}
	if p.name == "svn" {
		return p.svnWorkspaceExists(path, h)
	}
	return p.p4WorkspaceExists(path, h)
}

// ObservedWorkspace proves the registered copy's exact source and operation
// even when its local checkout or sync has not finished.
func (p *Provider) ObservedWorkspace(path, source, message string, h WorkspaceHandle) (string, bool, error) {
	if p.name == "git" {
		exists, err := p.WorkspaceExists(path, h)
		if err != nil || !exists {
			return "", false, err
		}
		identity, err := p.At(path, h).Resolve()
		if err == nil && identity != source {
			err = fmt.Errorf("workspace identity differs from original source")
		}
		return identity, err == nil, err
	}
	id, err := decodeNative(source)
	if err != nil {
		return "", false, err
	}
	if p.name == "svn" {
		exists, err := p.svnURLExists(h.URL)
		if err != nil || !exists {
			return "", false, err
		}
		revision, err := p.svnCreatedRevision(id, h, message)
		id.Scope, id.Revision = h.URL, revision
		return encodeNative(id), err == nil, err
	}
	bound := p.At(p.root, h)
	revision, err := bound.p4WorkspaceRevision(h, message)
	if err != nil || revision == 0 {
		return "", false, err
	}
	identity, err := bound.p4ResolveAt(strconv.Itoa(revision), path)
	return identity, err == nil, err
}

// ObservedWorkspaceCleanup locates the exact resource deletion independently
// of the remaining client, ref and local directory cleanup.
func (p *Provider) ObservedWorkspaceCleanup(source, message string, h WorkspaceHandle) (bool, error) {
	id, err := decodeNative(source)
	if err != nil {
		return false, err
	}
	if p.name == "svn" {
		var log svnLog
		if err := p.svnXML(&log, "log", "--xml", "-v", "-r", "HEAD:"+strconv.Itoa(id.Revision), svnLiteral(id.Address)); err != nil {
			return false, err
		}
		for _, entry := range log.Entries {
			if entry.Message != message {
				continue
			}
			for _, change := range entry.Paths {
				target, err := svnURLPath(id.Address, h.URL)
				if err != nil {
					return false, err
				}
				if change.Action == "D" && change.Path == target {
					return true, nil
				}
			}
		}
		return false, nil
	}
	revision, err := p.p4WorkspaceRevision(h, message)
	return revision > id.Revision, err
}

func (p *Provider) CreateWorkspace(path, source, message string, h WorkspaceHandle) error {
	if p.name == "git" {
		return p.AddWorktree(path, source)
	}
	id, err := decodeNative(source)
	if err != nil {
		return err
	}
	if p.name == "svn" {
		return p.svnCreateWorkspace(path, id, message, h)
	}
	return p.p4CreateWorkspace(path, id, message, h)
}
func (p *Provider) CleanupWorkspace(path, message string, h WorkspaceHandle) error {
	if p.name == "git" {
		return p.RemoveWorktree(path)
	}
	if p.name == "svn" {
		return p.svnCleanupWorkspace(path, message, h)
	}
	return p.p4CleanupWorkspace(path, message, h)
}
func (p *Provider) WorkspaceAbsent(path string, h WorkspaceHandle) (bool, error) {
	if p.name == "git" {
		exists, err := p.WorkspaceExists(path, h)
		return !exists, err
	}
	if p.name == "svn" {
		return p.svnWorkspaceAbsent(path, h)
	}
	return p.p4WorkspaceAbsent(path, h)
}

// IntegrationObservation carries actual native pending lineage and resolve
// facts. It never fills Git's MERGE_HEAD or parents for other providers.
type IntegrationObservation struct {
	Started bool
	Lineage string
	Entries []IndexEntry
}

func (p *Provider) ObserveIntegration(base, incoming string) (IntegrationObservation, error) {
	if p.name == "git" {
		head, err := p.MergeHead()
		if err != nil {
			return IntegrationObservation{}, err
		}
		entries, err := p.IndexEntries()
		return IntegrationObservation{Started: head == incoming, Lineage: head, Entries: entries}, err
	}
	if p.name == "svn" {
		return p.svnObserveIntegration(incoming)
	}
	return p.p4ObserveIntegration(base, incoming)
}
func (p *Provider) ResolveConflicts(paths []string) error {
	if p.name == "git" {
		return p.Track(paths)
	}
	if p.name == "svn" {
		if len(paths) == 0 {
			return nil
		}
		status, err := p.svnStatuses()
		if err != nil {
			return err
		}
		var unresolved []string
		for _, target := range status.Targets {
			for _, entry := range target.Entries {
				key := p.svnPath(entry.Path)
				if slices.Contains(paths, key) && (entry.Status.TreeConflict || entry.Status.Item == "conflicted" || entry.Status.Props == "conflicted") {
					unresolved = append(unresolved, key)
				}
			}
		}
		if len(unresolved) > 0 {
			if _, err := p.runner.Run(p.root, "svn", append([]string{"resolve", "--accept", "working", "--"}, svnLiterals(unresolved)...)...); err != nil {
				return err
			}
		}
		return p.Track(paths)
	}
	return p.p4ResolveConflicts(paths)
}
func (p *Provider) VerifyIntegration(base, incoming, head, message string) error {
	actual, err := p.Message(head)
	if err != nil {
		return err
	}
	if actual != message {
		return fmt.Errorf("native integration message differs from issued operation")
	}
	if p.name == "git" {
		parents, err := p.Parents(head)
		if err != nil {
			return err
		}
		if !slices.Equal(parents, []string{base, incoming}) {
			return fmt.Errorf("integration parents differ from original inputs")
		}
		return nil
	}
	if err = p.IsAncestorOrEqual(base, head); err != nil {
		return err
	}
	return p.IsAncestorOrEqual(incoming, head)
}
func materialPath(root, path string) string {
	if path == "." {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(path))
}

// Native status may report an unversioned directory without enumerating its
// children. Keep delivery leaves and empty directories while skipping only the
// registered tool inventory and existing runtime exclusions.
func (p *Provider) untrackedPaths(relative string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(materialPath(p.root, relative), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		key, e := filepath.Rel(p.root, path)
		if e != nil {
			return e
		}
		key = filepath.ToSlash(key)
		if p.isRuntimeStatusPath(key) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			paths = append(paths, key)
			return nil
		}
		children, e := os.ReadDir(path)
		if e != nil {
			return e
		}
		if len(children) == 0 && !p.isEmptyRuntimeDirectory(key) {
			paths = append(paths, key)
		}
		return nil
	})
	return paths, err
}

// ObservedCommit locates the exact bound operation on its native scope before
// another commit is considered. It also covers a commit whose WC update or
// receipt was interrupted after the repository accepted it.
func (p *Provider) ObservedCommit(base, message string) (string, bool, error) {
	if p.name == "git" {
		head, err := p.Resolve()
		if err != nil {
			return "", false, err
		}
		actual, err := p.Message(head)
		return head, actual == message, err
	}
	id, err := decodeNative(base)
	if err != nil {
		return "", false, err
	}
	if p.name == "svn" {
		var log svnLog
		// HEAD is valid before the operation exists; base is excluded below.
		if err = p.svnXML(&log, "log", "--xml", "-r", "HEAD:"+strconv.Itoa(id.Revision), svnLiteral(id.Scope)); err != nil {
			return "", false, err
		}
		var found string
		for _, entry := range log.Entries {
			if entry.Revision > id.Revision && entry.Message == message {
				if found != "" {
					return "", false, fmt.Errorf("multiple native commits match one operation")
				}
				observed := id
				observed.Revision = entry.Revision
				found = encodeNative(observed)
			}
		}
		return found, found != "", nil
	}
	records, err := p.p4JSON("changes", "-l", "-s", "submitted", "-c", p.p4Client(), "//"+p.p4Client()+"/...")
	if err != nil {
		return "", false, err
	}
	var revision string
	for _, record := range records {
		change, err := strconv.Atoi(record["change"])
		if err != nil {
			return "", false, err
		}
		if change > id.Revision && strings.TrimSuffix(record["desc"], "\n") == message {
			if revision != "" {
				return "", false, fmt.Errorf("multiple P4 changes match one operation")
			}
			revision = record["change"]
		}
	}
	if revision == "" {
		return "", false, nil
	}
	identity, err := p.p4Resolve(revision)
	return identity, err == nil, err
}
func (p *Provider) Synchronize(identity string) error {
	id, err := decodeNative(identity)
	if err != nil {
		return err
	}
	if p.name == "svn" {
		_, err = p.runner.Run(p.root, "svn", "update", "-r", strconv.Itoa(id.Revision), svnLiteral(p.root))
		return err
	}
	_, err = p.p4Raw(nil, "sync", "-f", "//"+p.p4Client()+"/...@"+strconv.Itoa(id.Revision))
	return err
}

// WorkspaceSynchronized proves the exact native base independently of later edits.
func (p *Provider) WorkspaceSynchronized(identity string) (bool, error) {
	head, err := p.Resolve()
	if err != nil || head != identity {
		return false, err
	}
	if p.name != "svn" {
		return true, nil
	}
	id, err := decodeNative(identity)
	if err != nil {
		return false, err
	}
	revisions, err := p.svnWorkingRevisions()
	return len(revisions) == 1 && revisions[id.Revision], err
}

func workingBytes(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return []byte(target), err
	}
	return os.ReadFile(path)
}

func filetypeExecutable(filetype string) bool {
	_, modifiers, ok := strings.Cut(filetype, "+")
	return ok && strings.Contains(modifiers, "x")
}
