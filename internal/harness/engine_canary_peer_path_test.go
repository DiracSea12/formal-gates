//go:build !windows

package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The peer's exact launch and session contract use the native resolved workspace.
func TestCanaryLocalPeerUsesResolvedWorkspace(t *testing.T) {
	s := newZcodeScenario(t, zcodeSuccessSteps, nil)
	alias := filepath.Join(t.TempDir(), "workspace alias")
	if err := os.Symlink(s.workspace, alias); err != nil {
		t.Fatal(err)
	}
	s.request["workspace"] = alias
	c := &engineCanary{t: t, nativeHost: canaryProvider, home: resolveNativePath(t.TempDir()), binding: objectOrEmpty(s.request["binding"])}
	writeCanaryHostFixture(t, c.home, "peer-provider")
	environment := map[string]any{}
	for key, value := range c.hostEnvironment() {
		environment[key] = value
	}
	objectOrEmpty(s.request["binding"])["environment"] = environment
	e := &canaryLocalExecutor{t: t, canary: c}
	if err := e.prepareNativePeer(s.request, s.directory, map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}
	var peer peerScript
	if err := json.Unmarshal(readTestFileBytes(t, filepath.Join(s.directory, "peer.json")), &peer); err != nil {
		t.Fatal(err)
	}
	workspace := resolveNativePath(alias)
	launch := objectOrEmpty(getOf(s.request["binding"], "launch"))
	if !slices.Equal(peer.Args, []string{textOf(launch["script"]), "--cwd", workspace, "app-server"}) || peer.Workspace != workspace {
		t.Fatalf("peer differs from native resolved launch: %q cwd=%q", peer.Args, peer.Workspace)
	}
	if textOf(s.request["workspace"]) != alias {
		t.Fatal("fixture rewrote the issued workspace")
	}
	found := false
	for _, step := range peer.Steps {
		if step.On != `"method":"session/create"` {
			continue
		}
		found = true
		if !valuesEqual(getOf(step.Params, "workspace"), map[string]any{"workspacePath": workspace, "workspaceKey": workspace}) {
			t.Fatalf("session identity differs from native resolved request: %#v", step.Params)
		}
	}
	if !found {
		t.Fatal("peer omitted session/create contract")
	}
}
