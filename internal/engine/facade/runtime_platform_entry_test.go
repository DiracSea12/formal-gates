package facade

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/host"
)

// Exercise the public lifecycle, not just the adapter's parser/model helper.
// Native execution is the same local fictional process used by the upgrade test.
// No real model is called and no production run or review evidence is modified.
func TestNativePlatformPublicEntryAndRestart(t *testing.T) {
	suffix := strconv.FormatUint(fixtureRegistrationCounter.Add(1), 10)
	a := &fixtureRuntime{id: "public-fixture-" + suffix, executableOption: "publicFixtureExecutable" + suffix, stateOption: "publicFixtureState" + suffix, serverVersion: 2, adapterVersion: 2}
	descriptor := a.descriptor()
	descriptor.Aliases = []string{a.id + "-alias"}
	if err := registerNativePlatform(descriptor, a); err != nil {
		t.Fatal(err)
	}
	if err := registerNativePlatform(descriptor, a); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	if _, err := runtimeAdapter(descriptor.Aliases[0]); err == nil {
		t.Fatal("installation alias admitted as a different execution provider")
	}
	missingID := "incomplete-" + a.id
	if err := registerNativePlatform(&host.Descriptor{ID: missingID}, a); err == nil {
		t.Fatal("native evidence capabilities were not checked")
	}
	if _, err := host.Lookup(missingID); err == nil {
		t.Fatal("failed registration changed the registry")
	}

	f := startPlatformFixture(t, a)
	x := facadeExecution(t, f, "product-review")
	interrupted := errors.New("local fixture interruption after native completion")
	f.fault = func(point persistence.FaultPoint) error {
		if point == persistence.FaultExecuteAfter {
			return interrupted
		}
		return nil
	}
	if _, err := f.ExecuteNative(x.Claim.ActionID, false); !errors.Is(err, interrupted) {
		t.Fatalf("expected observation boundary interruption, got %v", err)
	}
	f.fault = nil
	journal := filepath.Join(f.codexJob(x.Claim.ActionID), "stdout.jsonl")
	original, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	f, err = Open(f.root, "fixture-public")
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.ExecuteNative(x.Claim.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	done := run.Executions[x.Claim.ActionID]
	if !done.Finished || done.Identity != fixtureSessionID(x.Claim.ActionID) || done.Source == nil || done.Source.Status != "STOPPED" {
		t.Fatalf("public entry did not bind/collect the native execution: %+v", done)
	}
	usage := run.Cost.Dispatches[x.Claim.ActionID]
	if usage.Source != cost.SourceTranscript || usage.TotalInputTokens != 11 || usage.OutputTokens != 3 {
		t.Fatalf("public source settlement did not use the platform parser: %+v", usage)
	}
	collected, err := os.ReadFile(done.Source.TranscriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(collected, original) {
		t.Fatal("recovery repeated or changed native output")
	}
	beforeRevision := run.Revision
	if _, err := f.ExecuteNative(x.Claim.ActionID, false); err == nil {
		t.Fatal("completed action ran again")
	}
	after, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != beforeRevision {
		t.Fatal("duplicate action changed workflow state")
	}
	afterJournal, err := os.ReadFile(journal)
	if err != nil || !bytes.Equal(original, afterJournal) {
		t.Fatal("duplicate action launched another native process", err)
	}
	t.Log("one registration: public intake, capability capture, native process, persisted identity, restart collection and cost settlement; no model calls")
}

func TestNativeRoleAccessSharedPolicy(t *testing.T) {
	for _, tc := range []struct {
		step   string
		access nativeAccess
	}{
		{"product-review", nativeTextOnly}, {"start-readiness", nativeTextOnly},
		{"blackbox-execution", nativeOperationOnly}, {"whitebox-execution", nativeOperationOnly},
		{"development-worker", nativeWorkspaceWrite}, {"repair-worker", nativeWorkspaceWrite}, {"whitebox-design", nativeWorkspaceWrite},
		{"blackbox-design", nativeReadOnly}, {"blackbox-review", nativeReadOnly}, {"whitebox-review", nativeReadOnly},
		{"complexity-gate", nativeReadOnly}, {"implementation-quality-gate", nativeReadOnly},
	} {
		t.Run(tc.step, func(t *testing.T) {
			if got := nativeRoleAccess(tc.step); got != tc.access {
				t.Fatalf("role access %v != %v", got, tc.access)
			}
			command := exec.Command("unused", "--tools", "")
			workBuddyRoleTools(command, tc.step)
			tools := []string{command.Args[2], strings.Join(zcodeRoleTools(tc.step, false), ","), strings.Join(dshRoleTools(tc.step, false), ",")}
			for _, list := range tools {
				if strings.Contains(list, "Bash") != (tc.access == nativeWorkspaceWrite) {
					t.Fatalf("authoring privilege differs: %s", list)
				}
				if strings.Contains(list, "Read") != (tc.access == nativeWorkspaceWrite || tc.access == nativeReadOnly) {
					t.Fatalf("reader privilege differs: %s", list)
				}
			}
			if len(zcodeRoleTools(tc.step, true)) != 0 || len(dshRoleTools(tc.step, true)) != 0 {
				t.Fatal("coverage-only pass leaked tools")
			}
		})
	}
}

func startPlatformFixture(t *testing.T, a *fixtureRuntime) *Facade {
	t.Helper()
	root := regularFixtureRoot(t)
	writePreparationFile(t, root, "requirements.md", b1Document)
	writePreparationFile(t, root, ".gitignore", ".gates/\n")
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "fixture@example.invalid"}, {"config", "user.name", "Local platform fixture"}, {"add", "requirements.md", ".gitignore"}, {"-c", "core.hooksPath=/dev/null", "commit", "-qm", "Local fixture requirement"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %s: %v", out, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".gates"), 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(map[string]string{a.executableOption: executable, a.stateOption: filepath.Join(root, "native-session")})
	if err != nil {
		t.Fatal(err)
	}
	writePreparationFile(t, root, ".gates/config.json", string(config))
	packageRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, Request: StartRequest{RunID: "fixture-public", Provider: a.id, RequirementPath: "requirements.md"}, Admission: &Admission{PackageDigest: "sha256:test", InstalledTargetIdentity: "test-target"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "confirm-requirement"})
	facadeChoice(t, f, protocol.RegularRequest{Action: "mode", Mode: "regular"})
	return f
}

func TestNativePlatformAbortRetainsStagedSource(t *testing.T) {
	suffix := strconv.FormatUint(fixtureRegistrationCounter.Add(1), 10)
	a := &fixtureRuntime{id: "abort-fixture-" + suffix, executableOption: "abortExecutable" + suffix, stateOption: "abortState" + suffix, serverVersion: 1, adapterVersion: 1}
	if err := registerNativePlatform(a.descriptor(), a); err != nil {
		t.Fatal(err)
	}
	f := startPlatformFixture(t, a)
	x := facadeExecution(t, f, "product-review")
	interrupted := errors.New("local interruption before final collection")
	f.fault = func(point persistence.FaultPoint) error {
		if point == persistence.FaultExecuteAfter {
			return interrupted
		}
		return nil
	}
	if _, err := f.ExecuteNative(x.Claim.ActionID, false); !errors.Is(err, interrupted) {
		t.Fatal(err)
	}
	f.fault = nil
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	current := run.Executions[x.Claim.ActionID]
	// Source copying can precede result/source admission. Its append-once marker
	// is native process metadata and must survive a user abort's stopped update.
	source, err := a.source(current.Identity, f.codexJob(x.Claim.ActionID), current.Claim.Context)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "abort", Reason: "Local fixture: user abort after native evidence staging"})
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(source)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err == nil && !bytes.Equal(before, after) {
		t.Fatal("abort discarded append-once metadata and duplicated native evidence")
	}
	if run.Regular.Terminal != "ABORTED" {
		t.Fatalf("abort did not complete: %s", run.Regular.Terminal)
	}
	// Terminal cleanup may remove the native source. The retained cost receipt
	// must still report precisely the one observed invocation, not two copies.
	usage := run.Cost.Dispatches[x.Claim.ActionID]
	if usage.Source != cost.SourceTranscript || usage.TotalInputTokens != 11 || usage.OutputTokens != 3 {
		t.Fatalf("abort source counted twice: %+v", usage)
	}
}
