package facade

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
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
	root := t.TempDir()
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
	if _, _, err := Start(StartOptions{Root: alias, Request: StartRequest{RunID: runID, IntakeConfirmationReceipt: testReceipt()}, Admission: admission}); err != nil {
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
	_, _ = reopened.DevelopmentPlan(protocol.RouteDecision{Mode: "default"})
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
	root := t.TempDir()
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
			_, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: "missing-" + missing, IntakeConfirmationReceipt: testReceipt()}, Admission: &incomplete})
			if err == nil || !strings.Contains(err.Error(), UnregisteredInstall) {
				t.Fatalf("incomplete binding=%v", err)
			}
			if _, err := os.Stat(startIntentPath(root, "missing-"+missing)); !os.IsNotExist(err) {
				t.Fatalf("rejected writer left intent: %v", err)
			}
		})
	}
	wrongProvider := *a
	if _, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: "provider-mismatch", Provider: "zcode", IntakeConfirmationReceipt: testReceipt()}, Admission: &wrongProvider}); err == nil || !strings.Contains(err.Error(), UnregisteredInstall) {
		t.Fatalf("provider mismatch=%v", err)
	}
}

func TestStaleStartRecoveryRetainsItsOriginalPrefix(t *testing.T) {
	root := t.TempDir()
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
	_, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: id, IntakeConfirmationReceipt: testReceipt()}, Admission: &next})
	if !errors.Is(err, errStaleFencingToken) {
		t.Fatalf("stale prefix recovery=%v", err)
	}
	if after := installationFiles(t, filepath.Join(root, EngineNamespace)); !reflect.DeepEqual(before, after) {
		t.Fatal("new caller cleaned old start prefix")
	}
}

func TestPersistenceRecoveryUsesOwningInstallation(t *testing.T) {
	for _, point := range []persistence.FaultPoint{persistence.FaultIntentBefore, persistence.FaultIntentAfter, persistence.FaultReplaceBefore, persistence.FaultReplaceAfter, persistence.FaultCommitResponseLost} {
		for _, fenced := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fenced=%t", point, fenced), func(t *testing.T) {
				root := t.TempDir()
				a := testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)
				f, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: "recovery", IntakeConfirmationReceipt: testReceipt()}, Admission: a})
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
				_, err = store.Save(persistence.Transaction{ExpectedRevision: snapshot.Revision, ExpectedFingerprint: emptyFingerprint(), CollectFingerprint: func() (string, error) { return emptyFingerprint(), nil }, Content: snapshot.State})
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

func TestFencedDispatchClaimDoesNotWriteLifecycleBinding(t *testing.T) {
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
	root := t.TempDir()
	transcript := filepath.Join(t.TempDir(), "owner.jsonl")
	writeTranscript(t, transcript, []string{intervalBaseLine})
	old := startIntervalRun(t, root, "old-owner", transcript)
	before := installationFiles(t, old.stateDir)
	next := advanceInstallation(t, old.admission)
	f, _, err := Start(StartOptions{Root: root, Owner: OwnerBinding{Transcript: transcript, Provider: meteredProvider}, Request: StartRequest{RunID: "new-owner", IntakeConfirmationReceipt: testReceipt()}, Admission: &next})
	if err != nil {
		t.Fatalf("new installation start=%v", err)
	}
	writeTranscript(t, transcript, []string{intervalBaseLine, intervalDuringLine})
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.Cost.Owner == nil || run.Cost.Owner.Source != "unavailable" || run.Cost.Owner.TotalInputTokens != 0 {
		t.Fatalf("overlap owner=%+v", run.Cost.Owner)
	}
	if after := installationFiles(t, old.stateDir); !reflect.DeepEqual(before, after) {
		t.Fatal("new installation wrote fenced peer")
	}
}

func TestFencedTerminalWindowsRetainEvidenceAndAllowPureReplay(t *testing.T) {
	for _, window := range []string{"intent", "cleanup", "summary", "remove-intent", "end-lifecycle", "complete"} {
		t.Run(window, func(t *testing.T) {
			root := t.TempDir()
			f, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: "terminal", IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.engine.CompleteAll(emptyFingerprint()); err != nil {
				t.Fatal(err)
			}
			run, err := f.project()
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
	result, saveErr := f.store.Save(persistence.Transaction{ExpectedRevision: expected, ExpectedFingerprint: emptyFingerprint(), CollectFingerprint: func() (string, error) { return emptyFingerprint(), nil }, Content: snapshot.State})
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
	root := t.TempDir()
	f, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: "process-window", IntakeConfirmationReceipt: testReceipt()}, Admission: testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)})
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
	currentAdmission := testAdmission(t, currentRoot, "current", "sha256:package", DefaultProvider)
	current, _, err := Start(StartOptions{Root: currentRoot, Request: StartRequest{RunID: "process-window", IntakeConfirmationReceipt: testReceipt()}, Admission: currentAdmission})
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
	if _, _, err := d.f.engine.ReconcileUnknownReceipt(id, emptyFingerprint()); !errors.Is(err, errStaleFencingToken) {
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
	for _, window := range []string{"before-intent", "before-effect", "before-result"} {
		t.Run(window, func(t *testing.T) {
			root := t.TempDir()
			identities := snapshotRepoCommits(t, root, "application", "reviewed changes")
			d := completePromotionPipeline(t, nil, root, identities[0])
			head, err := d.f.vcs.Resolve()
			if err != nil {
				t.Fatal(err)
			}
			if window != "before-intent" {
				if err := d.f.engine.OpenPromotion(promotionInputForTest(t, d, head, "organized delivery"), emptyFingerprint()); err != nil {
					t.Fatal(err)
				}
			}
			if window == "before-result" {
				if err := d.f.admission.write(func() error { return d.f.vcs.SquashToBase(identities[0], "organized delivery") }); err != nil {
					t.Fatal(err)
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
				if pendingPromotionSource(snapshot.State) != head || snapshot.State.PromotedSnapshot != "" {
					t.Fatal("executed fact lost its original unresolved intent")
				}
			}
		})
	}
}

func TestSliceReceiptUsesOriginalRunAdmissions(t *testing.T) {
	for _, window := range []string{"publish", "handoff", "discovery", "replay"} {
		t.Run(window, func(t *testing.T) {
			master := receiptMaster(t)
			child := receiptChild(t, master, "child", "lane-core", "custom", OwnerBinding{})
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
			var err error
			switch window {
			case "publish":
				err = child.f.publishSliceReceipt(&receipt)
			case "discovery":
				err = master.f.discoverSliceReceipts(driverState(t, master))
			case "handoff", "replay":
				err = child.f.handoffSliceReceipt(receipt)
			}
			if window == "replay" {
				if err != nil {
					t.Fatalf("immutable accepted receipt replay=%v", err)
				}
			} else if !errors.Is(err, errStaleFencingToken) {
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
	child := promotedSlice(t, "custom", OwnerBinding{})
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
func TestNativePromotionHoldsItsActualAdmissionWindow(t *testing.T) {
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "application", "reviewed changes")
	d := completePromotionPipeline(t, nil, root, identities[0])
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
