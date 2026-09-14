package gitprep

import (
	"fmt"
	"os/exec"
)

// RestoreArtifacts applies a bound manifest to the current candidate. Validate
// every path before writing: divergent edits in a shared file must be resolved
// explicitly, never silently overwrite another worker's accepted changes.
func RestoreArtifacts(root, base, source string, manifest []string) error {
	paths, err := Paths(manifest)
	if err != nil {
		return err
	}
	original, err := Snapshot(root, base)
	if err != nil {
		return err
	}
	desired, err := Snapshot(root, source)
	if err != nil {
		return err
	}
	head, err := Head(root)
	if err != nil {
		return err
	}
	current, err := WorkingView(root, head)
	if err != nil {
		return err
	}
	entries := func(v View) map[string]Entry {
		m := map[string]Entry{}
		for _, e := range v.Entries {
			m[e.Path] = e
		}
		return m
	}
	old, want, now := entries(original), entries(desired), entries(current)
	selected := []string{}
	for _, path := range paths {
		if now[path] != old[path] && now[path] != want[path] {
			return fmt.Errorf("ARTIFACT_CONFLICT: %s changed in the target; reconcile the overlapping changes", path)
		}
		_, present := want[path]
		tracked := exec.Command("git", "-C", root, "ls-files", "--error-unmatch", "--", path).Run() == nil
		// A replay may already have removed a deleted/renamed source from the index.
		if present || tracked {
			selected = append(selected, path)
		}
	}
	if len(selected) == 0 {
		return nil
	}
	args := append([]string{"-C", root, "restore", "--source=" + source, "--staged", "--worktree", "--"}, selected...)
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("restore bound artifacts: %w: %s", err, out)
	}
	return nil
}
