package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"formal-gates/internal/host"
	"formal-gates/internal/validate"
	"io"
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
	head, err := validate.HarnessResolveSnapshot(q.Root)
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
	head, err = validate.HarnessResolveSnapshot(q.Root)
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
	head, err := validate.HarnessResolveSnapshot(q.Root)
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
	return map[string]any{"contract_version": Version, "profile": Profile, "status": status, "evidence_level": level, "candidate": q.Candidate, "revision": q.Revision, "checks": q.Checks, "gates": q.Gates, "baseline": Baseline, "full_3_5c_equivalence": false}, nil
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
	case "host-descriptors":
		var q struct{}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		return host.All(), nil
	case "schema":
		var q struct{}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		return WorkerSchema(), nil
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
			Role   string       `json:"role"`
			Result WorkerResult `json:"result"`
			Checks []Check      `json:"checks"`
		}
		if err := StrictDecode(e.Payload, &q); err != nil {
			return nil, err
		}
		if err := ValidateWorker(q.Role, q.Result, q.Checks); err != nil {
			return nil, err
		}
		return map[string]any{"result": q.Result, "needs_repair": NeedsRepair(q.Result)}, nil
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
		head, err := validate.HarnessResolveSnapshot(q.Root)
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
	default:
		return nil, fmt.Errorf("unknown harness operation %q", e.Operation)
	}
}
