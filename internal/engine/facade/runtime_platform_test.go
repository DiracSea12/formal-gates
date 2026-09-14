package facade

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/settings"
)

// This local process is a test host, not a model and not real review evidence.
// Its v2 changes identity/result/token field names. Only this adapter file knows
// either version; the same public runtime and evidence consumers handle both.
type fixtureRuntime struct {
	streamRuntimeAdapter
	id, executableOption, stateOption string
	serverVersion, adapterVersion     int
	answer                            func(nativeTurn) any
	beforeCapture                     func() error
}

type fixtureMessage struct {
	StartID   string          `json:"startId"`
	Version   int             `json:"version"`
	Resume    string          `json:"resume"`
	StateFile string          `json:"stateFile"`
	Answer    json.RawMessage `json:"answer"`
	Probe     bool            `json:"probe"`
}

type fixtureReply struct {
	Identity string
	Answer   json.RawMessage
	Usage    host.Usage
}

func TestB6FixtureProcess(t *testing.T) {
	if os.Getenv("FORMAL_GATES_B6_FIXTURE_PROCESS") != "1" {
		return
	}
	var q fixtureMessage
	if err := json.NewDecoder(os.Stdin).Decode(&q); err != nil {
		os.Exit(2)
	}
	identity := q.StartID
	if q.Resume != "" || q.Probe {
		old, err := os.ReadFile(q.StateFile)
		if err != nil || string(old) != q.Resume {
			os.Exit(3)
		}
		identity = q.Resume
	} else if err := os.WriteFile(q.StateFile, []byte(identity), 0600); err != nil {
		os.Exit(4)
	}
	// Opaque platform state uses only the assigned invocation directory. v2
	// deliberately changes both native field names and the private file layout.
	if !q.Probe {
		path := fixturePrivateArtifact(q.Version)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			os.Exit(6)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(7)
		}
		_, writeErr := f.WriteString(identity + "\nresume=" + q.Resume)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			os.Exit(8)
		}
	}
	row := map[string]any{"version": q.Version}
	if q.Version == 1 {
		row["identity"], row["answer"], row["usage"] = identity, q.Answer, []int64{11, 3}
	} else {
		row["session"], row["result"], row["tokens"] = identity, q.Answer, []int64{11, 3}
	}
	if err := json.NewEncoder(os.Stdout).Encode(row); err != nil {
		os.Exit(5)
	}
	os.Exit(0)
}

func fixturePrivateArtifact(version int) string {
	if version == 1 {
		return "vendor-cache.bin"
	}
	return filepath.Join("vendor-v2", "private", "session.index")
}

func (a *fixtureRuntime) decode(raw json.RawMessage) (fixtureReply, error) {
	var row map[string]json.RawMessage
	var v int
	if err := json.Unmarshal(raw, &row); err != nil {
		return fixtureReply{}, err
	}
	if err := json.Unmarshal(row["version"], &v); err != nil {
		return fixtureReply{}, err
	}
	id, answer, usage := "identity", "answer", "usage"
	if v == 2 && a.adapterVersion >= 2 {
		id, answer, usage = "session", "result", "tokens"
	} else if v != 1 {
		return fixtureReply{}, fmt.Errorf("fixture protocol %d is unsupported", v)
	}
	var result fixtureReply
	var tokens []int64
	if err := json.Unmarshal(row[id], &result.Identity); err != nil {
		return result, err
	}
	if err := json.Unmarshal(row[usage], &tokens); err != nil || len(tokens) != 2 {
		return result, fmt.Errorf("fixture usage missing")
	}
	if result.Identity == "" {
		return result, fmt.Errorf("fixture identity missing")
	}
	result.Answer = row[answer]
	result.Usage = host.Usage{InputCacheMissTokens: tokens[0], OutputTokens: tokens[1]}
	return result, nil
}

func (a *fixtureRuntime) rows(in io.Reader, visit func(fixtureReply) error) error {
	dec := json.NewDecoder(in)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		row, err := a.decode(raw)
		if err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
}

func (a *fixtureRuntime) descriptor() *host.Descriptor {
	return &host.Descriptor{ID: a.id,
		LifecycleEvents: []string{"fixture.started", "fixture.stopped"},
		Lifecycle: &host.Lifecycle{NormalizeEvent: func(event string) (string, error) {
			switch event {
			case "fixture.started":
				return "subagent_start", nil
			case "fixture.stopped":
				return "subagent_stop", nil
			}
			return "", fmt.Errorf("unsupported fixture lifecycle event")
		}},
		Configuration: []host.ConfigField{
			host.StringConfig(a.executableOption, ""), host.StringConfig(a.stateOption, "")},
		ReadUsage: func(in io.Reader) (host.Usage, error) {
			var total host.Usage
			count := 0
			err := a.rows(in, func(r fixtureReply) error {
				count++
				total.InputCacheMissTokens += r.Usage.InputCacheMissTokens
				total.OutputTokens += r.Usage.OutputTokens
				return nil
			})
			if count == 0 && err == nil {
				err = fmt.Errorf("no fixture usage")
			}
			return total, err
		},
		CheckIdentity: func(path, identity string) error {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			count := 0
			err = a.rows(f, func(r fixtureReply) error {
				count++
				if r.Identity != identity {
					return fmt.Errorf("wrong fixture identity")
				}
				return nil
			})
			if count == 0 && err == nil {
				err = fmt.Errorf("no fixture identity")
			}
			return err
		},
		ObserveRecovery: func(r host.RecoveryObservation) string {
			row, err := a.decode(r.NativeResponse)
			if err == nil && r.NativeMethod == "fixture/resume" && row.Identity == r.Identity {
				return "RESUMABLE"
			}
			return "UNKNOWN"
		},
	}
}
func (a *fixtureRuntime) capture(c settings.Values) (*protocol.HostRuntimeContext, error) {
	if a.beforeCapture != nil {
		if err := a.beforeCapture(); err != nil {
			return nil, err
		}
	}
	return captureStreamContext(a.id, c.NativeString(a.executableOption), map[string]string{"FIXTURE_STATE_FILE": c.NativeString(a.stateOption)})
}
func (a *fixtureRuntime) validate(c *protocol.HostRuntimeContext) error {
	return validateStreamContext(c, a.id)
}
func (a *fixtureRuntime) invoke(ctx context.Context, c *protocol.HostRuntimeContext, job, action, resume string, answer json.RawMessage, probe bool) (nativeProcessResult, error) {
	cmd := exec.Command(c.Executable, "-test.run=^TestB6FixtureProcess$")
	cmd.Dir = job
	if err := runtimeCommandContext(cmd, c); err != nil {
		return nativeProcessResult{}, err
	}
	cmd.Env = append(cmd.Env, "FORMAL_GATES_B6_FIXTURE_PROCESS=1")
	return runNativeStreamProcess(ctx, cmd, job, action, func(in io.Reader, out io.WriteCloser) (nativeProcessResult, error) {
		if err := json.NewEncoder(out).Encode(fixtureMessage{Version: a.serverVersion, Resume: resume, StateFile: fixtureStateFile(c, action, resume), Answer: answer, Probe: probe, StartID: fixtureSessionID(action)}); err != nil {
			return nativeProcessResult{}, err
		}
		var raw json.RawMessage
		if err := json.NewDecoder(in).Decode(&raw); err != nil {
			return nativeProcessResult{}, err
		}
		r, err := a.decode(raw)
		return nativeProcessResult{Identity: r.Identity, Answer: r.Answer}, err
	})
}

// Native stores support independent sessions. The local process fixture uses
// one file per identity so parallel actions cannot overwrite each other's state.
func fixtureStateFile(c *protocol.HostRuntimeContext, action, resume string) string {
	identity := resume
	if identity == "" {
		identity = fixtureSessionID(action)
	}
	return c.Environment["FIXTURE_STATE_FILE"] + "-" + identity
}

func (a *fixtureRuntime) turn(q nativeTurn) (string, error) {
	c, resume, job := q.Claim, q.Resume, q.Job
	// A deterministic review response is an explicit fixture, never an actual review.
	answer, _ := json.Marshal(protocol.RawReviewCandidate{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Action: string(c.Task.Step), Status: "PASS", Complete: true, Message: "Local host protocol fixture, not a semantic review", Findings: []protocol.ReviewFinding{}})
	if a.answer != nil {
		answer, _ = json.Marshal(a.answer(q))
	}
	identity := ""
	if resume {
		identity = c.Identity
	}
	out, err := a.invoke(context.Background(), c.Context, job, c.ActionID, identity, answer, false)
	if err == nil {
		err = q.Attached(out.Identity)
	}
	return out.Identity, err
}

func (a *fixtureRuntime) readIdentity(job, action string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(job, "stdout.jsonl"))
	if err != nil {
		return "", err
	}
	r, err := a.decode(raw)
	return r.Identity, err
}
func (a *fixtureRuntime) probe(q nativeProbe) (string, json.RawMessage, error) {
	_, err := a.invoke(context.Background(), q.Context, q.Job, q.ActionID, q.Identity, json.RawMessage(`{}`), true)
	if err != nil {
		return "fixture/resume", nil, err
	}
	raw, err := os.ReadFile(filepath.Join(q.Job, "stdout.jsonl"))
	return "fixture/resume", raw, err
}

var fixtureRegistrationCounter atomic.Uint64

func TestNativePlatformSingleRegistrationAndProtocolUpgrade(t *testing.T) {
	suffix := strconv.FormatUint(fixtureRegistrationCounter.Add(1), 10)
	a := &fixtureRuntime{id: "b6-fixture-" + suffix, executableOption: "fixtureExecutable" + suffix, stateOption: "fixtureState" + suffix, serverVersion: 1, adapterVersion: 1}
	descriptor := a.descriptor()
	if err := registerNativePlatform(descriptor, a); err != nil {
		t.Fatal(err)
	} // The only registration.
	hooks, err := lifecycle.HookDefinitions(a.id)
	if err != nil || len(hooks) != 2 || hooks[0].Event != "fixture.started" || hooks[1].Command[3] != a.id {
		t.Fatalf("single registration did not reach lifecycle installation: %+v %v", hooks, err)
	}
	root, home := t.TempDir(), t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]string{a.executableOption: executable, a.stateOption: filepath.Join(root, "native-session")}
	if err := os.MkdirAll(filepath.Join(root, ".gates"), 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	effective, err := settings.Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := captureRuntimeContext(a.id, effective.Values, root)
	if err != nil {
		t.Fatal(err)
	}
	// Both runtime dispatch and evidence use the same descriptor object.
	entry, err := host.LookupAdapter(a.id)
	if err != nil || entry.HostDescriptor() != descriptor {
		t.Fatal("descriptor diverged", err)
	}
	runtime, err := runtimeAdapter(a.id)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.validate(binding); err != nil {
		t.Fatal(err)
	}
	a.answer = func(nativeTurn) any { return map[string]string{"output": "small local fixture"} }
	invoke := func(label, identity string) protocol.TaskOperationReceipt {
		job := filepath.Join(root, label)
		id, err := runtime.turn(nativeTurn{Claim: protocol.ExecutionClaim{ActionID: label, Provider: a.id, Identity: identity, Context: binding}, Settings: effective.Values, Job: job, Workspace: root, Resume: identity != "", Attached: func(string) error { return nil }})
		result := protocol.TaskOperationReceipt{Status: "FAILED", Identity: id}
		if err != nil {
			result.Message = err.Error()
			return result
		}
		data, err := os.ReadFile(filepath.Join(job, "answer.json"))
		if err != nil {
			t.Fatal(err)
		}
		var answer struct {
			Output string `json:"output"`
		}
		if err = json.Unmarshal(data, &answer); err != nil {
			t.Fatal(err)
		}
		result.Output = answer.Output
		result.TranscriptPath, err = runtime.source(id, job, binding)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(result.TranscriptPath)
		replay, err := runtime.source(id, job, binding)
		after, _ := os.ReadFile(replay)
		if err != nil || string(before) != string(after) {
			t.Fatal("observation repeated native effect", err)
		}
		result.Status = "COMPLETED"
		return result
	}
	first := invoke("first", "")
	if first.Status != "COMPLETED" || first.Identity == "" || first.Output != "small local fixture" {
		t.Fatalf("start: %+v", first)
	}
	a.serverVersion = 2
	// A native change is not silently accepted by an unadapted implementation.
	failed := invoke("unadapted", first.Identity)
	if failed.Status != "FAILED" {
		t.Fatalf("unadapted protocol accepted: %+v", failed)
	}
	a.adapterVersion = 2 // Only platform parsing changes; no core edit or re-registration.
	second := invoke("upgraded", first.Identity)
	if second.Status != "COMPLETED" || second.Identity != first.Identity {
		t.Fatalf("resume/upgrade: %+v", second)
	}
	for label, version := range map[string]int{"first": 1, "upgraded": 2} {
		if _, err := os.Stat(filepath.Join(root, label, fixturePrivateArtifact(version))); err != nil {
			t.Fatalf("platform-private v%d artifact not retained: %v", version, err)
		}
	}
	usage, err := cost.ParseTranscript(a.id, second.TranscriptPath)
	if err != nil || usage.InputCacheMissTokens != 22 || usage.OutputTokens != 6 {
		t.Fatalf("cross-version usage: %+v %v", usage, err)
	}
	if err := descriptor.CheckIdentity(second.TranscriptPath, first.Identity); err != nil {
		t.Fatal(err)
	}
	if err := descriptor.CheckIdentity(second.TranscriptPath, "wrong"); err == nil {
		t.Fatal("wrong identity accepted")
	}
	raw, err := os.ReadFile(filepath.Join(root, "upgraded", "stdout.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	recovery := protocol.DeveloperRecovery{Claim: protocol.ExecutionClaim{Provider: a.id}, Identity: first.Identity, NativeMethod: "fixture/resume", NativeResponse: raw}
	if recovery.Outcome() != "RESUMABLE" {
		t.Fatal("recovery did not use platform descriptor")
	}
	recovery.Identity = "other"
	if recovery.Outcome() != "UNKNOWN" {
		t.Fatal("mismatched recovery accepted")
	}
	encoded, err := json.Marshal(effective.Values)
	if err != nil {
		t.Fatal(err)
	}
	var restored settings.Values
	if err := json.Unmarshal(encoded, &restored); err != nil || !reflect.DeepEqual(restored, effective.Values) {
		t.Fatalf("frozen native config lost: %s %v", encoded, err)
	}
	config[a.executableOption] = filepath.Join(root, "missing-native-executable")
	data, _ = json.Marshal(config)
	if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	missing, err := settings.Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := captureRuntimeContext(a.id, missing.Values, root); err == nil {
		t.Fatal("missing native capability accepted")
	}
	if _, err := runtimeAdapter("claude-code"); err == nil {
		t.Fatal("installation-only host accepted as a runtime")
	}
	if !strings.Contains(first.TranscriptPath, "native-sources") {
		t.Fatal("common evidence path not used")
	}
}

func fixtureSessionID(action string) string {
	sum := sha256.Sum256([]byte(action))
	return fmt.Sprintf("local-fixture-%x", sum[:8])
}
