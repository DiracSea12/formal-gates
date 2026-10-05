package vcs

import (
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type svnInfo struct {
	Entries []struct {
		Path       string `xml:"path,attr"`
		Kind       string `xml:"kind,attr"`
		Revision   int    `xml:"revision,attr"`
		URL        string `xml:"url"`
		Relative   string `xml:"relative-url"`
		Repository struct {
			Root string `xml:"root"`
			UUID string `xml:"uuid"`
		}
		WC struct {
			Root string `xml:"wcroot-abspath"`
		} `xml:"wc-info"`
		Conflict []struct {
			Type string `xml:"type,attr"`
		} `xml:"conflict"`
	} `xml:"entry"`
}
type svnStatus struct {
	Targets []struct {
		Entries []struct {
			Path   string `xml:"path,attr"`
			Status struct {
				Item         string `xml:"item,attr"`
				Props        string `xml:"props,attr"`
				Switched     bool   `xml:"switched,attr"`
				TreeConflict bool   `xml:"tree-conflicted,attr"`
				Revision     int    `xml:"revision,attr"`
			} `xml:"wc-status"`
		} `xml:"entry"`
	} `xml:"target"`
}
type svnProperties struct {
	Targets []struct {
		Path       string `xml:"path,attr"`
		Properties []struct {
			Name     string `xml:"name,attr"`
			Encoding string `xml:"encoding,attr"`
			Value    string `xml:",chardata"`
		} `xml:"property"`
	} `xml:"target"`
}
type svnLog struct {
	Entries []struct {
		Revision int    `xml:"revision,attr"`
		Message  string `xml:"msg"`
		Paths    []struct {
			Path    string `xml:",chardata"`
			Action  string `xml:"action,attr"`
			From    string `xml:"copyfrom-path,attr"`
			FromRev int    `xml:"copyfrom-rev,attr"`
		} `xml:"paths>path"`
	} `xml:"logentry"`
}

func (p *Provider) svnXML(target any, args ...string) error {
	body, err := p.runner.RunRaw(p.root, "svn", nil, args...)
	if err != nil {
		return err
	}
	return xml.Unmarshal(body, target)
}
func svnURL(id nativeIdentity) string { return id.Scope + "@" + strconv.Itoa(id.Revision) }
func (p *Provider) svnResolve(revision string) (string, error) {
	var info svnInfo
	args := []string{"info", "--xml", p.root}
	if revision != "" {
		args = []string{"info", "--xml", "-r", revision, p.root}
	}
	if err := p.svnXML(&info, args...); err != nil {
		return "", err
	}
	if len(info.Entries) != 1 {
		return "", fmt.Errorf("SVN working copy root is not unique")
	}
	entry := info.Entries[0]
	if revision == "" && !SamePath(entry.WC.Root, p.root) {
		return "", fmt.Errorf("SVN working copy root differs from bound root")
	}
	rev := entry.Revision
	if revision != "" {
		var err error
		rev, err = strconv.Atoi(revision)
		if err != nil {
			return "", err
		}
	}
	return encodeNative(nativeIdentity{Provider: "svn", Repository: entry.Repository.UUID, Scope: entry.URL, Revision: rev, Address: entry.Repository.Root}), nil
}
func (p *Provider) svnStatuses() (svnStatus, error) {
	var status svnStatus
	err := p.svnXML(&status, "status", "--xml", "--no-ignore", p.root)
	return status, err
}
func (p *Provider) svnPath(native string) string {
	relative, err := filepath.Rel(p.root, native)
	if err == nil && filepath.IsAbs(native) {
		return filepath.ToSlash(relative)
	}
	if native == p.root {
		return "."
	}
	return filepath.ToSlash(native)
}
func (p *Provider) svnChangedPaths() ([]string, error) {
	status, err := p.svnStatuses()
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, target := range status.Targets {
		for _, entry := range target.Entries {
			state := entry.Status
			if (state.Item == "unversioned" || state.Item == "ignored") && isRuntimeStatusPath(p.svnPath(entry.Path)) {
				continue
			}
			if state.Item != "normal" && state.Item != "none" || state.Props != "normal" && state.Props != "none" && state.Props != "" || state.Switched || state.TreeConflict {
				if state.Item == "unversioned" || state.Item == "ignored" {
					leaves, err := p.untrackedPaths(p.svnPath(entry.Path))
					if err != nil {
						return nil, err
					}
					paths = append(paths, leaves...)
				} else {
					paths = append(paths, p.svnPath(entry.Path))
				}
			}
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}
func (p *Provider) svnStatus() (string, error) {
	paths, err := p.svnChangedPaths()
	if err != nil {
		return "", err
	}
	var info svnInfo
	if err = p.svnXML(&info, "info", "--xml", "-R", p.root); err != nil {
		return "", err
	}
	revisions := map[int]bool{}
	for _, entry := range info.Entries {
		if entry.Revision > 0 {
			revisions[entry.Revision] = true
		}
	}
	if len(revisions) > 1 {
		paths = append(paths, "mixed-revision working copy")
	}
	return strings.Join(paths, "\n"), nil
}
func (p *Provider) svnEntries(id nativeIdentity) (map[string]string, error) {
	var listing struct {
		Lists []struct {
			Entries []struct {
				Name string `xml:"name"`
				Kind string `xml:"kind,attr"`
			} `xml:"entry"`
		} `xml:"list"`
	}
	if err := p.svnXML(&listing, "list", "--xml", "-R", svnURL(id)); err != nil {
		return nil, err
	}
	entries := map[string]nativeEntry{".": {Kind: "dir"}}
	for _, list := range listing.Lists {
		for _, entry := range list.Entries {
			value := nativeEntry{Kind: entry.Kind}
			if entry.Kind == "file" {
				body, err := p.runner.RunRaw(p.root, "svn", nil, "cat", id.Scope+"/"+entry.Name+"@"+strconv.Itoa(id.Revision), "-r", strconv.Itoa(id.Revision))
				if err != nil {
					return nil, err
				}
				value.Content = body
			}
			entries[entry.Name] = value
		}
	}
	var properties svnProperties
	if err := p.svnXML(&properties, "proplist", "--xml", "--verbose", "-R", svnURL(id)); err != nil {
		return nil, err
	}
	for _, target := range properties.Targets {
		key := strings.TrimPrefix(target.Path, id.Scope)
		key = strings.TrimPrefix(key, "/")
		if key == "" {
			key = "."
		}
		value, ok := entries[key]
		if !ok {
			return nil, fmt.Errorf("SVN property has no snapshot node: %s", key)
		}
		value.Properties = map[string][]byte{}
		for _, property := range target.Properties {
			body := []byte(property.Value)
			if property.Encoding == "base64" {
				var err error
				body, err = base64.StdEncoding.DecodeString(property.Value)
				if err != nil {
					return nil, err
				}
			}
			value.Properties[property.Name] = body
		}
		entries[key] = value
	}
	result := map[string]string{}
	for key, value := range entries {
		result[key] = entryValue(value)
	}
	return result, nil
}
func (p *Provider) svnTrack(paths []string) error {
	status, err := p.svnStatuses()
	if err != nil {
		return err
	}
	for _, target := range status.Targets {
		for _, entry := range target.Entries {
			key := p.svnPath(entry.Path)
			switch entry.Status.Item {
			case "unversioned", "ignored":
				for _, declared := range paths {
					if declared == key || strings.HasPrefix(declared, key+"/") {
						if _, err = p.runner.Run(p.root, "svn", "add", "--parents", "--force", "--", declared); err != nil {
							return err
						}
					}
				}
			case "missing":
				if slices.Contains(paths, key) {
					if _, err = p.runner.Run(p.root, "svn", "delete", "--force", "--", key); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
func (p *Provider) svnCommit(message string) error {
	if _, err := p.runner.Run(p.root, "svn", "commit", "-m", message, p.root); err != nil {
		return err
	}
	// Commit does not advance unchanged WC nodes. Update to the exact observed
	// operation revision so Resolve and readiness carry one native snapshot.
	id, err := p.svnResolve("")
	if err != nil {
		return err
	}
	bound, _ := decodeNative(id)
	var log svnLog
	if err = p.svnXML(&log, "log", "--xml", "-l", "1", bound.Scope); err != nil {
		return err
	}
	if len(log.Entries) != 1 || log.Entries[0].Message != message {
		return fmt.Errorf("SVN commit has no exact operation log")
	}
	_, err = p.runner.Run(p.root, "svn", "update", "-r", strconv.Itoa(log.Entries[0].Revision), p.root)
	return err
}
func (p *Provider) svnMessage(id nativeIdentity) (string, error) {
	var log svnLog
	if err := p.svnXML(&log, "log", "--xml", "-r", strconv.Itoa(id.Revision), svnURL(id)); err != nil {
		return "", err
	}
	if len(log.Entries) != 1 {
		return "", fmt.Errorf("SVN snapshot has no scoped operation")
	}
	return log.Entries[0].Message, nil
}
func (p *Provider) svnAncestor(a, b nativeIdentity) error {
	var log svnLog
	if err := p.svnXML(&log, "log", "--xml", "-v", "--stop-on-copy", svnURL(b)); err != nil {
		return err
	}
	relative := strings.TrimPrefix(b.Scope, b.Address)
	for _, entry := range log.Entries {
		if a.Scope == b.Scope && entry.Revision <= a.Revision {
			return nil
		}
		for _, change := range entry.Paths {
			if change.Path == relative && change.From != "" {
				copy := b
				copy.Scope = b.Address + change.From
				copy.Revision = change.FromRev
				if err := p.svnAncestor(a, copy); err == nil {
					return nil
				}
			}
		}
	}
	out, err := p.runner.Run(p.root, "svn", "mergeinfo", "--show-revs", "merged", svnURL(a), svnURL(b))
	if err == nil {
		for _, line := range strings.Fields(out) {
			if line == "r"+strconv.Itoa(a.Revision) {
				return nil
			}
		}
	}
	return fmt.Errorf("SVN snapshot has no copy or merge lineage from the required scope")
}
func (p *Provider) svnCount(a, b nativeIdentity) (int, error) {
	if a.Scope != b.Scope {
		return 0, fmt.Errorf("SVN count requires one native scope")
	}
	if a.Revision == b.Revision {
		return 0, nil
	}
	var log svnLog
	if err := p.svnXML(&log, "log", "--xml", "-r", fmt.Sprintf("%d:%d", a.Revision+1, b.Revision), svnURL(b)); err != nil {
		return 0, err
	}
	return len(log.Entries), nil
}
func (p *Provider) svnWorkspaceExists(workspace string, h WorkspaceHandle) (bool, error) {
	if _, err := os.Stat(filepath.Join(workspace, ".svn")); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	native := p.At(workspace, h)
	identity, err := native.Resolve()
	if err != nil {
		return false, err
	}
	id, _ := decodeNative(identity)
	if id.Scope != h.URL {
		return false, fmt.Errorf("SVN workspace URL differs from its registered scope")
	}
	return true, nil
}
func (p *Provider) svnCreatedRevision(id nativeIdentity, h WorkspaceHandle, message string) (int, error) {
	var log svnLog
	if err := p.svnXML(&log, "log", "--xml", "-v", "--stop-on-copy", h.URL); err != nil {
		return 0, err
	}
	for _, entry := range log.Entries {
		if entry.Message != message {
			continue
		}
		for _, change := range entry.Paths {
			if id.Address+change.Path == h.URL && id.Address+change.From == id.Scope && change.FromRev == id.Revision {
				return entry.Revision, nil
			}
		}
	}
	return 0, fmt.Errorf("SVN candidate URL has no exact native copy lineage")
}
func (p *Provider) svnCreateWorkspace(workspace string, id nativeIdentity, message string, h WorkspaceHandle) error {
	var info svnInfo
	exists := p.svnXML(&info, "info", "--xml", h.URL) == nil
	if !exists {
		if _, err := p.runner.Run(p.root, "svn", "copy", "--parents", svnURL(id), h.URL, "-m", message); err != nil {
			return err
		}
	}
	revision, err := p.svnCreatedRevision(id, h, message)
	if err != nil {
		return err
	}
	if exists, err := p.svnWorkspaceExists(workspace, h); err != nil {
		return err
	} else if exists {
		return nil
	}
	_, err = p.runner.Run(p.root, "svn", "checkout", "-r", strconv.Itoa(revision), h.URL+"@"+strconv.Itoa(revision), workspace)
	return err
}
func (p *Provider) svnWorkspaceAbsent(workspace string, h WorkspaceHandle) (bool, error) {
	var info svnInfo
	if p.svnXML(&info, "info", "--xml", h.URL) == nil {
		return false, nil
	}
	_, err := os.Stat(workspace)
	return os.IsNotExist(err), nil
}
func (p *Provider) svnCleanupWorkspace(workspace, message string, h WorkspaceHandle) error {
	source, err := decodeNative(h.Source)
	if err != nil {
		return err
	}
	var info svnInfo
	if p.svnXML(&info, "info", "--xml", h.URL) == nil {
		var log svnLog
		if err = p.svnXML(&log, "log", "--xml", "-v", "--stop-on-copy", h.URL); err != nil {
			return err
		}
		owned := false
		for _, entry := range log.Entries {
			for _, change := range entry.Paths {
				owned = owned || source.Address+change.Path == h.URL && source.Address+change.From == source.Scope && change.FromRev == source.Revision
			}
		}
		if !owned {
			return fmt.Errorf("SVN cleanup URL has no registered copy lineage")
		}
		if _, err = p.runner.Run(p.root, "svn", "delete", h.URL, "-m", message); err != nil {
			return err
		}
	}
	return os.RemoveAll(workspace)
}
func (p *Provider) svnApply(id nativeIdentity, paths []string) error {
	entries, err := p.svnEntries(id)
	if err != nil {
		return err
	}
	paths = slices.Clone(paths)
	slices.SortFunc(paths, func(a, b string) int { return strings.Count(a, "/") - strings.Count(b, "/") })
	for _, key := range paths {
		target := materialPath(p.root, key)
		encoded := entries[key]
		if encoded == "" {
			if _, err := os.Lstat(target); os.IsNotExist(err) {
				continue
			}
			if _, err = p.runner.Run(p.root, "svn", "delete", "--force", "--", key); err != nil {
				return err
			}
			continue
		}
		var value nativeEntry
		if err = json.Unmarshal([]byte(encoded), &value); err != nil {
			return err
		}
		if value.Kind == "dir" {
			if err = os.MkdirAll(target, 0755); err != nil {
				return err
			}
		} else {
			if _, err = p.runner.Run(p.root, "svn", "export", "--force", "-r", strconv.Itoa(id.Revision), id.Scope+"/"+key+"@"+strconv.Itoa(id.Revision), target); err != nil {
				return err
			}
		}
		if key != "." {
			if _, err = p.runner.Run(p.root, "svn", "add", "--parents", "--force", "--depth", "empty", "--", key); err != nil {
				return err
			}
		}
		var properties svnProperties
		if err = p.svnXML(&properties, "proplist", "--xml", "--verbose", target); err != nil {
			return err
		}
		for _, node := range properties.Targets {
			for _, property := range node.Properties {
				if _, ok := value.Properties[property.Name]; !ok {
					if _, err = p.runner.Run(p.root, "svn", "propdel", property.Name, "--", key); err != nil {
						return err
					}
				}
			}
		}
		for name, body := range value.Properties {
			file, err := os.CreateTemp("", "svn-property-")
			if err != nil {
				return err
			}
			filename := file.Name()
			_, writeErr := file.Write(body)
			closeErr := file.Close()
			if writeErr == nil {
				writeErr = closeErr
			}
			if writeErr == nil {
				_, writeErr = p.runner.Run(p.root, "svn", "propset", name, "-F", filename, "--", key)
			}
			_ = os.Remove(filename)
			if writeErr != nil {
				return writeErr
			}
		}
	}
	return p.svnTrack(paths)
}
func (p *Provider) svnPending(base, expected string) (Pending, error) {
	paths, err := p.svnChangedPaths()
	if err != nil {
		return Pending{}, err
	}
	entries, err := p.svnIndex()
	if err != nil {
		return Pending{}, err
	}
	result := Pending{Fingerprint: hashValue(entries), Clean: len(paths) == 0, Tracked: len(paths) > 0}
	status, err := p.svnStatuses()
	if err != nil {
		return result, err
	}
	for _, target := range status.Targets {
		for _, entry := range target.Entries {
			if entry.Status.Item == "unversioned" || entry.Status.Item == "ignored" || entry.Status.Item == "missing" {
				if isRuntimeStatusPath(p.svnPath(entry.Path)) {
					continue
				}
				leaves, err := p.untrackedPaths(p.svnPath(entry.Path))
				if os.IsNotExist(err) {
					leaves = []string{p.svnPath(entry.Path)}
				} else if err != nil {
					return result, err
				}
				if len(leaves) == 0 {
					continue
				}
				result.Tracked = false
			}
		}
	}
	baseEntries, err := p.TreeEntries(base)
	if err != nil {
		return result, err
	}
	working := map[string]string{}
	for _, entry := range entries {
		working[entry.Path] = entry.Metadata
	}
	for _, key := range paths {
		a, b := baseEntries[key], working[key]
		kind := "M"
		if a == "" {
			kind = "A"
		}
		if b == "" {
			kind = "D"
		}
		result.Changes = append(result.Changes, Change{Path: key, Kind: kind, BaseMode: entryMode(a), Mode: entryMode(b), BaseObject: entryObject(a), Object: entryObject(b), BaseMetadata: a, Metadata: b})
	}
	if expected != "" {
		id, err := decodeNative(expected)
		if err != nil {
			return result, err
		}
		body, err := p.runner.RunRaw(p.root, "svn", nil, "diff", "--force", "--old", svnURL(id), "--new", p.root)
		if err != nil {
			return result, err
		}
		result.Equal = len(body) == 0
		want, err := p.TreeEntries(expected)
		if err != nil {
			return result, err
		}
		changes := entryChanges(baseEntries, want)
		result.Prefix = true
		for _, key := range paths {
			position := slices.IndexFunc(changes, func(c Change) bool { return c.Path == key })
			if position < 0 {
				result.Prefix = false
				continue
			}
			if changes[position].Mode == "000000" {
				_, err := os.Lstat(materialPath(p.root, key))
				result.Prefix = result.Prefix && os.IsNotExist(err)
				continue
			}
			old := id.Scope
			if key != "." {
				old += "/" + key
			}
			diff, err := p.runner.RunRaw(p.root, "svn", nil, "diff", "--force", "--old", old+"@"+strconv.Itoa(id.Revision), "--new", materialPath(p.root, key))
			if err != nil {
				return result, err
			}
			result.Prefix = result.Prefix && len(diff) == 0
		}

	}
	return result, nil
}
func (p *Provider) svnIndex() ([]IndexEntry, error) {
	var info svnInfo
	if err := p.svnXML(&info, "info", "--xml", "-R", p.root); err != nil {
		return nil, err
	}
	values := map[string]nativeEntry{}
	conflicts := map[string]string{}
	for _, node := range info.Entries {
		key := p.svnPath(node.Path)
		value := nativeEntry{Kind: node.Kind}
		if node.Kind == "file" {
			body, err := workingBytes(materialPath(p.root, key))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			value.Content = body
		}
		values[key] = value
		for _, conflict := range node.Conflict {
			conflicts[key] = conflict.Type
		}
	}
	var properties svnProperties
	if err := p.svnXML(&properties, "proplist", "--xml", "--verbose", "-R", p.root); err != nil {
		return nil, err
	}
	for _, node := range properties.Targets {
		key := p.svnPath(node.Path)
		value, ok := values[key]
		if !ok {
			continue
		}
		value.Properties = map[string][]byte{}
		for _, property := range node.Properties {
			body := []byte(property.Value)
			if property.Encoding == "base64" {
				var err error
				body, err = base64.StdEncoding.DecodeString(property.Value)
				if err != nil {
					return nil, err
				}
			}
			value.Properties[property.Name] = body
		}
		values[key] = value
	}
	status, err := p.svnStatuses()
	if err != nil {
		return nil, err
	}
	for _, target := range status.Targets {
		for _, node := range target.Entries {
			key := p.svnPath(node.Path)
			if node.Status.Item == "conflicted" || node.Status.Props == "conflicted" || node.Status.TreeConflict {
				conflicts[key] = node.Status.Item + ":" + node.Status.Props
			}
			if (node.Status.Item == "unversioned" || node.Status.Item == "ignored") && !isRuntimeStatusPath(key) {
				body, err := workingBytes(materialPath(p.root, key))
				if err == nil {
					values[key] = nativeEntry{Kind: "file", Content: body}
				}
			}
		}
	}
	var entries []IndexEntry
	for key, value := range values {
		encoded := entryValue(value)
		stage := 0
		if conflicts[key] != "" {
			stage = 1
		}
		entries = append(entries, IndexEntry{Path: key, Mode: entryMode(encoded), Object: entryObject(encoded), Metadata: encoded, Stage: stage, Conflict: conflicts[key]})
	}
	slices.SortFunc(entries, func(a, b IndexEntry) int { return strings.Compare(a.Path, b.Path) })
	return entries, nil
}
func (p *Provider) svnMerge(incoming nativeIdentity) error {
	_, err := p.runner.Run(p.root, "svn", "merge", "--accept", "postpone", svnURL(incoming), p.root)
	if err != nil {
		entries, inspect := p.svnIndex()
		if inspect != nil {
			return inspect
		}
		if !slices.ContainsFunc(entries, func(e IndexEntry) bool { return e.Stage != 0 }) {
			return err
		}
	}
	return nil
}
func (p *Provider) svnObserveIntegration(base, incoming string) (IntegrationObservation, error) {
	id, err := decodeNative(incoming)
	if err != nil {
		return IntegrationObservation{}, err
	}
	entries, err := p.svnIndex()
	if err != nil {
		return IntegrationObservation{}, err
	}
	out, err := p.runner.Run(p.root, "svn", "mergeinfo", "--show-revs", "merged", svnURL(id), p.root)
	if err != nil {
		return IntegrationObservation{}, err
	}
	started := slices.Contains(strings.Fields(out), "r"+strconv.Itoa(id.Revision))
	for _, entry := range entries {
		started = started || entry.Stage != 0
	}
	lineage := hashValue(struct {
		Incoming, Mergeinfo string
		Entries             []IndexEntry
	}{incoming, out, entries})
	return IntegrationObservation{Started: started, Lineage: lineage, Entries: entries}, nil
}
