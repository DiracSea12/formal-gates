package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/host"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type envelope struct {
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload"`
}
type workspaceInput struct {
	Root        string `json:"root"`
	Candidate   string `json:"candidate"`
	OperationID string `json:"operation_id"`
}
type bindingInput struct {
	Root      string `json:"root"`
	Candidate string `json:"candidate"`
	Revision  string `json:"revision"`
	Check     Check  `json:"check"`
}
type sealInput struct {
	Spec      Spec          `json:"spec"`
	Revision  string        `json:"revision"`
	Root      string        `json:"root"`
	Candidate string        `json:"candidate"`
	Checks    []CheckResult `json:"checks"`
	Gates     []GateResult  `json:"gates"`
}

func git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var errbuf bytes.Buffer
	cmd.Stderr = &errbuf
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errbuf.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
func clean(root string) error {
	s, err := git(root, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return err
	}
	if s != "" {
		return fmt.Errorf("working tree is not clean: %s", s)
	}
	return nil
}
func snapshot(q workspaceInput) (any, error) {
	if len(q.OperationID) != 64 {
		return nil, fmt.Errorf("snapshot needs a stable operation id")
	}
	if _, err := hex.DecodeString(q.OperationID); err != nil {
		return nil, fmt.Errorf("invalid operation id")
	}
	head, err := vcs.New(q.Root, nil).Resolve()
	if err != nil {
		return nil, err
	}
	// A commit is an observable receipt: replay cannot create an extra candidate.
	message, err := git(q.Root, "log", "-1", "--format=%B")
	if err != nil {
		return nil, err
	}
	trailer := "Formal-Gates-Operation: " + q.OperationID
	if strings.Contains(message, trailer) {
		if err := clean(q.Root); err != nil {
			return nil, err
		}
		return map[string]any{"candidate": head, "replayed": true}, nil
	}
	status, err := git(q.Root, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return nil, err
	}
	if status == "" {
		return map[string]any{"candidate": head, "replayed": false, "unchanged": true}, nil
	}
	if _, err = git(q.Root, "add", "--all", "--", "."); err != nil {
		return nil, err
	}
	if _, err = git(q.Root, "-c", "user.name=formal-gates", "-c", "user.email=formal-gates@localhost", "-c", "commit.gpgsign=false", "commit", "-m", "Prepare harness candidate\n\n"+trailer); err != nil {
		return nil, err
	}
	head, err = vcs.New(q.Root, nil).Resolve()
	return map[string]any{"candidate": head, "replayed": false}, err
}
func scopeBinding(q bindingInput) (string, error) {
	if len(q.Revision) != 64 || q.Candidate == "" {
		return "", fmt.Errorf("candidate and revision are required")
	}
	// Default is the entire candidate. Scoped dependencies are explicit user input.
	cmd := exec.Command("git", "-C", q.Root, "ls-tree", "-r", "-z", q.Candidate)
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read candidate tree: %w", err)
	}
	entries := []string{}
	matches := map[string]bool{}
	for _, e := range bytes.Split(b, []byte{0}) {
		if len(e) == 0 {
			continue
		}
		parts := bytes.SplitN(e, []byte{'\t'}, 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid git tree entry")
		}
		p := string(parts[1])
		use := len(q.Check.Dependencies) == 0
		for _, d := range q.Check.Dependencies {
			if !safeDependency(d) {
				return "", fmt.Errorf("invalid dependency")
			}
			if p == d || strings.HasPrefix(p, d+"/") {
				use = true
				matches[d] = true
			}
		}
		if use {
			entries = append(entries, string(e))
		}
	}
	for _, d := range q.Check.Dependencies {
		if !matches[d] {
			entries = append(entries, "MISSING:"+d)
		}
	}
	sort.Strings(entries)
	return Digest(struct {
		Revision string
		Check    Check
		Entries  []string
	}{q.Revision, q.Check, entries}), nil
}
func certify(q sealInput) (any, error) {
	if err := ValidateSpec(q.Spec); err != nil {
		return nil, err
	}
	if q.Revision != Digest(q.Spec) {
		return nil, fmt.Errorf("requirement/spec revision mismatch")
	}
	head, err := vcs.New(q.Root, nil).Resolve()
	if err != nil {
		return nil, err
	}
	if head != q.Candidate {
		return nil, fmt.Errorf("candidate moved after verification")
	}
	if err = clean(q.Root); err != nil {
		return nil, err
	}
	if len(q.Checks) != len(q.Spec.Checks) || len(q.Gates) != len(q.Spec.Gates) {
		return nil, fmt.Errorf("missing/extra verification results")
	}
	cr := map[string]CheckResult{}
	for _, r := range q.Checks {
		if _, ok := cr[r.ID]; ok {
			return nil, fmt.Errorf("duplicate result %s", r.ID)
		}
		cr[r.ID] = r
	}
	for _, c := range q.Spec.Checks {
		r, ok := cr[c.ID]
		if !ok || r.Status != "PASS" || r.ExitCode != 0 || r.Evidence == "" || r.Candidate != q.Candidate {
			return nil, fmt.Errorf("check %s is not a bound PASS", c.ID)
		}
		binding, err := scopeBinding(bindingInput{q.Root, q.Candidate, q.Revision, c})
		if err != nil {
			return nil, err
		}
		if binding != r.Binding {
			return nil, fmt.Errorf("check %s has stale evidence", c.ID)
		}
	}
	gr := map[string]GateResult{}
	for _, r := range q.Gates {
		if _, ok := gr[r.ID]; ok {
			return nil, fmt.Errorf("duplicate gate")
		}
		gr[r.ID] = r
	}
	for _, id := range q.Spec.Gates {
		r, ok := gr[id]
		if !ok || r.Candidate != q.Candidate || r.Revision != q.Revision || r.Evidence == "" {
			return nil, fmt.Errorf("gate %s binding is missing", id)
		}
		if err := ValidateWorker(id, r.Result, q.Spec.Checks); err != nil {
			return nil, err
		}
		if NeedsRepair(r.Result) {
			return nil, fmt.Errorf("gate %s has unresolved mandatory findings", id)
		}
	}
	level, status := "native", "HARNESS_COMPLETE"
	if q.Spec.Provider == "fixture" {
		level, status = "fixture", "DEMO_COMPLETE"
	}
	return map[string]any{"contract_version": Version, "profile": Profile, "status": status, "evidence_level": level, "candidate": q.Candidate, "revision": q.Revision, "checks": q.Checks, "gates": q.Gates}, nil
}

// orderedProofDTO carries one recorded proof in document order.
type orderedProofDTO struct {
	Name     string          `json:"name"`
	Expected json.RawMessage `json:"expected"`
}

func decodeProofs(entries []orderedProofDTO) ([]Proof, error) {
	proofs := make([]Proof, 0, len(entries))
	for _, entry := range entries {
		expected, err := decodeObject(entry.Expected)
		if err != nil {
			return nil, err
		}
		proofs = append(proofs, Proof{Name: entry.Name, Expected: expected})
	}
	return proofs, nil
}

// decodeObject is decodeValue restricted to a JSON object.
func decodeObject(raw json.RawMessage) (map[string]any, error) {
	value, err := decodeValue(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected a JSON object")
	}
	return object, nil
}

// decodeValue parses a payload value while preserving number literals, so the
// canonical encoder can decide between integer and float the way Python does.
func decodeValue(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// nativeOutcome maps a native operation error to its bridge answer. A typed
// nativeRefusal becomes the in-value refusal object (handled=true); any other
// error is a bridge failure and is returned untouched.
func nativeOutcome(err error) (any, bool) {
	if refusal, ok := err.(*nativeRefusal); ok {
		return map[string]any{"ok": false, "kind": refusal.kind, "refusal": refusal.refusal}, true
	}
	return nil, false
}

func Bridge(in io.Reader, out io.Writer) int {
	b, err := io.ReadAll(io.LimitReader(in, 8*1024*1024+1))
	if err == nil && len(b) > 8*1024*1024 {
		err = fmt.Errorf("bridge input exceeds 8 MiB")
	}
	var e envelope
	if err == nil {
		err = StrictDecode(b, &e)
	}
	var result any
	if err == nil {
		result, err = dispatch(e)
	}
	if err != nil {
		_ = json.NewEncoder(out).Encode(map[string]any{"ok": false, "code": "INVALID_HARNESS_OPERATION", "message": err.Error()})
		return 2
	}
	if err = json.NewEncoder(out).Encode(map[string]any{"ok": true, "value": result}); err != nil {
		return 1
	}
	return 0
}
func dispatch(e envelope) (any, error) {
	switch e.Operation {
	case "native-questions":
		var q struct {
			Run string `json:"run"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		questions, err := nativeRunQuestions(q.Run)
		if err != nil {
			return nil, err
		}
		views := []any{}
		for i, question := range questions {
			views = append(views, nativeQuestionView(question.Record, i+1))
		}
		return views, nil
	case "host-descriptors":
		var q struct{}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		return host.All(), nil
	case "schema":
		var q struct {
			Checks []Check `json:"checks,omitempty"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		return WorkerSchema(q.Checks...), nil
	case "validate-spec":
		var q Spec
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		var err error
		q.Project, err = filepath.Abs(q.Project)
		if err != nil {
			return nil, err
		}
		if err = ValidateSpec(q); err != nil {
			return nil, err
		}
		return map[string]any{"spec": q, "revision": Digest(q)}, nil
	case "validate-worker":
		var q struct {
			Role            string         `json:"role"`
			Result          WorkerJudgment `json:"result"`
			Checks          []Check        `json:"checks"`
			NativeDirectory string         `json:"nativeDirectory,omitempty"`
			NativeSource    map[string]any `json:"nativeSource,omitempty"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		var facts []nativeToolFact
		if q.NativeDirectory != "" || q.NativeSource != nil {
			var err error
			facts, err = nativeResultToolFacts(q.NativeDirectory, q.NativeSource, q.Result)
			if err != nil {
				return nil, err
			}
		}
		result, err := workerJudgmentResult(q.Role, q.Result, q.Checks, facts...)
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": result, "needs_repair": NeedsRepair(result)}, nil
	case "resolve-snapshot", "snapshot", "clean":
		var q workspaceInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		if e.Operation == "snapshot" {
			return snapshot(q)
		}
		if e.Operation == "clean" {
			return map[string]bool{"clean": true}, clean(q.Root)
		}
		head, err := vcs.New(q.Root, nil).Resolve()
		return map[string]string{"candidate": head}, err
	case "binding":
		var q bindingInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		b, err := scopeBinding(q)
		return map[string]string{"binding": b}, err
	case "certify":
		var q sealInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		return certify(q)
	case "digest-bytes":
		var q struct {
			Text string `json:"text"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		h := sha256.Sum256([]byte(q.Text))
		return map[string]string{"digest": hex.EncodeToString(h[:])}, nil
	case "supervisor-process":
		var q struct {
			Directory     string `json:"directory"`
			RequestDigest string `json:"requestDigest"`
			Cancel        bool   `json:"cancel"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		value, err := readJSONFile(filepath.Join(q.Directory, "process.json"))
		if err != nil {
			return nil, err
		}
		if textOf(getOf(value, "request_digest")) != q.RequestDigest {
			return nil, fmt.Errorf("supervisor process differs from its request")
		}
		alive := supervisorAlive(q.Directory)
		if alive && q.Cancel {
			pid, pgid := intOf(getOf(value, "pid")), intOf(getOf(value, "pgid"))
			actual, err := nativeProcessGroupID(pid)
			if err != nil || pgid <= 0 || actual != pgid {
				return nil, fmt.Errorf("supervisor has no matching process group")
			}
			process, err := os.FindProcess(pid)
			if err != nil {
				return nil, err
			}
			terminateJob(&exec.Cmd{Process: process}, pgid)
		}
		return map[string]any{"alive": alive}, nil
	case "verify-process":
		// The caller reads the two JSON documents; the verification rules live
		// here. A refusal carries its family so the wrapper can raise the same
		// exception class the reference implementation raised.
		var q struct {
			Directory string            `json:"directory"`
			Process   json.RawMessage   `json:"process"`
			Receipt   json.RawMessage   `json:"receipt"`
			Request   json.RawMessage   `json:"request"`
			Proofs    []orderedProofDTO `json:"proofs"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		process, err := decodeObject(q.Process)
		if err != nil {
			return nil, err
		}
		receipt, err := decodeObject(q.Receipt)
		if err != nil {
			return nil, err
		}
		request, err := decodeObject(q.Request)
		if err != nil {
			return nil, err
		}
		proofs, err := decodeProofs(q.Proofs)
		if err != nil {
			return nil, err
		}
		kind, refusal := VerifyProcess(q.Directory, process, receipt, request, proofs)
		if refusal != "" {
			return map[string]any{"ok": false, "kind": kind, "refusal": refusal}, nil
		}
		return map[string]any{"ok": true}, nil
	case "verify-files":
		var q struct {
			Files []orderedProofDTO `json:"files"`
			Label string            `json:"label"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		files, err := decodeProofs(q.Files)
		if err != nil {
			return nil, err
		}
		kind, refusal := VerifyFiles(files, q.Label)
		if refusal != "" {
			return map[string]any{"ok": false, "kind": kind, "refusal": refusal}, nil
		}
		return map[string]any{"ok": true}, nil
	case "comparison-plan":
		// Only the parse + representation rules live here. Git invocation,
		// file I/O, tamper checks and proof collection stay in Python.
		var q comparisonPlanInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		return planComparison(q)
	case "task-input":
		// The wrapper shape and the canonical digest live here. File I/O
		// and the callers stay in Python; the digest is computed in-line.
		var q taskInputInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		return planTaskInput(q)
	case "evidence-feedback":
		// The wrapper shape and the revision-binding judgement live here.
		// A refusal is a typed business outcome inside the value:
		// Python's refuse() maps it back to the HarnessError family.
		var q evidenceFeedbackInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		return planEvidenceFeedback(q)
	case "artifact-plan":
		// The content-addressing rule (ref = sha256(CanonicalJSON(value)))
		// lives here; Python keeps the artifact path, the publish-once
		// check and the atomic write. The value is decoded with number
		// fidelity so integer literals keep arbitrary precision and the
		// produced canonical bytes match the Python side's
		// hashlib.sha256(canonical(value)) byte for byte.
		var q artifactPlanInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		value, err := decodeValue(q.Value)
		if err != nil {
			return nil, err
		}
		canonical, ref, err := ArtifactPlan(value)
		if err != nil {
			return nil, err
		}
		return map[string]any{"canonical": string(canonical), "ref": ref}, nil
	case "artifact-reference":
		// The reference FORMAT rule lives here. It is a typed business
		// refusal inside the value so Python's refuse() can raise
		// the HarnessError family. This call
		// runs BEFORE any filesystem access so the format verdict is structural.
		var q struct {
			Ref string `json:"ref"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		ok, refusal := VerifyArtifactReference(q.Ref)
		if !ok {
			return map[string]any{"ok": false, "kind": "binding", "refusal": refusal}, nil
		}
		return map[string]any{"ok": true}, nil
	case "artifact-binding":
		// The integrity rule lives here, over the RAW bytes of the file at
		// path, which Go reads itself. It does NOT re-check the reference format
		// — that is owned by artifact-reference — so the refusal ordering is
		// structural.
		//
		// The path, not the body, crosses this boundary because the request
		// envelope has one fixed input ceiling shared by every operation
		// (Bridge): carrying the body here would make READ capacity smaller
		// than WRITE capacity by the transport's own expansion factor, so an
		// artifact publication accepted could not be read back. A bounded
		// request keeps read capacity above whatever publication accepts. A
		// path that cannot be read is an integrity refusal, not a new error
		// class: the caller has already read the same file for the returned
		// value and for the OS error classes that read belongs to.
		var q struct {
			Ref  string `json:"ref"`
			Path string `json:"path"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		ok, refusal := VerifyArtifactBindingFile(q.Ref, q.Path)
		if !ok {
			return map[string]any{"ok": false, "kind": "binding", "refusal": refusal}, nil
		}
		return map[string]any{"ok": true}, nil
	case "native-capture", "native-verify-binding", "native-verify-source", "native-reconcile":
		// The native lifecycle operations. Business refusals are typed
		// outcomes inside the value (same convention as verify-files); a
		// malformed payload or an unexpected error is a bridge failure. The
		// binding, source and process arguments are raw JSON: their shape is
		// judged by the adapter under Python semantics, not by a typed struct
		// here.
		switch e.Operation {
		case "native-capture":
			var q struct {
				Provider string `json:"provider"`
			}
			if err := StrictDecode(e.Payload, &q); err != nil {
				return nil, err
			}
			binding, err := nativeCapture(q.Provider)
			if value, handled := nativeOutcome(err); handled {
				return value, nil
			}
			if err != nil {
				return nil, err
			}
			return map[string]any{"binding": binding}, nil
		case "native-verify-binding":
			var q struct {
				Provider string          `json:"provider"`
				Binding  json.RawMessage `json:"binding"`
			}
			if err := StrictDecode(e.Payload, &q); err != nil {
				return nil, err
			}
			if err := nativeVerifyBinding(q.Provider, q.Binding); err != nil {
				if value, handled := nativeOutcome(err); handled {
					return value, nil
				}
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		case "native-verify-source":
			var q struct {
				Provider  string          `json:"provider"`
				Directory string          `json:"directory"`
				Source    json.RawMessage `json:"source"`
			}
			if err := StrictDecode(e.Payload, &q); err != nil {
				return nil, err
			}
			if err := nativeVerifySource(q.Provider, q.Directory, q.Source); err != nil {
				if value, handled := nativeOutcome(err); handled {
					return value, nil
				}
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		default: // native-reconcile
			var q struct {
				Provider  string          `json:"provider"`
				Directory string          `json:"directory"`
				Process   json.RawMessage `json:"process"`
			}
			if err := StrictDecode(e.Payload, &q); err != nil {
				return nil, err
			}
			source, err := nativeReconcile(q.Provider, q.Directory, q.Process)
			if value, handled := nativeOutcome(err); handled {
				return value, nil
			}
			if err != nil {
				return nil, err
			}
			return map[string]any{"source": source}, nil
		}
	case "canonical", "digest":
		// The canonical encoding is the content-addressing format, so it is
		// owned here rather than in the caller. Numbers are decoded with
		// UseNumber so integer literals keep arbitrary precision and float
		// literals can be re-rendered with Python's rules.
		var q struct {
			Value json.RawMessage `json:"value"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		value, err := decodeValue(q.Value)
		if err != nil {
			return nil, err
		}
		if e.Operation == "canonical" {
			encoded, err := CanonicalJSON(value)
			if err != nil {
				return nil, err
			}
			return map[string]any{"canonical": string(encoded)}, nil
		}
		digest, err := CanonicalDigest(value)
		if err != nil {
			return nil, err
		}
		return map[string]any{"digest": digest}, nil
	case "file-proof":
		// A missing or unreadable file is a typed refusal, not a broken bridge,
		// for the same reason result framing refusals are: the caller has to be
		// able to tell "this evidence is gone" from "the bridge failed".
		var q struct {
			Path string `json:"path"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		proof, refusal := FileProof(q.Path)
		if refusal != "" {
			return map[string]any{"ok": false, "refusal": refusal}, nil
		}
		return map[string]any{"ok": true, "proof": proof}, nil
	case "frame-result":
		// A refusal is a typed business outcome, so it travels inside the value
		// rather than as a bridge failure: the failure shape is reserved for
		// malformed operations and must keep meaning "the bridge itself failed".
		var q struct {
			Text string `json:"text"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		result, refusal := FrameResult(q.Text)
		if refusal != "" {
			return map[string]any{"accepted": false, "refusal": refusal}, nil
		}
		return map[string]any{"accepted": true, "result": result}, nil
	case "evaluate-route":
		// The repair-or-seal judgement lives here. Python selects the full
		// records by the returned ids/gate names and assembles the feedback
		// artifact, so the blocking condition exists once, in NeedsRepair.
		// The map is the operation's one wire projection of the typed verdict.
		var q evaluateRouteInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		verdict := evaluateRoute(q)
		return map[string]any{
			"failed_checks": verdict.FailedChecks, "blocking_gates": verdict.BlockingGates, "route": verdict.Route,
		}, nil
	case "worker-outcome":
		// The invoke_worker branch selection lives here; the loop
		// mechanics (continuation, asks, attempt counters) stay in Python.
		var q workerOutcomeInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		return map[string]any{"action": workerOutcome(q)}, nil
	case "check-retention":
		// The incremental-check eligibility lives here as two phases over
		// one operation: without evidence_valid it judges eligibility, with it
		// the retain/fail/rerun verdict. Python still owns verify_evidence so
		// the original exception can be re-raised on the first attempt. The
		// projection spells exactly the keys each branch defines.
		var q checkRetentionInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		verdict := checkRetention(q)
		answer := map[string]any{"action": verdict.Action}
		if verdict.Action == "retain" {
			answer["inherited_from"] = verdict.InheritedFrom
		}
		if verdict.Reason != "" {
			answer["reason"] = verdict.Reason
		}
		return answer, nil
	case "repair-allowance":
		// The repair-limit boundary lives here. The increment after the
		// user continues is a flow decision and stays in Python.
		var q repairAllowanceInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		verdict := repairAllowance(q)
		return map[string]any{"allowance": verdict.Allowance, "ask_user": verdict.AskUser}, nil
	case "candidate-admission":
		// The candidate admission refusals live here. A refusal is a
		// typed business outcome inside the value so Python's
		// refuse() raises the HarnessError family.
		var q candidateAdmissionInput
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		if verdict := candidateAdmission(q); !verdict.Admitted {
			return map[string]any{"ok": false, "kind": verdict.Kind, "refusal": verdict.Refusal}, nil
		}
		return map[string]any{"ok": true}, nil
	case "archive-inventory", "archive-destination", "archive-settled", "archive-index-plan",
		"archive-verify", "archive-stale", "archive-projection",
		"prune-admission", "prune-bind", "prune-marker", "prune-plan":
		// The terminal-retention judgements live here. Locking, the
		// SQLite backup, zip reading and writing, hashing, process probing and
		// the deletions stay in Python; these ops own admission, ordering and
		// the removal plan. Inventories and proofs cross in chunked batches or
		// as small facts, never whole. Every retention refusal is
		// HarnessError family, so the typed outcome carries only the refusal
		// text and retention.py raises it directly (infra.refuse is not used).
		return retentionDispatch(e.Operation, e.Payload)
	default:
		return nil, fmt.Errorf("unknown harness operation %q", e.Operation)
	}
}
