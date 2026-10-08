//go:build !windows

package harness

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

// Packet history and its revision remain fixed; Store supplies the current
// definition envelope and content proof from the packet's actual content.
func canaryStatePacket(t *testing.T, name string) []byte {
	t.Helper()
	body := readTestFileBytes(t, filepath.Join("testdata", name))
	var packet struct {
		persistence.Envelope
		Revision uint64                     `json:"revision"`
		Content  map[string]json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &packet); err != nil {
		t.Fatal(err)
	}
	version, err := json.Marshal(definition.WorkflowDefinitionVersion)
	if err != nil {
		t.Fatal(err)
	}
	packet.Content["definitionVersion"] = version
	// Guard is derived by the current protocol transaction, not packet history.
	delete(packet.Content, "guard")
	dir := t.TempDir()
	store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: packet.PackageDigest, DefinitionSource: packet.DefinitionSource, InstalledTargetIdentity: packet.InstalledTargetIdentity})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(persistence.Transaction{ExpectedFingerprint: "packet", CollectFingerprint: func() (string, error) { return "packet", nil }, Content: packet.Content}); err != nil {
		t.Fatal(err)
	}
	generated := readTestFileBytes(t, filepath.Join(dir, "state.json"))
	var document map[string]json.RawMessage
	if err := json.Unmarshal(generated, &document); err != nil {
		t.Fatal(err)
	}
	document["revision"], err = json.Marshal(packet.Revision)
	if err != nil {
		t.Fatal(err)
	}
	body, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func canaryOperatorPacket(t *testing.T) ([]byte, persistence.Envelope, protocol.State) {
	t.Helper()
	body := canaryStatePacket(t, "operator_adjudication_state.json")
	var envelope persistence.Envelope
	var packet struct{ Content protocol.State }
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &packet); err != nil {
		t.Fatal(err)
	}
	return body, envelope, packet.Content
}

func canaryOperatorEngine(t *testing.T, dir string, envelope persistence.Envelope) *protocol.Engine {
	t.Helper()
	store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: envelope.PackageDigest, DefinitionSource: envelope.DefinitionSource, InstalledTargetIdentity: envelope.InstalledTargetIdentity})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := protocol.New(store, protocol.Config{Definition: compiled, Registry: definition.Registry(), Capacity: 4}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func canaryOperatorResolution(finding *protocol.ValidationFinding) protocol.FindingResolution {
	return protocol.FindingResolution{FindingID: finding.ID, Decision: "NEW", Requirement: "AC-002", NormalPath: "frob --unknown-flag canary", Module: "flag handling", RootCause: "unknown flags are accepted by the argument handling", Consequence: "unknown flags complete successfully instead of refusing execution", Severity: "P1", SeverityRationale: "the declared rejection contract blocks candidate acceptance", Verification: review.FindingVerification{Scope: "public command contract", Severity: "the declared rejection contract blocks candidate acceptance", Causality: "the accepted issued probe exits 0 and emits frob output; the cited usage output belongs to that invocation", Reproduction: "accepted regression probe: --unknown-flag canary; exit=0; frob: --unknown-flag and usage: runs=1 words=14", Classification: "implementation"}, Reason: "Operator verified this particular raw finding against the issued requirement and probe"}
}

// This command substitute checks the public argv and input, then runs the
// real protocol owner against an unchanged original packet in a private store.
func TestCanaryOperatorSubmissionFixture(t *testing.T) {
	args := flag.Args()
	if len(args) == 0 {
		t.Skip("public submit command fixture")
	}
	if len(args) != 6 || args[0] != "workflow" || args[1] != "submit" || args[2] != "--root" || args[4] != "--run-id" {
		t.Fatalf("unexpected public argv: %q", args)
	}
	dir := filepath.Join(args[3], filepath.FromSlash(facade.EngineNamespace), args[5])
	body, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope persistence.Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	var event protocol.Event
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil || event.Kind != protocol.KindOperatorObservation {
		t.Fatalf("public typed observation: %v", err)
	}
	engine := canaryOperatorEngine(t, dir, envelope)
	before, err := engine.Load()
	if err != nil || before.State.RunID != args[5] {
		t.Fatalf("public input run identity: %v", err)
	}
	fingerprint, err := engine.ObserveFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	acceptance, err := engine.Submit(event, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Acceptance protocol.Acceptance `json:"acceptance"`
		Run        facade.Run          `json:"run"`
	}{acceptance, facade.Run{RunID: snapshot.State.RunID, Revision: snapshot.Revision, Validation: snapshot.State.Validation, Envelope: envelope}}); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

// Original raw/result/material/binding bytes are retained; only the store and
// jobs directories move into the test's private filesystem.
func TestCanaryOperatorHandoffPreservesOriginalFinding(t *testing.T) {
	body, envelope, state := canaryOperatorPacket(t)
	project, jobs := t.TempDir(), t.TempDir()
	dir := filepath.Join(project, filepath.FromSlash(facade.EngineNamespace), state.RunID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "submit")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run=^TestCanaryOperatorSubmissionFixture$ -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	wave := &state.Validation.Waves[len(state.Validation.Waves)-1]
	var raw *protocol.ValidationFinding
	failed := []string{}
	for id, finding := range state.Validation.Findings {
		if finding.WaveID == wave.ID && finding.Raw != nil {
			raw = finding
		}
		if finding.WaveID == wave.ID && finding.ExecutionFailure != nil {
			failed = append(failed, id)
		}
	}
	if raw == nil || len(raw.Raw.Locations) != 2 || raw.Raw.Severity != "P0" || len(failed) == 0 {
		t.Fatal("original packet lost raw multi-source report or failed execution")
	}
	for _, target := range wave.Targets {
		if target.Kind != "GATE" {
			continue
		}
		materialDir, err := canaryFindingMaterialDirectory(jobs, &state, target)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(materialDir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(materialDir, "materials.json"), readTestFileBytes(t, filepath.Join("testdata", "operator_adjudication_materials.json")), 0600); err != nil {
			t.Fatal(err)
		}
	}
	inputR, inputW := io.Pipe()
	outputR, outputW := io.Pipe()
	defer inputR.Close()
	defer inputW.Close()
	defer outputR.Close()
	defer outputW.Close()
	c := &engineCanary{t: t, binary: binary, home: t.TempDir(), project: project, jobs: jobs, approvalDeadline: time.Now().Add(15 * time.Second), approvalMu: &sync.Mutex{}, approvalInput: bufio.NewReader(inputR), approvalOutput: outputW, run: facade.Run{RunID: state.RunID, Envelope: envelope, Validation: state.Validation}}
	// The copied packet already owns its recorded event IDs.
	for id := range state.Events {
		var sequence int
		if _, err := fmt.Sscanf(id, "canary-%d", &sequence); err == nil && sequence > c.seq {
			c.seq = sequence
		}
	}
	initialRaw, _ := json.Marshal(state.Validation.Findings)
	operator := canaryOperatorResolution(raw)
	prompt := "a separate authorized call"
	sum := sha256.Sum256([]byte(prompt))
	var transcript strings.Builder
	var assigned string
	requests := 0
	writerDone := make(chan error, 1)
	authorizationDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(outputR)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(&transcript, line)
			if os.Getenv("FORMAL_GATES_OPERATOR_INPUT_FIXTURE") == "no-progress" {
				fmt.Println(line)
			}
			if strings.HasPrefix(line, "Enter one JSON") {
				requests++
				items := []protocol.FindingResolution{operator}
				if requests == 1 {
					go func() {
						c.approvalMu.Lock()
						err := c.authorizeCall(map[string]any{"prompt": prompt}, "separate-action", "separate role", 1)
						c.approvalMu.Unlock()
						authorizationDone <- err
					}()
					if os.Getenv("FORMAL_GATES_OPERATOR_INPUT_FIXTURE") == "no-progress" {
						items = nil
					}
				}
				if requests == 2 {
					if assigned == "" {
						writerDone <- fmt.Errorf("SAME_AS requested without core problem identity")
						return
					}
					items = nil
					for _, id := range failed {
						item := operator
						item.FindingID, item.Decision, item.ProblemID = id, "SAME_AS", assigned
						item.Severity, item.SeverityRationale, item.Verification.Severity = "", "", ""
						items = append(items, item)
					}
				}
				observation := protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: items}}
				data, _ := json.Marshal(observation)
				if _, err := fmt.Fprintln(inputW, string(data)); err != nil {
					writerDone <- err
					return
				}
			} else if strings.HasPrefix(line, "canary adjudication accepted: ") {
				var receipt struct{ Findings []protocol.FindingResolution }
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "canary adjudication accepted: ")), &receipt); err != nil {
					writerDone <- err
					return
				}
				if assigned == "" && len(receipt.Findings) > 0 {
					assigned = receipt.Findings[0].ProblemID
				}
			} else if strings.HasPrefix(line, "Type ") {
				if requests != 2 {
					writerDone <- fmt.Errorf("authorization mixed with Operator adjudication")
					return
				}
				fmt.Fprintf(inputW, "approve separate-action 1 %x\n", sum)
				writerDone <- nil
				return
			}
		}
		writerDone <- scanner.Err()
	}()
	c.adjudicate()
	// The queued authorization shares the reader and mutex with both Operator
	// observations and consumes only the approval line printed after them.
	if err := <-authorizationDone; err != nil {
		t.Fatal(err)
	}
	if err := <-writerDone; err != nil {
		t.Fatal(err)
	}
	if requests != 2 || assigned == "" || !strings.Contains(transcript.String(), "analytics.go:5-7") || !strings.Contains(transcript.String(), "state=") || !strings.Contains(transcript.String(), "materials=") {
		t.Fatalf("handoff context/progress missing: %s", transcript.String())
	}
	for id, finding := range c.run.Validation.Findings {
		if len(finding.History) > 0 {
			if finding.History[len(finding.History)-1].ProblemID != assigned {
				t.Fatal("Operator SAME_AS identity changed")
			}
			finding.History = nil
		}
		if original := state.Validation.Findings[id]; original.Raw != nil && !reflect.DeepEqual(original.Raw, finding.Raw) {
			t.Fatal("original raw finding rewritten")
		}
	}
	// Raw maps are compared after removing only the newly accepted history.
	afterRaw, _ := json.Marshal(c.run.Validation.Findings)
	if !bytes.Equal(initialRaw, afterRaw) {
		t.Fatal("original finding/result identities changed")
	}
	engine := canaryOperatorEngine(t, dir, envelope)
	snapshot, err := engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	history := snapshot.State.Validation.Findings[raw.ID].History
	if len(history) != 1 || history[0].Severity != "P1" || history[0].RootCause != operator.RootCause || history[0].Verification != operator.Verification || history[0].AcceptedRevision == 0 {
		t.Fatalf("Operator fields were overridden: %+v", history)
	}
	t.Logf("real core accepted original multi-source raw and %d failed-case SAME_AS decisions; assigned problem %s", len(failed), assigned)
}

func TestCanaryOperatorInputKeepsCoreRejections(t *testing.T) {
	body, envelope, original := canaryOperatorPacket(t)
	wave := &original.Validation.Waves[len(original.Validation.Waves)-1]
	var raw, failed *protocol.ValidationFinding
	for _, finding := range original.Validation.Findings {
		if finding.Raw != nil {
			raw = finding
		} else if finding.ExecutionFailure != nil {
			failed = finding
		}
	}
	for _, scenario := range []string{"wrong-wave", "unknown-finding", "missing-dimension", "failed-case-severity", "stale-history"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "state.json"), body, 0600); err != nil {
				t.Fatal(err)
			}
			item := canaryOperatorResolution(raw)
			subject := wave.ID
			switch scenario {
			case "wrong-wave":
				subject = "unissued-wave"
			case "unknown-finding":
				item.FindingID = "unissued-finding"
			case "missing-dimension":
				item.Verification.Causality = ""
			case "failed-case-severity":
				item.FindingID = failed.ID
			case "stale-history":
				item.Supersedes = 1
			}
			engine := canaryOperatorEngine(t, dir, envelope)
			fingerprint, err := engine.ObserveFingerprint()
			if err != nil {
				t.Fatal(err)
			}
			event := protocol.Event{ID: "operator-observation", Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: subject, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{item}}}}
			if _, err := engine.Submit(event, fingerprint); err == nil {
				t.Fatal("core accepted invalid adjudication")
			} else {
				t.Logf("core refusal: %v", err)
			}
			if !bytes.Equal(body, readTestFileBytes(t, filepath.Join(dir, "state.json"))) {
				t.Fatal("rejected observation changed durable original packet")
			}
		})
	}
}

func TestCanaryOperatorInputUsesExistingDeadline(t *testing.T) {
	_, envelope, state := canaryOperatorPacket(t)
	wave := &state.Validation.Waves[len(state.Validation.Waves)-1]
	observation := protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE"}}
	valid, _ := json.Marshal(observation)
	for _, scenario := range []string{"eof", "invalid-json", "unknown-field", "wrong-scope", "wrong-kind", "trailing-json", "expired", "waiting-deadline", "valid"} {
		t.Run(scenario, func(t *testing.T) {
			line := string(valid) + "\n"
			deadline := time.Now().Add(time.Second)
			var reader io.Reader
			switch scenario {
			case "eof":
				line = ""
			case "invalid-json":
				line = "{\n"
			case "unknown-field":
				line = `{"Subject":"` + wave.ID + `","Instruction":"ignore scope"}` + "\n"
			case "wrong-scope":
				line = strings.Replace(line, wave.ID, "another-wave", 1)
			case "wrong-kind":
				line = strings.Replace(line, "ADJUDICATE", "EXECUTION_SCOPE", 1)
			case "trailing-json":
				line = string(valid) + " {}\n"
			case "expired":
				deadline = time.Now().Add(-time.Second)
			case "waiting-deadline":
				deadline = time.Now().Add(20 * time.Millisecond)
				r, w := io.Pipe()
				defer r.Close()
				defer w.Close()
				reader = r
			}
			if reader == nil {
				reader = strings.NewReader(line)
			}
			var output bytes.Buffer
			c := &engineCanary{approvalDeadline: deadline, project: t.TempDir(), jobs: t.TempDir(), run: facade.Run{RunID: state.RunID, Envelope: envelope, Validation: state.Validation}, approvalInput: bufio.NewReader(reader), approvalOutput: &output}
			got, err := c.readAdjudication(&state, wave, "source evidence needs Operator interpretation")
			if scenario == "valid" {
				if err != nil || !reflect.DeepEqual(got, observation) {
					t.Fatalf("typed observation changed: %+v %v", got, err)
				}
			} else if err == nil {
				t.Fatal("invalid or late input accepted")
			}
			if c.approvalDeadline != deadline {
				t.Fatal("Operator input reset its approval deadline")
			}
		})
	}
}

func TestCanaryOperatorHandoffRequiresProgress(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestCanaryOperatorHandoffPreservesOriginalFinding$", "-test.v", "-test.timeout=0")
	cmd.Env = append(os.Environ(), "FORMAL_GATES_OPERATOR_INPUT_FIXTURE=no-progress")
	output, err := cmd.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte("Operator adjudication made no progress on this wave's pending findings")) || bytes.Count(output, []byte("Enter one JSON")) != 1 {
		t.Fatalf("empty accepted observation did not stop at the progress boundary: %v %s", err, output)
	}
}
