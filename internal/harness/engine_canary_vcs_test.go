//go:build !windows

package harness

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/vcs"
)

// The same installed workflow consumes real SVN and P4 workspaces. Only host
// transport is synthetic; candidate identities and all native effects are real.
func TestCanaryInstalledNativeFlows(t *testing.T) {
	for _, provider := range []string{"svn", "p4"} {
		t.Run(provider, func(t *testing.T) {
			for _, split := range []bool{true, false} {
				name := "split"
				if !split {
					name = "no-split"
				}
				t.Run(name, func(t *testing.T) { runCanaryInstalledFlow(t, provider, split) })
			}
		})
	}
}

func TestCanaryInstalledNativeTransportFailureKeepsIndependentFrontier(t *testing.T) {
	for _, provider := range []string{"svn", "p4"} {
		t.Run(provider, func(t *testing.T) {
			for _, mode := range []string{"transient", "permanent"} {
				t.Run(mode, func(t *testing.T) { runCanaryInstalledTransportFailure(t, provider, mode) })
			}
		})
	}
}

// Every cell is separately selectable in a test binary. The existing live
// authorization owner checks each call; the ordinary suite spends nothing.
func TestEngineNativeCanary(t *testing.T) {
	if os.Getenv("FORMAL_GATES_ENGINE_CANARY") != "1" {
		t.Skip("engine canary: set FORMAL_GATES_ENGINE_CANARY=1 (real host calls, real spend)")
	}
	for _, provider := range []string{"svn", "p4"} {
		t.Run(provider, func(t *testing.T) {
			t.Run("split", func(t *testing.T) { runEngineSplitCanary(t, provider) })
			t.Run("no-split", func(t *testing.T) { runEngineNoSplitCanary(t, provider) })
		})
	}
}

func (c *engineCanary) fixtureCommand(dir, command string, input []byte, args ...string) string {
	c.t.Helper()
	started := time.Now()
	body, err := (vcs.ExecRunner{}).RunRaw(dir, command, input, args...)
	log, logErr := os.OpenFile(filepath.Join(c.jobs, "fixture-native-commands.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if logErr != nil {
		c.fail("native fixture log: %v", logErr)
	}
	_, writeErr := fmt.Fprintf(log, "%s %q dir=%q stdin=%q elapsed=%s error=%v\nstdout=%q\n", command, args, dir, input, time.Since(started), err, body)
	closeErr := log.Close()
	if writeErr != nil || closeErr != nil || err != nil {
		c.fail("native fixture %s %v: %v %v %v", command, args, err, writeErr, closeErr)
	}
	return strings.TrimSpace(string(body))
}

// Bootstrap owns only the private repository/server and its initial workspace.
// Provider owns snapshot, workspace, promotion and cleanup semantics.
func (c *engineCanary) initFixtureRepository() {
	c.t.Helper()
	if c.vcs == nil {
		c.vcs = vcs.NewNative(c.project, "git", nil)
	}
	if c.vcs.Name() == "git" {
		c.fixtureCommand(c.project, "git", nil, "init", "--initial-branch=main", "--quiet")
		for _, config := range [][2]string{{"user.name", "engine-canary"}, {"user.email", "engine-canary@localhost"}, {"commit.gpgsign", "false"}} {
			c.fixtureCommand(c.project, "git", nil, "config", "--local", config[0], config[1])
		}
		return
	}
	// Native tools read configuration under HOME. Keep that lookup private while
	// preserving the existing Go cache used by the real installer and probes.
	cache := strings.Fields(c.fixtureCommand(c.project, "go", nil, "env", "GOMODCACHE", "GOCACHE"))
	c.t.Setenv("GOMODCACHE", cache[0])
	c.t.Setenv("GOCACHE", cache[1])
	c.t.Setenv("HOME", c.home)
	// Nested SVN working copies make Go stamping ambiguous; native snapshots
	// are verified by Provider, while these builds check fixture behavior.
	c.t.Setenv("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -buildvcs=false"))
	c.fixtureCommand(c.project, "go", nil, "env", "GOFLAGS")
	if c.vcs.Name() == "svn" {
		repository := filepath.Join(c.t.TempDir(), "repository")
		c.fixtureCommand("", "svnadmin", nil, "create", repository)
		c.fixtureCommand("", "svn", nil, "mkdir", "file://"+repository+"/trunk", "-m", "fixture trunk")
		c.fixtureCommand("", "svn", nil, "checkout", "file://"+repository+"/trunk", c.project)
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		c.fail("private p4d address: %v", err)
	}
	address := listener.Addr().String()
	listener.Close()
	for key, value := range map[string]string{"P4PORT": address, "P4USER": "fixture", "P4CLIENT": "main", "P4CONFIG": ".canary-p4-config", "P4ENVIRO": filepath.Join(c.home, "p4enviro"), "P4TICKETS": filepath.Join(c.home, "p4tickets"), "P4TRUST": filepath.Join(c.home, "p4trust"), "P4PASSWD": ""} {
		c.t.Setenv(key, value)
	}
	log, err := os.Create(filepath.Join(c.jobs, "p4d.log"))
	if err != nil {
		c.fail("private p4d log: %v", err)
	}
	server := exec.Command("p4d", "-r", c.t.TempDir(), "-p", address)
	server.Stdout, server.Stderr = log, log
	if err := server.Start(); err != nil {
		log.Close()
		c.fail("private p4d start: %v", err)
	}
	c.t.Cleanup(func() { server.Process.Kill(); server.Wait(); log.Close() })
	ready := false
	for i := 0; i < 50; i++ {
		if _, err := (vcs.ExecRunner{}).Run("", "p4", "info"); err == nil {
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		c.fail("private p4d did not start")
	}
	spec := "Client: main\nOwner: fixture\nRoot: " + c.project + "\nLineEnd: unix\nView:\n\t//depot/main/... //main/...\n"
	c.fixtureCommand(c.project, "p4", []byte(spec), "client", "-i")
}

func (c *engineCanary) commitFixture(paths []string, message string) string {
	c.t.Helper()
	if err := c.vcs.Track(paths); err != nil {
		c.fail("track fixture: %v", err)
	}
	if err := c.vcs.Commit(message); err != nil {
		c.fail("commit fixture: %v", err)
	}
	identity, err := c.vcs.Resolve()
	if err != nil {
		c.fail("fixture snapshot: %v", err)
	}
	return identity
}

// Both terminal consumers check the same complete native promotion and finite
// resource registry. Path existence alone does not prove native cleanup.
func (c *engineCanary) assertNativeTerminal(base string, final facade.Run, paths []string) (string, string) {
	c.t.Helper()
	head, err := c.vcs.Resolve()
	if err != nil {
		c.fail("final native snapshot: %v", err)
	}
	if err := c.vcs.Verify(head); err != nil {
		c.fail("final snapshot is not reproducible: %v", err)
	}
	tree, err := c.vcs.TreeDigest(head)
	if err != nil {
		c.fail("final native digest: %v", err)
	}
	terminal := final.Termination
	if terminal == nil || !terminal.ResourcesCleaned() || terminal.CandidatePromotion == nil {
		c.fail("terminal resource/promotion proof missing")
	}
	proof := terminal.CandidatePromotion
	if proof.Provider != c.vcs.Name() || proof.Final != head || proof.FinalDigest != tree || proof.SourceDigest != tree || final.PromotedSnapshot != head {
		c.fail("promotion does not bind the actual native snapshot and full tree")
	}
	validated, err := c.vcs.TreeEntries(proof.Source)
	if err != nil {
		c.fail("validated native candidate cannot be reproduced: %v", err)
	}
	delivered, err := c.vcs.TreeEntries(head)
	if err != nil || !canaryValuesEqual(validated, delivered) {
		c.fail("promotion changed complete native bytes or metadata: %v", err)
	}
	count, err := c.vcs.CommitCount(base, head)
	if err != nil || count != 1 {
		c.fail("final range holds %d commits, want one organized commit: %v", count, err)
	}
	status, err := c.vcs.Status()
	if err != nil || status != "" {
		c.fail("final native workspace is not clean: %s %v", status, err)
	}
	changed, err := c.vcs.DiffNames(base, head)
	if err != nil {
		c.fail("final native range: %v", err)
	}
	for _, path := range paths {
		if !slices.Contains(changed, path) {
			c.fail("final candidate misses %s: %v", path, changed)
		}
	}
	if c.vcs.Name() == "git" {
		trees, err := c.vcs.Worktrees()
		if err != nil || len(trees) != 1 {
			c.fail("native Git worktree residuals: %+v %v", trees, err)
		}
	}
	for _, resource := range terminal.NativeResources {
		if resource.Path == "" || resource.Identity == "" || resource.Owner == "" {
			c.fail("native resource lost issued path/identity/owner: %+v", resource)
		}
		if _, err := os.Stat(resource.Path); !os.IsNotExist(err) {
			c.fail("native resource directory remains: %s %v", resource.Path, err)
		}
		absent, err := c.vcs.WorkspaceAbsent(resource.Path, resource.Handle)
		if err != nil || !absent {
			c.fail("native resource registration remains: %+v %v", resource, err)
		}
		if c.vcs.Name() == "git" {
			ref := c.fixtureCommand(c.project, "git", nil, "for-each-ref", "--format=%(objectname)", resource.Ref)
			if ref != "" {
				c.fail("native resource ref remains: %s", ref)
			}
		}
	}
	return head, tree
}

// The integration-owned flag gap is independent of the two function batches.
// Merge QA and merge-gate observe it before the existing master repair closes it.
func (c *engineCanary) splitFlagContract() bool {
	return c.split && c.vcs.Name() != "git"
}
