//go:build !windows

package harness

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"formal-gates/internal/engine/vcs"
)

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
func (c *engineCanary) initFixtureRepository(repositoryRoot ...string) {
	c.t.Helper()
	c.sourceBuildEnv = os.Environ()
	nativeRoot := func(name string) string {
		if len(repositoryRoot) == 0 {
			return c.t.TempDir()
		}
		return filepath.Join(repositoryRoot[0], name)
	}
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
	// Native tools and the installer share the host's private environment. Read
	// Go's original cache values and resolve the runtime before HOME changes.
	var cache struct{ GOMODCACHE, GOCACHE string }
	if err := json.Unmarshal([]byte(c.fixtureCommand(c.project, "go", nil, "env", "-json", "GOMODCACHE", "GOCACHE")), &cache); err != nil {
		c.fail("read configured Go caches: %v", err)
	}
	c.t.Setenv("GOMODCACHE", cache.GOMODCACHE)
	c.t.Setenv("GOCACHE", cache.GOCACHE)
	for key, value := range c.hostEnvironment() {
		c.t.Setenv(key, value)
	}

	c.t.Setenv("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -buildvcs=false"))
	c.fixtureCommand(c.project, "go", nil, "env", "GOFLAGS")
	if c.vcs.Name() == "svn" {
		repository := filepath.Join(nativeRoot("svn"), "repository")
		if err := os.MkdirAll(filepath.Dir(repository), 0700); err != nil {
			c.fail("private SVN repository directory: %v", err)
		}
		c.fixtureCommand("", "svnadmin", nil, "create", repository)
		repositoryURL := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(repository), "/")}).String()
		c.fixtureCommand("", "svn", nil, "mkdir", repositoryURL+"/trunk", "-m", "fixture trunk")
		c.fixtureCommand("", "svn", nil, "checkout", repositoryURL+"/trunk", c.project)
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
	pidFile := filepath.Join(c.jobs, "p4d.pid")
	serverRoot := nativeRoot("p4")
	if err := os.MkdirAll(serverRoot, 0700); err != nil {
		c.fail("private P4 server directory: %v", err)
	}
	server := exec.Command("p4d", "--pid-file="+pidFile, "-r", serverRoot, "-p", address)
	server.Stdout, server.Stderr = log, log
	if err := server.Start(); err != nil {
		log.Close()
		c.fail("private p4d start: %v", err)
	}
	c.t.Cleanup(func() { server.Process.Kill(); server.Wait(); log.Close(); _ = os.Remove(pidFile) })
	if err := writeAtomicJSON(filepath.Join(c.jobs, "p4d-bootstrap.json"), map[string]any{"argv": server.Args, "pid": server.Process.Pid, "serverRoot": serverRoot, "address": address, "client": "main", "user": "fixture", "workingCopy": c.project}); err != nil {
		c.fail("private P4 server evidence: %v", err)
	}
	ready := false
	for i := 0; i < 50; i++ {
		if _, err = (vcs.ExecRunner{}).Run("", "p4", "info"); err == nil {
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		body, readErr := os.ReadFile(log.Name())
		c.fail("private p4d did not start: address=%s pid=%d probe=%v log=%q readError=%v\n%s", address, server.Process.Pid, err, log.Name(), readErr, body)
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
