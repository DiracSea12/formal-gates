package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/harness"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
)

func TestPreparedIntakeBindsSelectedMaterialAndStartsThroughOriginalAdmission(t *testing.T) {
	t.Parallel()
	for _, changed := range []string{"", "requirements.md", "design.md", "reference.md"} {
		t.Run("changed="+changed, func(t *testing.T) {
			root := t.TempDir()
			contents := map[string]string{"requirements.md": strings.ReplaceAll(validationDocument, "\n", "\r\n"), "design.md": "# Plan\r\n\r\nUse the approved entry.\r\n", "reference.md": "# Reference\n\nExact selected evidence.\n"}
			for path, content := range contents {
				if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			preview, err := PrepareIntake(root, filepath.Join(root, "requirements.md"), "./design.md", []string{"reference.md", "./reference.md"}, true)
			if err != nil {
				t.Fatal(err)
			}
			receipt := preview.IntakeCandidate
			if receipt.Source != DefaultIntakeSource || receipt.Authority != DefaultIntakeAuthority || receipt.Transport != DefaultIntakeTransport || receipt.RequirementSource != "requirements.md" || len(preview.Materials) != 3 || len(receipt.Artifacts) != 3 {
				t.Fatalf("prepared identity and selection: %+v", preview)
			}
			for i, material := range preview.Materials {
				normalized := strings.ReplaceAll(contents[material.Path], "\r\n", "\n")
				sum := sha256.Sum256([]byte(normalized))
				expected := hex.EncodeToString(sum[:])
				if material.Content != normalized || material.Revision != expected || receipt.Artifacts[i] != (IntakeArtifact{Path: material.Path, Revision: expected}) {
					t.Fatalf("material bytes or binding differ: %+v", material)
				}
			}
			if preview.RequirementDocument == nil || preview.Content != preview.RequirementDocument.Content || preview.Revision != preview.RequirementDocument.ContentDigest || receipt.RequirementRevision != preview.Revision {
				t.Fatal("precheck and candidate do not share the full confirmed requirement")
			}
			if receipt.SolutionRevision != preview.Materials[0].Revision || receipt.SolutionDigest != "sha256:"+preview.Materials[0].Revision {
				t.Fatal("solution selection was not bound to design.md")
			}
			digest, err := IntakeDigest(receipt)
			if err != nil || digest != receipt.Digest {
				t.Fatal("candidate digest differs from the existing canonical owner", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
				t.Fatal("unconfirmed preview created run or receipt state")
			}
			if changed != "" {
				if err := os.WriteFile(filepath.Join(root, changed), []byte(contents[changed]+"changed\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// This fixture supplies the displayed material's explicit user confirmation.
			configureLightweight(t, root)
			_, run, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{RunID: "prepared-intake", Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: receipt}, Admission: testAdmission(t, root, "intake-runtime", "sha256:package", DefaultProvider)})
			if changed != "" {
				if err == nil || !strings.Contains(err.Error(), InvalidIntakeConfirmation) || !strings.Contains(err.Error(), "artifact "+changed+" revision mismatch") {
					t.Fatalf("original file verification did not reject changed material: %v", err)
				}
				if _, err := os.Stat(filepath.Join(root, EngineNamespace)); !os.IsNotExist(err) {
					t.Fatal("changed material reached bootstrap")
				}
				return
			}
			if err != nil || run.Status != "ACTIVE" || !reflect.DeepEqual(run.IntakeConfirmationReceipt, &receipt) {
				t.Fatalf("prepared receipt did not enter original Start: %+v %v", run, err)
			}
			var request StartRequest
			body, err := os.ReadFile(filepath.Join(root, EngineNamespace, "prepared-intake", "request.json"))
			if err != nil || json.Unmarshal(body, &request) != nil || !reflect.DeepEqual(request.IntakeConfirmationReceipt, receipt) {
				t.Fatal("persisted request changed selected material bindings", err)
			}
		})
	}
}

func TestPreparedIntakeKeepsSingleFileDefaultAndLightweightMaterial(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "requirements.md")
	if err := os.WriteFile(path, []byte("# Confirmed request and solution\n"), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := PrepareIntake(root, "requirements.md", "", nil, false)
	if err != nil || len(preview.Materials) != 1 || preview.RequirementDocument != nil || preview.IntakeCandidate.SolutionRevision != preview.Revision || preview.IntakeCandidate.SolutionDigest != "sha256:"+preview.Revision {
		t.Fatalf("single-file lightweight preview: %+v %v", preview, err)
	}
	if _, err := PrepareIntake(root, "requirements.md", "", nil, true); err == nil {
		t.Fatal("regular precheck skipped REQ/AC structure")
	}
	if _, err := PrepareIntake(root, "../requirements.md", "", nil, false); err == nil {
		t.Fatal("selection escaped the project")
	}
}

func TestRegularStartRejectsUnstructuredConfirmedIntakeBeforeBootstrap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	path := filepath.Join(root, "requirements.md")
	content := []byte("# Confirmed small task and solution\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := PrepareIntake(root, "requirements.md", "", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	_, precheckErr := PrepareIntake(root, "requirements.md", "", nil, true)
	if precheckErr == nil {
		t.Fatal("regular precheck accepted unstructured material")
	}
	options := StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{RunID: "confirmed-intake", Route: "regular", VCS: "git", BaseSnapshot: base, IntakeConfirmationReceipt: preview.IntakeCandidate}, Admission: testAdmission(t, root, "intake-runtime", "sha256:package", DefaultProvider)}
	for attempt := 0; attempt < 2; attempt++ {
		f, run, err := Start(options)
		if err == nil || err.Error() != precheckErr.Error() || f != nil || run.RunID != "" {
			t.Fatalf("Start must return the original precheck error before bootstrap: facade=%v run=%+v err=%v", f != nil, run, err)
		}
		for _, name := range []string{EngineNamespace, filepath.Join(".gates", "tmp", options.Request.RunID)} {
			if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
				t.Fatalf("rejected intake created bootstrap material at %s: %v", name, err)
			}
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, content) {
			t.Fatal("rejected intake changed the confirmed source", err)
		}
	}
	options.Request.Route = "lightweight"
	configureLightweight(t, root)
	f, run, err := Start(options)
	if err != nil || f == nil || run.Status != "ACTIVE" || run.RequirementGuarantee != nil || !reflect.DeepEqual(run.IntakeConfirmationReceipt, &preview.IntakeCandidate) {
		t.Fatalf("lightweight rejected its ordinary confirmed material: %+v %v", run, err)
	}
	snapshot, err := f.engine.Load()
	if err != nil || snapshot.State.RequirementGuarantee != nil {
		t.Fatal("lightweight persisted a regular guarantee", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, content) {
		t.Fatal("lightweight Start changed the confirmed source", err)
	}
}

func TestPreparedIntakeRegularStartBindsPrecheckedDocument(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	preview, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	f, run, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{RunID: "prepared-regular", Route: "regular", VCS: "git", BaseSnapshot: base, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: preview.IntakeCandidate}, Admission: testAdmission(t, root, "regular-intake-runtime", "sha256:package", DefaultProvider)})
	if err != nil || run.Status != "ACTIVE" || !reflect.DeepEqual(run.IntakeConfirmationReceipt, &preview.IntakeCandidate) {
		t.Fatalf("regular Start did not accept its prechecked candidate: %+v %v", run, err)
	}
	expected := protocol.NewGuaranteeActivation(preview.Revision, preview.IntakeCandidate.SolutionRevision, *preview.RequirementDocument)
	snapshot, err := f.engine.Load()
	if err != nil || !reflect.DeepEqual(snapshot.State.RequirementGuarantee, expected) {
		t.Fatal("regular Start did not freeze the complete confirmed guarantee", err)
	}
	f, err = Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = f.engine.Load()
	if err != nil || !reflect.DeepEqual(snapshot.State.RequirementGuarantee, expected) {
		t.Fatal("reopen changed the frozen guarantee", err)
	}
}

func TestPreparedIntakeStartReusesGeneratedSourceIdentities(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	content := strings.ReplaceAll(strings.ReplaceAll(validationDocument, "REQ-001：", ""), "AC-001：", "")
	path := filepath.Join(root, "requirements.md")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	document := preview.RequirementDocument
	if document == nil || document.Requirements[0].ID != "REQ-001" || document.Requirements[0].AcceptanceConditions[0].ID != "AC-001" || document.Content != content {
		t.Fatalf("precheck source projection: %+v", document)
	}
	// The fixture confirms this exact displayed material before Start.
	f, run, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{RunID: "semantic-intake", Route: "regular", VCS: "git", BaseSnapshot: base, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: preview.IntakeCandidate}, Admission: testAdmission(t, root, "semantic-intake-runtime", "sha256:package", DefaultProvider)})
	if err != nil || run.Status != "ACTIVE" || !reflect.DeepEqual(run.IntakeConfirmationReceipt, &preview.IntakeCandidate) {
		t.Fatalf("confirmed semantic material failed original Start: %+v %v", run, err)
	}
	snapshot, err := f.engine.Load()
	if err != nil || !reflect.DeepEqual(snapshot.State.RequirementGuarantee, protocol.NewGuaranteeActivation(preview.Revision, preview.IntakeCandidate.SolutionRevision, *document)) {
		t.Fatal("Start reparsing changed the confirmed identities or content", err)
	}
	if _, err = f.Drive(); err != nil {
		t.Fatal(err)
	}
	snapshot, err = f.engine.Load()
	if err != nil || snapshot.State.Validation == nil || !reflect.DeepEqual(snapshot.State.Validation.Document, *document) {
		t.Fatal("first drive did not preserve the prechecked source projection", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != content {
		t.Fatal("Start rewrote the confirmed material", err)
	}
}

type intakePathAdapter struct {
	*host.Descriptor
	calls int
	fail  bool
}

func (a *intakePathAdapter) ResolveNativePaths(native, sources map[string]string) error {
	a.calls++
	if a.fail {
		return fmt.Errorf("selected path unavailable")
	}
	native["intakeNativeRoot"] = os.Getenv("INTAKE_NATIVE_ROOT")
	sources["intakeNativeRoot"] = "INTAKE_NATIVE_ROOT"
	return nil
}

func TestStartFreezesSelectedPathsAndChildrenInheritWithoutDiscovery(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	usage, err := host.Lookup("claude")
	if err != nil {
		t.Fatal(err)
	}
	selected := &intakePathAdapter{Descriptor: &host.Descriptor{ID: "intake-native", InstallName: "intake-native", Installable: true, ReadUsage: usage.ReadUsage}}
	unselected := &intakePathAdapter{Descriptor: &host.Descriptor{ID: "intake-unselected", InstallName: "intake-unselected", Installable: true}, fail: true}
	for _, adapter := range []*intakePathAdapter{selected, unselected} {
		if err := host.Register(adapter); err != nil {
			t.Fatal(err)
		}
	}
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit-settings=%v", explicit), func(t *testing.T) {
			selected.calls = 0
			root := testkit.ShortRoot(t)
			base := snapshotRepoCommits(t, root)[0]
			preview, err := PrepareIntake(root, "requirements.md", "", nil, true)
			if err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(root, ".gates", "config.json")
			if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config, []byte(`{"capacity":3,"qaCapacity":2,"model":"chosen-model","reasoningEffort":"high","tokenCap":77}`), 0600); err != nil {
				t.Fatal(err)
			}
			home := t.TempDir()
			loaded, err := settings.Load(root, home)
			if err != nil {
				t.Fatal(err)
			}
			options := StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{RunID: "native-parent", Route: "regular", Provider: selected.ID, VCS: "git", BaseSnapshot: base, IntakeConfirmationReceipt: preview.IntakeCandidate}, Admission: testAdmission(t, root, "intake-path-runtime", "sha256:package", selected.ID)}
			if explicit {
				options.Settings = &loaded
				if err := os.WriteFile(config, []byte(`{"capacity":9,"qaCapacity":5,"model":"later-model","reasoningEffort":"low","tokenCap":5}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("INTAKE_NATIVE_ROOT", "native-A")
			f, run, err := Start(options)
			if err != nil || selected.calls != 1 || unselected.calls != 0 {
				t.Fatalf("selected top-level discovery: %v selected=%d unselected=%d", err, selected.calls, unselected.calls)
			}
			if loaded.Values.Native["intakeNativeRoot"] != "" {
				t.Fatal("Start changed caller settings")
			}
			snapshot, err := f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			want := loaded
			want.Values.Native = maps.Clone(loaded.Values.Native)
			want.Sources = maps.Clone(loaded.Sources)
			want.Values.Native["intakeNativeRoot"], want.Sources["intakeNativeRoot"] = "native-A", "INTAKE_NATIVE_ROOT"
			want.Values.TokenCap = nil
			want.Configuration, want.ConfigurationSource = settings.Configuration{}, ""
			want.Revisions = nil
			if !reflect.DeepEqual(snapshot.State.Settings, &want) || snapshot.State.TokenCapMaxTokens == nil || *snapshot.State.TokenCapMaxTokens != 77 || run.Settings.Values.TokenCap == nil || *run.Settings.Values.TokenCap != 77 {
				t.Fatalf("persistent settings/common/sources/cap differ: %+v", snapshot.State.Settings)
			}
			before, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("INTAKE_NATIVE_ROOT", "native-B")
			if err := os.WriteFile(config, []byte(`{"capacity":9,"qaCapacity":5,"model":"later-model","reasoningEffort":"low","tokenCap":5}`), 0600); err != nil {
				t.Fatal(err)
			}
			f, err = Open(root, run.RunID)
			if err != nil || selected.calls != 1 {
				t.Fatalf("Open rediscovered paths: %v calls=%d", err, selected.calls)
			}
			after, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("Open changed persistent settings, cap, cost or grants", err)
			}
			d := &validationDriver{t: t, f: f, run: run, revision: preview.Revision, provider: selected.ID, git: f.vcs}
			d.drive()
			passPreDevelopmentReviews(t, d, splitCommandPlan())
			d.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})})
			prepareChildFrontier(t, d)
			d.drive()
			parent := driverState(t, d)
			if len(parent.SliceChildren) != 2 || selected.calls != 1 || unselected.calls != 0 {
				t.Fatalf("children rediscovered paths or did not start: %+v calls=%d", parent.SliceChildren, selected.calls)
			}
			for _, id := range parent.SliceChildren {
				child, err := Open(root, id)
				if err != nil {
					t.Fatal(err)
				}
				state, err := child.engine.Load()
				if err != nil || !reflect.DeepEqual(state.State.Settings, parent.Settings) || state.State.TokenCapMaxTokens == nil || *state.State.TokenCapMaxTokens != 77 || child.capacity != 3 {
					t.Fatalf("child lost inherited settings/cap: %v %+v", err, state.State.Settings)
				}
			}
			fresh := options
			fresh.Request.RunID = "native-fresh"
			fresh.Settings = &loaded
			_, next, err := Start(fresh)
			if err != nil || selected.calls != 2 || next.Settings.Values.Native["intakeNativeRoot"] != "native-B" {
				t.Fatalf("new run did not discover B: %v %+v calls=%d", err, next.Settings, selected.calls)
			}
		})
	}
}

func TestStartRejectsInvalidIntakeAndResolverFailureBeforeIntent(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	adapter := &intakePathAdapter{Descriptor: &host.Descriptor{ID: "intake-refusal", InstallName: "intake-refusal", Installable: true}, fail: true}
	if err := host.Register(adapter); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"admission", "confirmation", "definition", "guarantee", "resolver"} {
		t.Run(invalid, func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			preview, err := PrepareIntake(root, "requirements.md", "", nil, true)
			if err != nil {
				t.Fatal(err)
			}
			options := StartOptions{Root: root, Request: StartRequest{RunID: "intake-refusal", Route: "regular", VCS: "git", BaseSnapshot: base, IntakeConfirmationReceipt: preview.IntakeCandidate}, Admission: testAdmission(t, root, "intake-refusal-runtime", "sha256:package", adapter.ID)}
			switch invalid {
			case "admission":
				options.Admission = nil
			case "confirmation":
				options.Request.IntakeConfirmationReceipt.RequirementRevision = ""
			case "definition":
				options.Request.DefinitionDigest = "sha256:other"
			case "guarantee":
				if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte("# Unstructured confirmed material\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			adapter.calls = 0
			if _, _, err := Start(options); err == nil {
				t.Fatal("rejected start succeeded")
			}
			wantCalls := 0
			if invalid == "resolver" {
				wantCalls = 1
			}
			if adapter.calls != wantCalls {
				t.Fatalf("discovery before admission/intake/definition/guarantee: %d", adapter.calls)
			}
			for _, path := range []string{filepath.Join(root, EngineNamespace), startIntentPath(root, options.Request.RunID), filepath.Join(root, ".gates", "tmp", options.Request.RunID)} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("rejected start wrote bootstrap at %s: %v", path, err)
				}
			}
		})
	}
}

func TestNativeStartKeepsFrozenPathsForColdWorkerAndResume(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	preview, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	admission := testAdmission(t, root, "native-path-runtime", "sha256:package", "workbuddy")
	homeA, homeB, nativeA, nativeB, binA, binB := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	canonical := func(path string) string {
		t.Helper()
		value, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	homeA, homeB, nativeA, nativeB, binA, binB = canonical(homeA), canonical(homeB), canonical(nativeA), canonical(nativeB), canonical(binA), canonical(binB)
	calls := filepath.Join(t.TempDir(), "calls")
	peer := "#!/bin/sh\nprintf '%s|%s|%s|%s|%s\\n' \"$*\" \"$HOME\" \"$CODEBUDDY_CONFIG_DIR\" \"$WORKBUDDY_CONFIG_DIR\" \"$ACC_PRODUCT_CONFIG_PATH\" >> \"$NATIVE_INTAKE_CALLS\"\n[ \"$#\" = 1 ] && [ \"$1\" = --help ] || exit 91\nprintf '%s\\n' '--resume --session-id stream-json --mcp-config --tools --permission-mode dontAsk'\n"
	peerName := "codebuddy"
	if runtime.GOOS == "windows" {
		peerName += ".exe"
	}
	for _, bin := range []string{binA, binB} {
		if runtime.GOOS == "windows" {
			tool := buildPreparationExecutable(t, bin, `package main
import ("fmt"; "os"; "strings")
func main() {
 f, err := os.OpenFile(os.Getenv("NATIVE_INTAKE_CALLS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
 if err != nil { panic(err) }
 fmt.Fprintf(f, "%s|%s|%s|%s|%s\n", strings.Join(os.Args[1:], " "), os.Getenv("HOME"), os.Getenv("CODEBUDDY_CONFIG_DIR"), os.Getenv("WORKBUDDY_CONFIG_DIR"), os.Getenv("ACC_PRODUCT_CONFIG_PATH"))
 if err := f.Close(); err != nil { panic(err) }
 if len(os.Args) != 2 || os.Args[1] != "--help" { os.Exit(91) }
 fmt.Println("--resume --session-id stream-json --mcp-config --tools --permission-mode dontAsk")
}`)
			if err := os.Rename(tool, filepath.Join(bin, peerName)); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(filepath.Join(bin, peerName), []byte(peer), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, native := range []string{nativeA, nativeB} {
		product := filepath.Join(native, "cache", "acc-product-config-v3.json")
		if err := os.MkdirAll(filepath.Dir(product), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(product, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(root, ".gates", "config.json")
	if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(`{"capacity":2,"qaCapacity":1,"model":"selected-model","reasoningEffort":"high","tokenCap":77,"claudeExecutable":"missing-unselected-claude","codexExecutable":"missing-unselected-codex","cursorExecutable":"missing-unselected-cursor","zcodeExecutable":"missing-unselected-zcode","dshExecutable":"missing-unselected-dsh"}`), 0600); err != nil {
		t.Fatal(err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("HOME", homeA)
	t.Setenv("USERPROFILE", homeA)
	t.Setenv("PATH", binA+string(os.PathListSeparator)+originalPath)
	t.Setenv("WORKBUDDY_CONFIG_DIR", nativeA)
	t.Setenv("ACC_PRODUCT_CONFIG_PATH", "")
	t.Setenv("NATIVE_INTAKE_CALLS", calls)
	loaded, err := settings.Load(root, homeA)
	if err != nil {
		t.Fatal(err)
	}
	options := StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{RunID: "native-paths", Route: "regular", VCS: "git", BaseSnapshot: base, IntakeConfirmationReceipt: preview.IntakeCandidate}, Admission: admission}
	f, run, err := Start(options)
	if err != nil {
		t.Fatal(err)
	}
	want := loaded
	want.Values.Native = maps.Clone(loaded.Values.Native)
	want.Sources = maps.Clone(loaded.Sources)
	for key, value := range map[string]string{"workbuddyExecutable": filepath.Join(binA, peerName), "workbuddyHome": homeA, "workbuddyConfigRoot": nativeA, "workbuddyProductConfig": filepath.Join(nativeA, "cache", "acc-product-config-v3.json")} {
		want.Values.Native[key] = value
	}
	homeSource := "HOME"
	if runtime.GOOS == "windows" {
		homeSource = "USERPROFILE"
	}
	for key, source := range map[string]string{"workbuddyExecutable": "PATH", "workbuddyHome": homeSource, "workbuddyConfigRoot": "WORKBUDDY_CONFIG_DIR", "workbuddyProductConfig": "workbuddyConfigRoot"} {
		want.Sources[key] = source
	}
	want.Values.TokenCap = nil
	want.Configuration, want.ConfigurationSource = settings.Configuration{}, ""
	want.Revisions = nil
	state, err := f.engine.Load()
	if err != nil || !reflect.DeepEqual(state.State.Settings, &want) {
		t.Fatalf("Start did not persist complete native/common/sources/files: %v %+v", err, state.State.Settings)
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Fatal("Start mapper invoked a native command", err)
	}
	stored, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var persisted protocol.State
	if err := json.Unmarshal(stored.Content, &persisted); err != nil || !reflect.DeepEqual(persisted.Settings, &want) || persisted.Settings.Values.TokenCap != nil || persisted.TokenCapMaxTokens == nil || *persisted.TokenCapMaxTokens != 77 {
		t.Fatal("state JSON lost settings or duplicated the cap", err)
	}
	run, err = f.Drive()
	if err != nil || len(run.WorkerMaterials) != 1 {
		t.Fatal("regular Start did not issue the original worker", err)
	}
	var material WorkerMaterial
	for _, value := range run.WorkerMaterials {
		material = value
	}
	t.Setenv("HOME", homeB)
	t.Setenv("USERPROFILE", homeB)
	t.Setenv("PATH", binB+string(os.PathListSeparator)+originalPath)
	t.Setenv("WORKBUDDY_CONFIG_DIR", nativeB)
	if err := os.WriteFile(config, []byte(`{"capacity":9,"qaCapacity":5,"model":"later-model","reasoningEffort":"low","tokenCap":5,"workbuddyExecutable":"missing-later-workbuddy"}`), 0600); err != nil {
		t.Fatal(err)
	}
	f, err = Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	bind := func(resumeID string) map[string]any {
		t.Helper()
		request, _, _, original, err := f.WorkerExecutionRequest(material, resumeID)
		if err != nil || original.ActionID != material.ActionID || request["model"] != "selected-model" || request["reasoning_effort"] != "high" || request["provider"] != "workbuddy" {
			t.Fatalf("original worker identity/common selection changed: %v %v", err, request)
		}
		shown, err := f.Show()
		if err != nil {
			t.Fatal(err)
		}
		binding, err := harness.NativeBinding(request["provider"].(string), shown.Settings.Values)
		if err != nil {
			t.Fatal(err)
		}
		launch := binding["launch"].(map[string]any)
		environment := binding["environment"].(map[string]any)
		if launch["executable"] != want.Values.Native["workbuddyExecutable"] || environment["HOME"] != homeA || environment["CODEBUDDY_CONFIG_DIR"] != nativeA || environment["WORKBUDDY_CONFIG_DIR"] != nativeA || environment["ACC_PRODUCT_CONFIG_PATH"] != want.Values.Native["workbuddyProductConfig"] {
			t.Fatalf("cold invocation rediscovered B: %v", binding)
		}
		if resumeID != "" && request["identity"] != "original-session" {
			t.Fatal("resume lost signed native identity")
		}
		return binding
	}
	first := bind("")
	cold, err := f.engine.Load()
	if err != nil || !reflect.DeepEqual(cold.State.Settings, before.State.Settings) || !reflect.DeepEqual(cold.State.Cost, before.State.Cost) || !reflect.DeepEqual(cold.State.Guard, before.State.Guard) {
		t.Fatal("cold request changed settings, cost or grants", err)
	}
	for _, event := range []protocol.Event{func() protocol.Event {
		e, err := protocol.NewSpawnReceiptEvent("native-spawn", material.ActionID, "workbuddy", "original-session", protocol.SpawnStatusSpawned)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}(), func() protocol.Event {
		e, err := protocol.NewLifecycleEventEvent("native-start", "workbuddy", "original-session", protocol.LifecycleStart)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}()} {
		if _, _, err := f.Submit(event); err != nil {
			t.Fatal(err)
		}
	}
	updatedPeer := []byte(peer + "# current invocation content\n")
	if runtime.GOOS == "windows" {
		updatedPeer, err = os.ReadFile(want.Values.Native["workbuddyExecutable"])
		if err != nil {
			t.Fatal(err)
		}
		updatedPeer = append(updatedPeer, []byte("current invocation content\n")...)
	}
	if err := os.WriteFile(want.Values.Native["workbuddyExecutable"], updatedPeer, 0700); err != nil {
		t.Fatal(err)
	}
	fingerprint := protocol.Observation{}
	plan, _, err := f.engine.RecoverAttempt(material.ActionID, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, fingerprint)
	if err != nil || plan.Action != protocol.RecoveryResumeAttempt {
		t.Fatal("original interruption did not authorize resume", err)
	}
	current, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	resumeID := ""
	for id, intent := range current.State.PendingHostActions {
		if intent.Correlation == plan.RequestID && intent.Operation == protocol.HostActionResumeAgent {
			resumeID = id
		}
	}
	if resumeID == "" {
		t.Fatal("missing signed resume intent")
	}
	f, err = Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	second := bind(resumeID)
	if reflect.DeepEqual(first["files"], second["files"]) {
		t.Fatal("resume cached executable proof instead of proving current content")
	}
	after, err := f.engine.Load()
	if err != nil || !reflect.DeepEqual(after.State.Settings, before.State.Settings) || !reflect.DeepEqual(after.State.TokenCapMaxTokens, before.State.TokenCapMaxTokens) || !reflect.DeepEqual(after.State.Cost, current.State.Cost) || !reflect.DeepEqual(after.State.Guard, current.State.Guard) || !reflect.DeepEqual(after.State.RecoveryRecords, current.State.RecoveryRecords) || !reflect.DeepEqual(after.State.PendingHostActions, current.State.PendingHostActions) || !reflect.DeepEqual(after.State.Attempts, current.State.Attempts) {
		t.Fatal("resume changed settings/cap/cost or lost retry authorization", err)
	}
	callsBody, err := os.ReadFile(calls)
	line := "--help|" + homeA + "|" + nativeA + "|" + nativeA + "|" + want.Values.Native["workbuddyProductConfig"] + "\n"
	if err != nil || string(callsBody) != line+line {
		t.Fatalf("non-model native argv/environment: %q %v", callsBody, err)
	}
	options.Request.RunID = "native-paths-fresh"
	options.Settings = &loaded
	_, fresh, err := Start(options)
	if err != nil || fresh.Settings.Values.Native["workbuddyHome"] != homeB || fresh.Settings.Values.Native["workbuddyConfigRoot"] != nativeB || fresh.Settings.Values.Native["workbuddyExecutable"] != filepath.Join(binB, peerName) {
		t.Fatalf("new run did not resolve B: %+v %v", fresh.Settings, err)
	}
}
