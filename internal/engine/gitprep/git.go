// Package gitprep owns Git side effects for the regular preparation pipeline.
// Workers return paths; only this adapter stages, commits and freezes them.
package gitprep

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Entry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Type   string `json:"type"`
	Object string `json:"object"`
}
type View struct {
	ProviderMetadata map[string]string `json:"providerMetadata"`
	Identity         string            `json:"identity"`
	Tree             string            `json:"tree"`
	Entries          []Entry           `json:"entries"`
	Digest           string            `json:"digest"`
}
type Ownership struct {
	Status           string   `json:"status"`
	Paths            []string `json:"paths"`
	ProductionDigest string   `json:"productionDigest"`
	Reason           string   `json:"reason,omitempty"`
}
type Receipt struct {
	OperationID string     `json:"operationId"`
	Before      string     `json:"before"`
	After       string     `json:"after"`
	Paths       []string   `json:"paths"`
	View        View       `json:"view"`
	Ownership   *Ownership `json:"ownership,omitempty"`
}

func Git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}
func Digest(v any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		return ""
	}
	h := sha256.Sum256(buf.Bytes())
	return "sha256:" + hex.EncodeToString(h[:])
}

func Head(root string) (string, error) { return Git(root, "rev-parse", "--verify", "HEAD^{commit}") }
func Snapshot(root, identity string) (View, error) {
	id, err := Git(root, "rev-parse", "--verify", identity+"^{commit}")
	if err != nil {
		return View{}, err
	}
	tree, err := Git(root, "rev-parse", id+"^{tree}")
	if err != nil {
		return View{}, err
	}
	raw, err := Git(root, "ls-tree", "-r", "-z", id)
	if err != nil {
		return View{}, err
	}
	metadata := map[string]string{}
	for _, key := range []string{"core.autocrlf", "core.eol", "core.filemode", "core.symlinks", "extensions.objectFormat"} {
		out, err := exec.Command("git", "-C", root, "config", "--get", key).Output()
		if err == nil {
			metadata[key] = strings.TrimSpace(string(out))
		} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return View{}, err
		}
	}
	v := View{ProviderMetadata: metadata, Identity: id, Tree: tree, Entries: []Entry{}}
	for _, row := range strings.Split(raw, "\x00") {
		if row == "" {
			continue
		}
		a := strings.SplitN(row, "\t", 2)
		if len(a) != 2 {
			return View{}, fmt.Errorf("invalid Git tree")
		}
		fields := strings.Fields(a[0])
		if len(fields) != 3 {
			return View{}, fmt.Errorf("invalid Git entry")
		}
		v.Entries = append(v.Entries, Entry{a[1], fields[0], fields[1], fields[2]})
	}
	v.Digest = Digest(struct {
		Entries  []Entry
		Metadata map[string]string
	}{v.Entries, v.ProviderMetadata})
	return v, nil
}
func Paths(paths []string) ([]string, error) {
	out := append([]string(nil), paths...)
	sort.Strings(out)
	for i, p := range out {
		if p == "" || filepath.IsAbs(p) || filepath.ToSlash(filepath.Clean(p)) != p || p == ".." || strings.HasPrefix(p, "../") || p == ".git" || strings.HasPrefix(p, ".git/") || (i > 0 && out[i-1] == p) {
			return nil, fmt.Errorf("invalid or duplicate delivery path %q", p)
		}
	}
	return out, nil
}

// Changes includes staged, unstaged, deleted and untracked paths. Rename endpoints
// are deliberately represented as delete/add so neither can escape the manifest.
func Changes(root, base string) ([]string, error) {
	if _, err := Git(root, "status", "--porcelain=v1", "-z", "--untracked-files=all"); err != nil {
		return nil, err
	}
	unmerged, err := Git(root, "ls-files", "-u")
	if err != nil {
		return nil, err
	}
	if unmerged != "" {
		return nil, fmt.Errorf("unresolved Git index")
	}
	changed, err := Git(root, "diff", "--no-renames", "--name-only", "-z", base, "--")
	if err != nil {
		return nil, err
	}
	untracked, err := Git(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, s := range []string{changed, untracked} {
		for _, p := range strings.Split(s, "\x00") {
			if p != "" {
				set[p] = true
			}
		}
	}
	out := []string{}
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
func CheckManifest(root, base string, paths []string) error {
	p, err := Paths(paths)
	if err != nil {
		return err
	}
	actual, err := Changes(root, base)
	if err != nil {
		return err
	}
	if !equal(p, actual) {
		return fmt.Errorf("delivery manifest differs from status/diff/untracked: declared=%v actual=%v", p, actual)
	}
	return nil
}
func equal(a, b []string) bool { return strings.Join(a, "\x00") == strings.Join(b, "\x00") }

// Workspace uses a detached worktree with a known baseline. A restart validates
// the existing resource instead of silently recreating or mixing working trees.
func Workspace(root, path, base string) error {
	if _, err := os.Stat(path); err == nil {
		h, e := Head(path)
		if e != nil || h != base {
			return fmt.Errorf("UNKNOWN: workspace %s does not match %s", path, base)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	_, err := Git(root, "worktree", "add", "--detach", path, base)
	return err
}

// WorkingCopy overlays the registered worker's actual edits on an independent
// candidate worktree. It does not stage or commit the repair source. A caller
// records completion only after all modified, new and deleted paths are copied.
func WorkingCopy(root, source, path, base string) ([]string, error) {
	head, err := Head(source)
	if err != nil || head != base {
		return nil, fmt.Errorf("EXTERNAL_CHANGED: repair workspace does not match %s: %v", base, err)
	}
	paths, err := Changes(source, base)
	if err != nil {
		return nil, err
	}
	if err = Workspace(root, path, base); err != nil {
		return nil, err
	}
	for _, name := range paths {
		from, to := filepath.Join(source, name), filepath.Join(path, name)
		info, statErr := os.Lstat(from)
		if os.IsNotExist(statErr) {
			if err = os.RemoveAll(to); err != nil {
				return nil, err
			}
			continue
		}
		if statErr != nil {
			return nil, statErr
		}
		if err = os.MkdirAll(filepath.Dir(to), 0755); err != nil {
			return nil, err
		}
		if err = os.RemoveAll(to); err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(from)
			if err != nil {
				return nil, err
			}
			if err = os.Symlink(link, to); err != nil {
				return nil, err
			}
		} else {
			data, err := os.ReadFile(from)
			if err != nil {
				return nil, err
			}
			if err = os.WriteFile(to, data, info.Mode().Perm()); err != nil {
				return nil, err
			}
		}
	}
	return paths, nil
}

func Cleanup(root, path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	_, err := Git(root, "worktree", "remove", "--force", path)
	return err
}

// Commit receives a controller-owned operation ID. Its unique trailer and exact
// tree/parent check recover the commit-before-receipt crash window without making
// another commit. An unrelated HEAD is UNKNOWN, never adopted automatically.
func Commit(root, base, id string, paths []string) (Receipt, error) {
	head, err := Head(root)
	if err != nil {
		return Receipt{}, err
	}
	if head != base {
		parent, e := Git(root, "rev-parse", head+"^")
		if e != nil || parent != base {
			return Receipt{}, fmt.Errorf("UNKNOWN: candidate parent changed")
		}
		msg, e := Git(root, "log", "-1", "--format=%B")
		if e != nil || !strings.Contains(msg, "Formal-Gates-Operation: "+id) {
			return Receipt{}, fmt.Errorf("UNKNOWN: candidate lacks operation receipt")
		}
		dirty, e := Changes(root, head)
		if e != nil || len(dirty) > 0 {
			return Receipt{}, fmt.Errorf("UNKNOWN: post-commit worktree changed")
		}
		diff, e := Git(root, "diff", "--no-renames", "--name-only", "-z", base, head, "--")
		if e != nil {
			return Receipt{}, e
		}
		got := []string{}
		for _, p := range strings.Split(diff, "\x00") {
			if p != "" {
				got = append(got, p)
			}
		}
		sort.Strings(got)
		want, e := Paths(paths)
		if e != nil || !equal(got, want) {
			return Receipt{}, fmt.Errorf("UNKNOWN: committed manifest differs")
		}
	} else {
		if err = CheckManifest(root, base, paths); err != nil {
			return Receipt{}, err
		}
		if len(paths) > 0 {
			// Only the exact manifest already checked above is staged. Frozen QA
			// artifacts are delivery paths even when their .gates parent is
			// ignored; Git requires -f again when restaging such tracked paths.
			// A crash may leave a deletion/rename already staged, in which case
			// the old path no longer exists in either the worktree or index. Do
			// not turn that valid partial side effect into an unrecoverable
			// pathspec error on replay.
			stage := make([]string, 0, len(paths))
			for _, path := range paths {
				if _, statErr := os.Lstat(filepath.Join(root, path)); statErr == nil {
					stage = append(stage, path)
					continue
				} else if !os.IsNotExist(statErr) {
					return Receipt{}, statErr
				}
				if _, trackedErr := Git(root, "ls-files", "--error-unmatch", "--", path); trackedErr == nil {
					stage = append(stage, path)
				}
			}
			if len(stage) > 0 {
				args := append([]string{"add", "-f", "--"}, stage...)
				if _, err = Git(root, args...); err != nil {
					return Receipt{}, err
				}
			}
			if _, err = Git(root, "commit", "-m", "Prepare regular candidate\n\nFormal-Gates-Operation: "+id); err != nil {
				return Receipt{}, err
			}
			head, err = Head(root)
			if err != nil {
				return Receipt{}, err
			}
		}
	}
	if _, err = Git(root, "merge-base", "--is-ancestor", base, head); err != nil {
		return Receipt{}, err
	}
	v, err := Snapshot(root, head)
	if err != nil {
		return Receipt{}, err
	}
	return Receipt{OperationID: id, Before: base, After: head, Paths: paths, View: v}, nil
}

// Squash creates a commit from the already frozen tree, then compare-and-swaps
// detached HEAD. commit-tree does not alter the index, so a crash never leaves a
// reset working tree that could accidentally be recommitted with extra paths.
func Squash(root, base, id string) (Receipt, error) {
	head, err := Head(root)
	if err != nil {
		return Receipt{}, err
	}
	dirty, err := Changes(root, head)
	if err != nil || len(dirty) > 0 {
		return Receipt{}, fmt.Errorf("final candidate requires a clean workspace: %v", err)
	}
	if _, err = Git(root, "merge-base", "--is-ancestor", base, head); err != nil {
		return Receipt{}, err
	}
	count, err := Git(root, "rev-list", "--count", base+".."+head)
	if err != nil {
		return Receipt{}, err
	}
	if count != "0" && count != "1" {
		tree, err := Git(root, "rev-parse", head+"^{tree}")
		if err != nil {
			return Receipt{}, err
		}
		target, err := Git(root, "commit-tree", tree, "-p", base, "-m", "Regular validation candidate\n\nFormal-Gates-Operation: "+id)
		if err != nil {
			return Receipt{}, err
		}
		if _, err = Git(root, "update-ref", "HEAD", target, head); err != nil {
			return Receipt{}, err
		}
		head = target
	}
	v, err := Snapshot(root, head)
	return Receipt{OperationID: id, Before: base, After: head, View: v}, err
}

// TestOnly compares every path/mode/blob in the two canonical trees and retains
// all non-test entries, including configuration, dependencies and provider metadata.
func TestOnly(before, after View, oldOwners, newOwners map[string]bool) (Ownership, error) {
	if Digest(before.ProviderMetadata) != Digest(after.ProviderMetadata) {
		return Ownership{Status: "PRODUCTION_CHANGED", Reason: "provider metadata changed"}, nil
	}
	old := map[string]Entry{}
	now := map[string]Entry{}
	for _, e := range before.Entries {
		old[e.Path] = e
	}
	for _, e := range after.Entries {
		now[e.Path] = e
	}
	changed := []string{}
	production := []Entry{}
	for p, e := range old {
		n, ok := now[p]
		if !ok || n != e {
			changed = append(changed, p)
			if !oldOwners[p] || (ok && !newOwners[p]) {
				return Ownership{Status: "PRODUCTION_CHANGED", Paths: changed, Reason: p}, nil
			}
		}
		if !oldOwners[p] {
			production = append(production, e)
		}
	}
	for p := range now {
		if _, ok := old[p]; !ok {
			changed = append(changed, p)
			if !newOwners[p] {
				return Ownership{Status: "PRODUCTION_CHANGED", Paths: changed, Reason: p}, nil
			}
		}
	}
	sort.Strings(changed)
	sort.Slice(production, func(i, j int) bool { return production[i].Path < production[j].Path })
	return Ownership{Status: "TEST_ONLY", Paths: changed, ProductionDigest: Digest(production)}, nil
}

// WorkingView observes unstaged contents without modifying the Git index. It is
// used before whitebox tracking, so a failed ownership proof has zero VCS writes.
func WorkingView(root, base string) (View, error) {
	v, err := Snapshot(root, base)
	if err != nil {
		return View{}, err
	}
	entries := map[string]Entry{}
	for _, e := range v.Entries {
		entries[e.Path] = e
	}
	changed, err := Changes(root, base)
	if err != nil {
		return View{}, err
	}
	for _, p := range changed {
		path := filepath.Join(root, p)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			delete(entries, p)
			continue
		}
		if err != nil {
			return View{}, err
		}
		mode := "100644"
		var data []byte
		if info.Mode()&os.ModeSymlink != 0 {
			mode = "120000"
			target, e := os.Readlink(path)
			err = e
			data = []byte(target)
		} else {
			if !info.Mode().IsRegular() {
				return View{}, fmt.Errorf("UNKNOWN: non-file Git input %s", p)
			}
			if info.Mode()&0111 != 0 {
				mode = "100755"
			}
			data, err = os.ReadFile(path)
		}
		if err != nil {
			return View{}, err
		}
		cmd := exec.Command("git", "-C", root, "hash-object", "--path="+p, "--stdin")
		cmd.Stdin = bytes.NewReader(data)
		out, err := cmd.Output()
		if err != nil {
			return View{}, err
		}
		entries[p] = Entry{Path: p, Mode: mode, Type: "blob", Object: strings.TrimSpace(string(out))}
	}
	v.Entries = nil
	for _, e := range entries {
		v.Entries = append(v.Entries, e)
	}
	sort.Slice(v.Entries, func(i, j int) bool { return v.Entries[i].Path < v.Entries[j].Path })
	v.Identity = "WORKING"
	v.Tree = ""
	v.Digest = Digest(struct {
		Entries  []Entry
		Metadata map[string]string
	}{v.Entries, v.ProviderMetadata})
	return v, nil
}
