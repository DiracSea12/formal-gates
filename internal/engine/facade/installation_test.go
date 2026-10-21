package facade

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"formal-gates/internal/engine/decision"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/engine/vcs"
)

// Installation fencing follows the owning run through retained objects,
// side effects, receipt publication and interrupted persistence transactions.
// Registry replacement is fixture setup; the public installed/reinstall path
// is exercised by the acceptance package.
func advanceInstallation(t *testing.T, admitted Admission) Admission {
	t.Helper()
	release, err := coordination.AcquirePathWaiting(admitted.RegistryPath+".lock", "install")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	data, err := os.ReadFile(admitted.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	next := admitted
	next.Generation++
	next.Lease = fmt.Sprintf("lease-%d", next.Generation)
	next.Token = fmt.Sprintf("token-%d", next.Generation)
	found := false
	for _, value := range doc["records"].([]any) {
		record := value.(map[string]any)
		if record["id"] == admitted.InstalledTargetIdentity {
			record["generation"], record["lease"], record["token"] = next.Generation, next.Lease, next.Token
			found = true
		}
	}
	if !found {
		t.Fatal("fixture lost the owning registry record")
	}
	doc["epoch"] = doc["epoch"].(float64) + 1
	if err := writeJSONAtomic(admitted.RegistryPath, doc); err != nil {
		t.Fatal(err)
	}
	return next
}

func installationFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestStartRecoveryKeepsAdmissionThroughRootAlias(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	verifiedStartFixture(t, root)
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	admission := testAdmission(t, root, "target", "sha256:package", DefaultProvider)
	runID := "root-alias"
	if err := os.MkdirAll(filepath.Join(root, EngineNamespace, runID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(startIntentPath(root, runID), startIntent{RunID: runID, Admission: *admission}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Start(StartOptions{Root: alias, VCSRunner: testkit.NewFrozenGitRepo(), Request: StartRequest{BaseSnapshot: testkit.FrozenSnapshot, RunID: runID, IntakeConfirmationReceipt: testReceipt()}, Admission: admission}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.admission.RegistryPath != admission.RegistryPath || reopened.admission.Token != admission.Token {
		t.Fatal("root alias changed the owning registry/token")
	}
}

func TestRetainedInstallationKeepsNamespaceAndReadOnlyReplay(t *testing.T) {
	d := startValidationDriver(t, nil)
	event, err := protocol.NewRequestEvent("stop-request", protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := d.f.Submit(event)
	if err != nil {
		t.Fatal(err)
	}
	before := installationFiles(t, d.f.root)
	// A second namespace can contain the old exact token. Changing HOME must
	// neither adopt it nor replace the request's immutable registry path.
	home := t.TempDir()
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	for _, suffix := range []string{"", ".bootstrap.json"} {
		data, err := os.ReadFile(d.f.admission.RegistryPath + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(registry), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(registry+suffix, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	advanceInstallation(t, d.f.admission)
	reopened, err := Open(d.f.root, d.f.runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, read := range []func() (Run, error){reopened.Show, reopened.Status} {
		if _, err := read(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := reopened.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Freshness(string(event.ID)); err != nil {
		t.Fatal(err)
	}
	_, _ = reopened.DevelopmentPlan(protocol.RouteDecision{})
	if _, err := Diagnose(filepath.Join(d.f.stateDir, "state.json"), "", ""); err != nil {
		t.Fatal(err)
	}
	replayed, _, err := reopened.Submit(event)
	if err != nil || !reflect.DeepEqual(replayed, accepted) {
		t.Fatalf("stable replay=%+v err=%v", replayed, err)
	}
	next, _ := protocol.NewRequestEvent("another-stop", protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
	if _, _, err := reopened.Submit(next); !errors.Is(err, errStaleFencingToken) {
		t.Fatalf("stale new event=%v", err)
	}
	if after := installationFiles(t, d.f.root); !reflect.DeepEqual(before, after) {
		t.Fatal("read/replay/stale submit changed run bytes")
	}
}

func TestCandidateWriterRequiresCompleteAdmission(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	verifiedStartFixture(t, root)
	a := testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)
	for _, missing := range []string{"namespace", "generation", "lease", "token", "target", "provider"} {
		t.Run(missing, func(t *testing.T) {
			incomplete := *a
			switch missing {
			case "namespace":
				incomplete.RegistryPath = ""
			case "generation":
				incomplete.Generation = 0
			case "lease":
				incomplete.Lease = ""
			case "token":
				incomplete.Token = ""
			case "target":
				incomplete.Target = ""
			case "provider":
				incomplete.Provider = ""
			}
			_, _, err := Start(StartOptions{Root: root, VCSRunner: testkit.NewFrozenGitRepo(), Request: StartRequest{BaseSnapshot: testkit.FrozenSnapshot, RunID: "missing-" + missing, IntakeConfirmationReceipt: testReceipt()}, Admission: &incomplete})
			if err == nil || !strings.Contains(err.Error(), UnregisteredInstall) {
				t.Fatalf("incomplete binding=%v", err)
			}
			if _, err := os.Stat(startIntentPath(root, "missing-"+missing)); !os.IsNotExist(err) {
				t.Fatalf("rejected writer left intent: %v", err)
			}
		})
	}
	wrongProvider := *a
	if _, _, err := Start(StartOptions{Root: root, VCSRunner: testkit.NewFrozenGitRepo(), Request: StartRequest{BaseSnapshot: testkit.FrozenSnapshot, RunID: "provider-mismatch", Provider: "zcode", IntakeConfirmationReceipt: testReceipt()}, Admission: &wrongProvider}); err == nil || !strings.Contains(err.Error(), UnregisteredInstall) {
		t.Fatalf("provider mismatch=%v", err)
	}
}

func TestStaleStartRecoveryRetainsItsOriginalPrefix(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	verifiedStartFixture(t, root)
	a := testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)
	id := "interrupted-start"
	dir := filepath.Join(root, EngineNamespace, id)
	if err := writeJSONAtomic(startIntentPath(root, id), startIntent{RunID: id, Admission: *a}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "request.json"), []byte("partial request\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := installationFiles(t, filepath.Join(root, EngineNamespace))
	next := advanceInstallation(t, *a)
	_, _, err := Start(StartOptions{Root: root, VCSRunner: testkit.NewFrozenGitRepo(), Request: StartRequest{BaseSnapshot: testkit.FrozenSnapshot, RunID: id, IntakeConfirmationReceipt: testReceipt()}, Admission: &next})
	if !errors.Is(err, errStaleFencingToken) {
		t.Fatalf("stale prefix recovery=%v", err)
	}
	if after := installationFiles(t, filepath.Join(root, EngineNamespace)); !reflect.DeepEqual(before, after) {
		t.Fatal("new caller cleaned old start prefix")
	}
}

func TestPersistenceRecoveryUsesOwningInstallation(t *testing.T) {
	t.Parallel()
	for _, point := range []persistence.FaultPoint{persistence.FaultIntentBefore, persistence.FaultIntentAfter, persistence.FaultReplaceBefore, persistence.FaultReplaceAfter, persistence.FaultCommitResponseLost} {
		for _, fenced := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fenced=%t", point, fenced), func(t *testing.T) {
				root := t.TempDir()
				verifiedStartFixture(t, root)
				a := testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)
				f, _, err := Start(StartOptions{Root: root, VCSRunner: testkit.NewFrozenGitRepo(), Request: StartRequest{BaseSnapshot: testkit.FrozenSnapshot, RunID: "recovery", IntakeConfirmationReceipt: testReceipt()}, Admission: a})
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				cap := int64(123)
				snapshot.State.TokenCapMaxTokens = &cap
				store, err := persistence.NewStore(f.stateDir, persistence.Config{PackageDigest: f.envelope.PackageDigest, DefinitionSource: f.envelope.DefinitionSource, InstalledTargetIdentity: f.envelope.InstalledTargetIdentity, AcquireAdmission: f.admission.acquire, FaultInjector: func(at persistence.FaultPoint) error {
					if at == point {
						return &persistence.InjectedCrashError{Point: at}
					}
					return nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				fingerprint, _ := (decision.Observation{}).Digest()
				_, err = store.Save(persistence.Transaction{ExpectedRevision: snapshot.Revision, ExpectedFingerprint: fingerprint, CollectFingerprint: (decision.Observation{}).Digest, Content: snapshot.State})
				if !errors.Is(err, persistence.ErrInjectedCrash) {
					t.Fatalf("fault=%v", err)
				}
				before := installationFiles(t, f.stateDir)
				if fenced {
					advanceInstallation(t, f.admission)
				}
				recovery, err := store.Recover()
				if fenced && point != persistence.FaultCommitResponseLost {
					if !errors.Is(err, errStaleFencingToken) {
						t.Fatalf("stale recovery=%+v %v", recovery, err)
					}
					if after := installationFiles(t, f.stateDir); !reflect.DeepEqual(before, after) {
						t.Fatal("stale recovery changed protocol bytes")
					}
					return
				}
				// Response loss leaves no intent/temp, so clean recovery is a read.
				if err != nil {
					t.Fatal(err)
				}
				after, err := f.store.Load()
				if err != nil {
					t.Fatal(err)
				}
				var state protocol.State
				if err := json.Unmarshal(after.Content, &state); err != nil {
					t.Fatal(err)
				}
				if point == persistence.FaultIntentBefore {
					if state.TokenCapMaxTokens != nil || after.Revision != snapshot.Revision {
						t.Fatal("uncommitted temp changed state")
					}
				} else if state.TokenCapMaxTokens == nil || *state.TokenCapMaxTokens != cap || after.Revision != snapshot.Revision+1 {
					t.Fatal("recovery lost committed content/revision")
				}
			})
		}
	}
}

// Public requests preserve interrupted durable input and return the original
// acceptance on replay. Independent input must be read against the recovered
// revision before acceptance.
func TestSubmitPreservesPendingCommitAndReplay(t *testing.T) {
	t.Parallel()
	for _, point := range []persistence.FaultPoint{persistence.FaultIntentAfter, persistence.FaultExecuteAfter, persistence.FaultCommitResponseLost} {
		for _, replay := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/replay=%t", point, replay), func(t *testing.T) {
				root := t.TempDir()
				d := startValidationDriverAtVCS(t, root, DefaultProvider, nil, snapshotRepoCommits(t, root)[0], nil)
				baseline, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				original, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Revise"}, protocol.AskOption{ID: "abort", Label: "Keep"})
				if err != nil {
					t.Fatal(err)
				}
				store, err := persistence.NewStore(d.f.stateDir, persistence.Config{PackageDigest: d.f.envelope.PackageDigest, DefinitionSource: d.f.envelope.DefinitionSource, InstalledTargetIdentity: d.f.envelope.InstalledTargetIdentity, AcquireAdmission: d.f.admission.acquire, FaultInjector: func(at persistence.FaultPoint) error {
					if at == point {
						return &persistence.InjectedCrashError{Point: at}
					}
					return nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				engine, err := protocol.New(store, protocol.Config{NativeRoot: d.f.root, Definition: d.f.definition, Registry: definition.Registry(), Capacity: 16, CostSource: CostSourceBridge(d.f.root, d.f.runID), ClaimDispatch: dispatchClaimBridge(d.f.root, d.f.runID, d.f.admission)})
				if err != nil {
					t.Fatal(err)
				}
				d.f.store, d.f.engine = store, engine
				if _, _, err := d.f.Submit(original); !errors.Is(err, persistence.ErrInjectedCrash) {
					t.Fatalf("interrupted public submit: %v", err)
				}
				statePath := filepath.Join(d.f.stateDir, "state.json")
				if point == persistence.FaultIntentAfter {
					data, err := os.ReadFile(filepath.Join(d.f.stateDir, "state.json.intent"))
					if err != nil {
						t.Fatal(err)
					}
					var intent struct {
						Temp string `json:"temp"`
					}
					if err := json.Unmarshal(data, &intent); err != nil {
						t.Fatal(err)
					}
					statePath = filepath.Join(d.f.stateDir, intent.Temp)
				}
				data, err := os.ReadFile(statePath)
				if err != nil {
					t.Fatal(err)
				}
				var durable struct {
					Revision uint64         `json:"revision"`
					Content  protocol.State `json:"content"`
				}
				if err := json.Unmarshal(data, &durable); err != nil {
					t.Fatal(err)
				}
				if durable.Revision != baseline.Revision+1 {
					t.Fatalf("durable revision=%d baseline=%d", durable.Revision, baseline.Revision)
				}
				originalDigest, err := original.Digest()
				if err != nil {
					t.Fatal(err)
				}
				if record, ok := durable.Content.Events[string(original.ID)]; !ok || record.Digest != originalDigest || record.Acceptance.EventID != string(original.ID) || record.Acceptance.Revision != durable.Revision || durable.Content.PendingAsks[string(original.ID)].Control != protocol.ControlRequestRequirementChange {
					t.Fatalf("durable input lost original request identity/content: %+v", record)
				}
				f, err := Open(d.f.root, d.f.runID)
				if err != nil {
					t.Fatal(err)
				}
				submitted := original
				if !replay {
					submitted, err = protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
					if err != nil {
						t.Fatal(err)
					}
				}
				acceptance, _, err := f.Submit(submitted)
				if point == persistence.FaultIntentAfter {
					var conflict *persistence.RevisionConflictError
					if !errors.As(err, &conflict) || conflict.Expected != baseline.Revision || conflict.Observed != durable.Revision {
						t.Fatalf("stale public submit: %v, want CAS %d/%d", err, baseline.Revision, durable.Revision)
					}
					recovered, loadErr := f.engine.Load()
					if loadErr != nil || recovered.Revision != durable.Revision || !reflect.DeepEqual(recovered.State, &durable.Content) {
						t.Fatalf("stale submit changed original durable state: %+v %v", recovered, loadErr)
					}
					acceptance, _, err = f.Submit(submitted)
				}
				if err != nil {
					t.Fatal(err)
				}
				driven, err := f.Drive()
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				wantRevision := durable.Revision
				if !replay {
					wantRevision++
				}
				if snapshot.Revision != wantRevision || driven.Revision != wantRevision || !reflect.DeepEqual(snapshot.State.Events[string(original.ID)], durable.Content.Events[string(original.ID)]) || !reflect.DeepEqual(snapshot.State.PendingAsks[string(original.ID)], durable.Content.PendingAsks[string(original.ID)]) {
					t.Fatalf("original request/acceptance/ask changed: revision=%d want=%d", snapshot.Revision, wantRevision)
				}
				if replay {
					if !reflect.DeepEqual(acceptance, durable.Content.Events[string(original.ID)].Acceptance) {
						t.Fatalf("replay acceptance changed: %+v", acceptance)
					}
				} else {
					digest, err := submitted.Digest()
					if err != nil {
						t.Fatal(err)
					}
					if record, ok := snapshot.State.Events[string(submitted.ID)]; !ok || record.Digest != digest || !reflect.DeepEqual(record.Acceptance, acceptance) || record.Acceptance.Revision != wantRevision || snapshot.State.PendingAsks[string(submitted.ID)].Control != protocol.ControlAbort {
						t.Fatalf("independent request lost identity/content: %+v", record)
					}
				}
				before := installationFiles(t, f.stateDir)
				if repeated, _, err := f.Submit(submitted); err != nil || !reflect.DeepEqual(repeated, acceptance) {
					t.Fatalf("repeat acceptance: %+v %v", repeated, err)
				}
				if report, err := f.store.Recover(); err != nil || report.Outcome != persistence.RecoveryClean || report.Revision != wantRevision {
					t.Fatalf("repeat recovery: %+v %v", report, err)
				}
				if after := installationFiles(t, f.stateDir); !reflect.DeepEqual(before, after) {
					t.Fatal("repeat submit/recovery changed durable bytes")
				}
			})
		}
	}
}

func TestFencedDispatchClaimDoesNotWriteLifecycleBinding(t *testing.T) {
	t.Parallel()
	d := startValidationDriverAt(t, t.TempDir(), meteredProvider, nil)
	id := ""
	for action := range d.run.PendingActions {
		id = action
		break
	}
	if id == "" {
		t.Fatal("no issued dispatch")
	}
	before := installationFiles(t, d.f.root)
	advanceInstallation(t, d.f.admission)
	event, err := protocol.NewSpawnReceiptEvent("spawn-stale", id, meteredProvider, "agent-stale", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.f.Submit(event); !errors.Is(err, errStaleFencingToken) {
		t.Fatalf("stale spawn=%v", err)
	}
	if after := installationFiles(t, d.f.root); !reflect.DeepEqual(before, after) {
		t.Fatal("stale dispatch wrote sidecar or state")
	}
}

func TestNewInstallationStartsWithoutWritingFencedOwnerPeer(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	transcript := filepath.Join(t.TempDir(), "owner.jsonl")
	writeTranscript(t, transcript, []string{intervalBaseLine})
	old := startIntervalRun(t, root, "old-owner", transcript)
	before := installationFiles(t, old.stateDir)
	next := advanceInstallation(t, old.admission)
	f, _, err := Start(StartOptions{Root: root, VCSRunner: testkit.NewFrozenGitRepo(), Owner: OwnerBinding{Transcript: transcript, Provider: meteredProvider}, Request: StartRequest{BaseSnapshot: testkit.FrozenSnapshot, RunID: "new-owner", IntakeConfirmationReceipt: testReceipt()}, Admission: &next})
	if err != nil {
		t.Fatalf("new installation start=%v", err)
	}
	writeTranscript(t, transcript, []string{intervalBaseLine, intervalDuringLine})
	run, err := abortVerifiedRun(t, f)
	if err != nil {
		t.Fatal(err)
	}
	if run.Cost == nil || run.Cost.Owner == nil || run.Cost.Owner.Source != "unavailable" || run.Cost.Owner.TotalInputTokens != 0 {
		t.Fatalf("overlap owner=%+v", run.Cost.Owner)
	}
	if after := installationFiles(t, old.stateDir); !reflect.DeepEqual(before, after) {
		t.Fatal("new installation wrote fenced peer")
	}
}

func TestFencedTerminalWindowsRetainEvidenceAndAllowPureReplay(t *testing.T) {
	t.Parallel()
	for _, window := range []string{"intent", "cleanup", "summary", "remove-intent", "end-lifecycle", "complete"} {
		t.Run(window, func(t *testing.T) {
			root := t.TempDir()
			verifiedStartFixture(t, root)
			f, _, err := Start(StartOptions{Root: root, VCSRunner: testkit.NewFrozenGitRepo(), Request: StartRequest{RunID: "terminal", BaseSnapshot: testkit.FrozenSnapshot, IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
			if err != nil {
				t.Fatal(err)
			}
			// 决定 abort 并让引擎推进到终态：得到未发布的权威终态投影（状
			// 态文件仍在盘上），终态发布窗口由本测试按序手工展开。
			decideAbort(t, f)
			if _, _, err := f.engine.Drive(protocol.Observation{}); err != nil {
				t.Fatal(err)
			}
			run, err := f.project(nil)
			if err != nil {
				t.Fatal(err)
			}
			summary := summaryForRun(run, ownerInterval{})
			if window != "intent" {
				if err := f.admission.write(func() error { return writeJSONAtomic(terminalIntentPath(f.stateDir), summary) }); err != nil {
					t.Fatal(err)
				}
			}
			if window == "summary" || window == "remove-intent" || window == "end-lifecycle" || window == "complete" {
				if err := f.store.CleanupTerminal(); err != nil {
					t.Fatal(err)
				}
			}
			if window == "remove-intent" || window == "end-lifecycle" || window == "complete" {
				if err := f.writeSummaryValue(summary); err != nil {
					t.Fatal(err)
				}
			}
			if window == "end-lifecycle" || window == "complete" {
				if err := f.admission.write(func() error { return os.Remove(terminalIntentPath(f.stateDir)) }); err != nil {
					t.Fatal(err)
				}
			}
			if window == "complete" {
				if err := f.endLifecycle(); err != nil {
					t.Fatal(err)
				}
			}
			before := installationFiles(t, root)
			advanceInstallation(t, f.admission)
			if window == "intent" {
				err = f.finalizeTerminal(run, ownerInterval{})
			} else {
				_, err = f.Drive()
			}
			if window == "complete" {
				if err != nil {
					t.Fatalf("pure complete replay=%v", err)
				}
			} else if !errors.Is(err, errStaleFencingToken) {
				t.Fatalf("stale terminal window=%v", err)
			}
			if after := installationFiles(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("fenced terminal retry changed state/evidence/lifecycle")
			}
		})
	}
}

// TestInstallationSubprocess uses the same Store admission as the facade. The
// command protocol is fixture-only; it controls a real cross-process lock window.
func TestInstallationSubprocess(t *testing.T) {
	root := os.Getenv("INSTALLATION_TEST_ROOT")
	if root == "" {
		t.Skip("subprocess entry only")
	}
	f, err := Open(root, "process-window")
	if err != nil {
		t.Fatal(err)
	}
	role := os.Getenv("INSTALLATION_TEST_ROLE")
	if role == "holder" {
		release, err := f.admission.acquire()
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		fmt.Println("registry-held")
		_, _ = io.ReadAll(os.Stdin)
		os.Exit(0) // Crash releases the kernel descriptor; no application cleanup.
	}
	snapshot, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	cap, err := strconv.ParseInt(os.Getenv("INSTALLATION_TEST_CAP"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.State.TokenCapMaxTokens = &cap
	fmt.Println("opened")
	if _, err := io.ReadAll(os.Stdin); err != nil {
		t.Fatal(err)
	}
	expected, err := strconv.ParseUint(os.Getenv("INSTALLATION_TEST_REVISION"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, _ := (decision.Observation{}).Digest()
	result, saveErr := f.store.Save(persistence.Transaction{ExpectedRevision: expected, ExpectedFingerprint: fingerprint, CollectFingerprint: (decision.Observation{}).Digest, Content: snapshot.State})
	outcome := map[string]any{"cap": cap, "revision": result.Revision}
	if saveErr != nil {
		outcome["error"] = saveErr.Error()
	}
	data, err := json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(data))
}

func TestRegistryWindowFencesDelayedProcessAndReleasesAfterCrash(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	verifiedStartFixture(t, root)
	f, _, err := Start(StartOptions{Root: root, VCSRunner: testkit.NewFrozenGitRepo(), Request: StartRequest{BaseSnapshot: testkit.FrozenSnapshot, RunID: "process-window", IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	command := func(role, cap string) (*exec.Cmd, io.WriteCloser, io.ReadCloser) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestInstallationSubprocess$")
		cmd.Env = append(os.Environ(), "INSTALLATION_TEST_ROOT="+root, "INSTALLATION_TEST_ROLE="+role, "INSTALLATION_TEST_CAP="+cap, "INSTALLATION_TEST_REVISION="+strconv.FormatUint(snapshot.Revision, 10), "GORACE=atexit_sleep_ms=0")
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
			}
		})
		return cmd, input, output
	}
	readSignal := func(output io.Reader, signal string) {
		t.Helper()
		// Each signal is emitted only after its required lock/open prefix.
		data := make([]byte, len(signal)+1)
		if _, err := io.ReadFull(output, data); err != nil || string(data) != signal+"\n" {
			t.Fatalf("process signal=%q err=%v", data, err)
		}
	}
	writer, input, output := command("writer", "41")
	readSignal(output, "opened")
	before := installationFiles(t, f.stateDir)
	advanceInstallation(t, f.admission)
	_ = input.Close()
	result, err := io.ReadAll(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Wait(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result, []byte("STALE_FENCING_TOKEN")) {
		t.Fatalf("delayed process=%s", result)
	}
	if after := installationFiles(t, f.stateDir); !reflect.DeepEqual(before, after) {
		t.Fatal("delayed process wrote stale state")
	}
	// A holder from the current generation exits without releasing its lock.
	// Use the newly admitted run to check both process/state order and crash release.
	currentRoot := t.TempDir()
	verifiedStartFixture(t, currentRoot)
	currentAdmission := testAdmission(t, currentRoot, "current", "sha256:package", DefaultProvider)
	current, _, err := Start(StartOptions{Root: currentRoot, VCSRunner: testkit.NewFrozenGitRepo(), Request: StartRequest{BaseSnapshot: testkit.FrozenSnapshot, RunID: "process-window", IntakeConfirmationReceipt: testReceipt()}, Admission: currentAdmission})
	if err != nil {
		t.Fatal(err)
	}
	root = currentRoot
	snapshot, err = current.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	holder, holderInput, holderOutput := command("holder", "0")
	readSignal(holderOutput, "registry-held")
	blocked, blockedInput, blockedOutput := command("writer", "42")
	readSignal(blockedOutput, "opened")
	_ = blockedInput.Close()
	blockedResult := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(blockedOutput); blockedResult <- data }()
	select {
	case data := <-blockedResult:
		t.Fatalf("writer passed held registry: %s", data)
	case <-time.After(40 * time.Millisecond):
	}
	// Registry waits precede the Store state lock, which remains acquirable.
	release, err := coordination.AcquirePath(filepath.Join(current.stateDir, "write.lock"), "state probe")
	if err != nil {
		t.Fatalf("writer acquired state before registry: %v", err)
	}
	release()
	_ = holderInput.Close()
	if err := holder.Wait(); err != nil {
		t.Fatal(err)
	}
	result = <-blockedResult
	if err := blocked.Wait(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(result, []byte(`"error"`)) || !bytes.Contains(result, []byte(`"cap":42`)) {
		t.Fatalf("crash release writer=%s", result)
	}
	stored, err := current.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var state protocol.State
	if err := json.Unmarshal(stored.Content, &state); err != nil {
		t.Fatal(err)
	}
	if state.TokenCapMaxTokens == nil || *state.TokenCapMaxTokens != 42 || stored.Revision != snapshot.Revision+1 {
		t.Fatal("cross-process save lost content/revision")
	}
	snapshot, err = current.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	first, firstInput, firstOutput := command("writer", "43")
	second, secondInput, secondOutput := command("writer", "44")
	readSignal(firstOutput, "opened")
	readSignal(secondOutput, "opened")
	_ = firstInput.Close()
	_ = secondInput.Close()
	firstData, err := io.ReadAll(firstOutput)
	if err != nil {
		t.Fatal(err)
	}
	secondData, err := io.ReadAll(secondOutput)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := second.Wait(); err != nil {
		t.Fatal(err)
	}
	firstCommitted := !bytes.Contains(firstData, []byte(`"error"`))
	secondCommitted := !bytes.Contains(secondData, []byte(`"error"`))
	if firstCommitted == secondCommitted {
		t.Fatalf("same-revision process results: %s / %s", firstData, secondData)
	}
	loser := secondData
	cap := int64(43)
	if secondCommitted {
		loser = firstData
		cap = 44
	}
	if !bytes.Contains(loser, []byte("REVISION_CONFLICT")) {
		t.Fatalf("non-CAS loser=%s", loser)
	}
	stored, err = current.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stored.Content, &state); err != nil {
		t.Fatal(err)
	}
	if state.TokenCapMaxTokens == nil || *state.TokenCapMaxTokens != cap || stored.Revision != snapshot.Revision+1 {
		t.Fatal("parallel CAS lost winner bytes/revision")
	}
}

func TestFencedUnknownAttachmentPreservesItsObservation(t *testing.T) {
	t.Parallel()
	d := startValidationDriverAt(t, t.TempDir(), meteredProvider, nil)
	id := ""
	for action := range d.run.PendingActions {
		id = action
		break
	}
	if id == "" {
		t.Fatal("no issued dispatch")
	}
	unknown, err := protocol.NewSpawnReceiptEvent("unknown", id, meteredProvider, "correlation", protocol.SpawnStatusUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.f.Submit(unknown); err != nil {
		t.Fatal(err)
	}
	start, err := protocol.NewCorrelatedLifecycleEvent("observed-start", meteredProvider, "correlation", "actual-agent", protocol.LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.f.Submit(start); err != nil {
		t.Fatal(err)
	}
	before := installationFiles(t, d.f.root)
	advanceInstallation(t, d.f.admission)
	if _, _, err := d.f.engine.ReconcileUnknownReceipt(id, protocol.Observation{}); !errors.Is(err, errStaleFencingToken) {
		t.Fatalf("stale UNKNOWN attach=%v", err)
	}
	if after := installationFiles(t, d.f.root); !reflect.DeepEqual(before, after) {
		t.Fatal("stale UNKNOWN attach wrote binding/state or erased observation")
	}
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State.SpawnReceipts[id].Status != protocol.SpawnStatusUnknown {
		t.Fatal("stale attach changed UNKNOWN fact")
	}
}

func TestPromotionFencingPreservesIntentAndExecutedNativeFact(t *testing.T) {
	t.Parallel()
	for _, window := range []string{"before-intent", "before-effect", "before-result"} {
		t.Run(window, func(t *testing.T) {
			root := t.TempDir()
			identities := snapshotRepoCommits(t, root, "application", "reviewed changes")
			d := completePromotionPipeline(t, nil, root, identities[0])
			head, err := d.f.vcs.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			source := d.run.Validation.Candidate.Identity
			if window != "before-intent" {
				if err := d.f.engine.OpenPromotion(promotionInputForTest(t, d, source, "organized delivery"), protocol.Observation{}); err != nil {
					t.Fatal(err)
				}
			}
			if window == "before-result" {
				snapshot, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				intent, exists := snapshot.State.PromotionIntent()
				if !exists {
					t.Fatal("promotion has no original signed intent")
				}
				unlock, err := d.f.acquireNativeWindow()
				if err != nil {
					t.Fatal(err)
				}
				observed, complete, err := d.f.nativeEvidence(snapshot.State, intent, true)
				unlock()
				if err != nil || !complete || observed.Candidate == nil || observed.Candidate.Promotion == nil {
					t.Fatalf("original promotion effect did not complete: %+v %v", observed, err)
				}
			}
			beforeHead, err := d.f.vcs.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			beforeTree, err := d.f.vcs.StagedTreeDigest()
			if err != nil {
				t.Fatal(err)
			}
			beforeIndex, err := os.ReadFile(filepath.Join(root, ".git", "index"))
			if err != nil {
				t.Fatal(err)
			}
			beforeState, err := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			advanceInstallation(t, d.f.admission)
			if _, err := d.f.PrepareCandidate("organized delivery"); !errors.Is(err, errStaleFencingToken) {
				t.Fatalf("fenced promotion=%v", err)
			}
			afterHead, err := d.f.vcs.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			afterTree, err := d.f.vcs.StagedTreeDigest()
			if err != nil {
				t.Fatal(err)
			}
			afterIndex, err := os.ReadFile(filepath.Join(root, ".git", "index"))
			if err != nil {
				t.Fatal(err)
			}
			afterState, err := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if beforeHead != afterHead || beforeTree != afterTree || !bytes.Equal(beforeIndex, afterIndex) || !bytes.Equal(beforeState, afterState) {
				t.Fatal("fencing changed native HEAD/index/tree or intent/state")
			}
			if window == "before-result" {
				if beforeHead == head {
					t.Fatal("premise lost: actual effect did not move HEAD")
				}
				snapshot, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if pendingPromotionSource(snapshot.State) != source || snapshot.State.PromotedSnapshot != "" {
					t.Fatal("executed fact lost its original unresolved intent")
				}
			}
		})
	}
}

func TestSliceReceiptUsesOriginalRunAdmissions(t *testing.T) {
	t.Parallel()
	for _, window := range []string{"publish", "handoff", "discovery", "replay"} {
		t.Run(window, func(t *testing.T) {
			master := receiptMaster(t)
			child := receiptChild(t, master, "child", "lane-core")
			for _, f := range []*Facade{master.f, child.f} {
				req, err := f.request()
				if err != nil {
					t.Fatal(err)
				}
				if req.RunID != f.runID || admissionFromRequest(req) != f.admission || f.admission != master.f.admission || req.AdmissionRoot != master.f.root || req.PackageDigest != f.envelope.PackageDigest || req.InstalledTargetIdentity != f.envelope.InstalledTargetIdentity {
					t.Fatal("writer lost its immutable original installation admission")
				}
			}
			promoteReceiptChild(t, child)
			receipt := publishReceiptChild(t, child)
			assigned := driverState(t, master).SliceChildren["lane-core"]
			if assigned != child.f.runID || receipt.Receipt.ChildRunID != assigned || receipt.Receipt.MasterRunID != master.f.runID || receipt.Receipt.SliceID != "lane-core" || receipt.Digest != receipt.Receipt.CanonicalDigest() {
				t.Fatal("receipt lost its assigned child or immutable content binding")
			}
			if window == "publish" || window == "discovery" {
				// The state checkpoint is durable before the missing publish window.
				if err := os.Remove(filepath.Join(child.f.stateDir, "slice-receipt.json")); err != nil {
					t.Fatal(err)
				}
				advanceInstallation(t, child.f.admission)
			} else {
				if window == "replay" {
					if err := child.f.handoffSliceReceipt(receipt); err != nil {
						t.Fatal(err)
					}
				}
				advanceInstallation(t, master.f.admission)
			}
			beforeChild := installationFiles(t, child.f.stateDir)
			beforeMaster := installationFiles(t, master.f.stateDir)
			if window == "replay" {
				if err := master.f.engine.AcceptSliceReceipt(receipt, protocol.Observation{}); err != nil {
					t.Fatalf("pure accepted receipt replay=%v", err)
				}
				for _, query := range []func() (Run, error){master.f.Show, master.f.Status} {
					projection, err := query()
					if err != nil || projection.RunID != master.f.runID || !reflect.DeepEqual(projection.SliceReceipts[child.f.runID], receipt) {
						t.Fatalf("retained accepted receipt projection=%+v, %v", projection, err)
					}
					if next, err := master.f.Next(); err != nil || !reflect.DeepEqual(next, projection.Next) {
						t.Fatalf("retained accepted receipt next=%+v, %v", next, err)
					}
				}
			}
			var err error
			switch window {
			case "publish":
				err = child.f.publishSliceReceipt(&receipt)
			case "discovery":
				err = master.f.discoverSliceReceipts(driverState(t, master))
			case "handoff", "replay":
				err = child.f.handoffSliceReceipt(receipt)
			}
			if !errors.Is(err, errStaleFencingToken) {
				t.Fatalf("fenced slice window=%v", err)
			}
			if after := installationFiles(t, child.f.stateDir); !reflect.DeepEqual(beforeChild, after) {
				t.Fatal("fenced slice window changed child files")
			}
			if after := installationFiles(t, master.f.stateDir); !reflect.DeepEqual(beforeMaster, after) {
				t.Fatal("fenced slice window changed master files")
			}
		})
	}
}

func TestCheckpointFencingLeavesTheChildActive(t *testing.T) {
	t.Parallel()
	child := promotedSlice(t, "selected", OwnerBinding{})
	before, err := os.ReadFile(filepath.Join(child.f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	advanceInstallation(t, child.f.admission)
	if _, err := child.f.Drive(); !errors.Is(err, errStaleFencingToken) {
		t.Fatalf("stale checkpoint=%v", err)
	}
	after, err := os.ReadFile(filepath.Join(child.f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("fenced child changed checkpoint state")
	}
	if _, err := os.Stat(filepath.Join(child.f.stateDir, "slice-receipt.json")); !os.IsNotExist(err) {
		t.Fatal("fenced child published receipt")
	}
}

// Native admission is held only over the local VCS write, rather than host work.
type admittedGitRunner struct {
	inner    vcs.Runner
	registry string
	writes   int
}

func (runner *admittedGitRunner) Run(dir, command string, args ...string) (string, error) {
	if command == "git" && len(args) > 0 && (args[0] == "reset" || args[0] == "commit") {
		release, err := coordination.AcquirePath(runner.registry+".lock", "installation probe")
		if err == nil {
			release()
			return "", fmt.Errorf("native write has no registry admission")
		}
		var held *coordination.LockHeldError
		if !errors.As(err, &held) {
			return "", err
		}
		runner.writes++
	}
	return runner.inner.Run(dir, command, args...)
}

func (runner *admittedGitRunner) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	return runner.inner.RunRaw(dir, command, input, args...)
}

func TestNativePromotionHoldsItsActualAdmissionWindow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "application", "reviewed changes")
	d := completePromotionPipeline(t, nil, root, identities[0])
	candidate := d.run.Validation.Candidate
	promotion := promotionInputForTest(t, d, candidate.Identity, "admitted native delivery")
	if err := d.f.engine.OpenPromotion(promotion, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	if err := d.f.admission.write(func() error { return d.f.vcs.FastForward(candidate.Identity) }); err != nil {
		t.Fatal(err)
	}
	tree, err := d.f.vcs.TreeDigest("HEAD")
	if err != nil || tree != candidate.Digest {
		t.Fatalf("prepared target tree=%s want approved=%s err=%v", tree, candidate.Digest, err)
	}
	observer := &admittedGitRunner{inner: vcs.ExecRunner{}, registry: d.f.admission.RegistryPath}
	d.f.vcs = vcs.New(root, observer)
	if _, err := d.f.PrepareCandidate("admitted native delivery"); err != nil {
		t.Fatal(err)
	}
	if observer.writes != 2 {
		t.Fatalf("actual reset/commit writes=%d", observer.writes)
	}
	release, err := coordination.AcquirePath(d.f.admission.RegistryPath+".lock", "after promotion")
	if err != nil {
		t.Fatal(err)
	}
	release()
}
