//go:build !windows

package harness

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

const canarySplitRequirement = `# 交付

## 需求点

### REQ-001：独立文本转换与组合命令

#### 要求
frob 接受一个词；参数不足以非零退出。正常输出为一行 frob: UPPER bytes=N，其中 UPPER 是 strings.ToUpper 的结果，N 是输入词的 UTF-8 字节数。入口 frob.go 已接线 UpperWord 与 WordSize。当前 uppercase.go 的 UpperWord 原样返回，size.go 的 WordSize 固定返回 0，这两个已知占位必须实现。两个独立开发批次分别只修改 uppercase.go 和 size.go；分别验证 go build ./...，交付对应函数。uppercase.go 的批次归 uppercase 片，size.go 的批次归 word-size 片。组合验收 AC-003 由保留 master-merge 负责。
#### 验收条件
- AC-001：UpperWord 将输入词转换为 Unicode 大写；空词返回空串。
- AC-002：WordSize 返回输入词的 UTF-8 字节数；空词返回 0。
- AC-003：frob héLLo 输出精确 frob: HÉLLO bytes=6，frob 空词输出精确 frob:  bytes=0。
#### 来源
用户要求。
`
const canarySplitMain = `package main
import("fmt";"os")
func main(){if len(os.Args)<2{fmt.Fprintln(os.Stderr,"usage: frob <word>");os.Exit(2)};fmt.Printf("frob: %s bytes=%d\n",UpperWord(os.Args[1]),WordSize(os.Args[1]))}
`

func canarySplitTopology() protocol.TopologyDecision {
	return protocol.TopologyDecision{Decision: "split", SplitCount: 2, Slices: []string{"uppercase", "word-size"}, Parallel: "independent functions and concurrent merge case preparation", ACOwners: []protocol.ACOwner{{AC: "AC-001", Owner: "uppercase"}, {AC: "AC-002", Owner: "word-size"}, {AC: "AC-003", Owner: "master-merge"}}}
}
func (c *engineCanary) currentRunID() string { return firstNonEmpty(c.run.RunID, "engine-canary") }
func (c *engineCanary) dispatchKey(id string) string {
	if c.run.RunID == "" {
		return id
	}
	return c.run.RunID + "/" + id
}
func (c *engineCanary) jobDirectory(id string) string {
	if c.run.RunID == "" {
		return filepath.Join(c.jobs, canaryName(id))
	}
	return filepath.Join(c.jobs, c.run.RunID, canaryName(id))
}
func (c *engineCanary) caseKey(id string) string { return c.dispatchKey(id) }
func (c *engineCanary) childSlice() string       { return c.slice }
func canaryPending(run facade.Run, id string) *protocol.PendingAction {
	pending := run.PendingActions[id]
	return &pending
}
func (c *engineCanary) materials(id, role string, input protocol.ValidationInput) canaryMaterials {
	return canaryMaterials{Role: role, RunID: c.currentRunID(), Slice: c.childSlice(), ActionID: id, Input: &input, RequirementPath: input.RequirementDocument.Path, RequirementRevision: input.Binding.RequirementRevision, Pending: canaryPending(c.run, id)}
}
func canaryCaseMode(input protocol.ValidationInput) coverage.CaseMode {
	if input.Manifest.Binding.ReviewKind == coverage.ReviewMerge {
		return coverage.CaseMerge
	}
	return coverage.CaseBlackbox
}
func (c *engineCanary) confirmRequirement() {
	revision, document := c.run.Review.RequirementRevision, string(c.doc)
	if c.run.Validation != nil && c.run.Validation.Document.Content != "" {
		document = c.run.Validation.Document.Content
	}
	c.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
}
func (c *engineCanary) sliceProposal() *protocol.GranularityReview {
	if c.masterPlan == nil {
		c.fail("child route has no accepted master proposal")
	}
	artifact := map[string]string{"uppercase": "uppercase.go", "word-size": "size.go"}[c.childSlice()]
	proposal := *c.masterPlan
	proposal.Batches = nil
	for _, batch := range c.masterPlan.Batches {
		if slices.Contains(batch.Artifacts, artifact) {
			proposal.Batches = append(proposal.Batches, batch)
		}
	}
	if len(proposal.Batches) != 1 || len(proposal.Batches[0].Artifacts) != 1 {
		c.fail("master proposal does not assign one independent artifact to %s", c.childSlice())
	}
	return &proposal
}

// Content authority is exactly the original conflict paths. Native staging,
// commit and candidate freezing remain in the adapter.
func (c *engineCanary) conflictResult(id string, input protocol.ValidationInput) protocol.ValidationResult {
	if input.Conflict == nil || len(input.Conflict.Evidence.Conflicts) == 0 {
		c.fail("conflict dispatch lacks original conflicts")
	}
	materials := c.materials(id, "merge conflict resolution", input)
	var answer protocol.ConflictResolution
	expected := []string{}
	for _, conflict := range input.Conflict.Evidence.Conflicts {
		expected = append(expected, conflict.Path)
	}
	slices.Sort(expected)
	shape := func(raw map[string]any) error {
		if err := decodeCanaryAnswer(raw, &answer); err != nil {
			return err
		}
		actual := slices.Clone(answer.Paths)
		slices.Sort(actual)
		if !slices.Equal(actual, expected) || strings.TrimSpace(answer.Summary) == "" {
			return fmt.Errorf("resolution must declare exactly the original conflict paths and summary")
		}
		return nil
	}
	c.turnIn(c.workspace(input), id, materials.Role, c.prompt(materials.Role, materials, "Resolve content only in input.conflict.evidence.conflicts[].path using the original incoming and prefix. Do not stage, commit or edit other paths. Report every conflict path and a summary.", false), canarySchema([]string{"paths", "summary"}, map[string]any{"paths": canaryArr(canaryStr()), "summary": canaryStr()}), false, shape)
	return protocol.ValidationResult{Binding: input.Binding, Status: protocol.OutcomePass, Resolution: &answer}
}

func TestEngineGitSplitCanary(t *testing.T) {
	if os.Getenv("FORMAL_GATES_ENGINE_CANARY") != "1" {
		t.Skip("engine canary: set FORMAL_GATES_ENGINE_CANARY=1 (real host calls, real spend)")
	}
	c := &engineCanary{t: t, split: true, deadline: time.Now().Add(canaryRunBudget), caseArgs: map[string][]string{}, caseTests: map[string]string{}, approvalInput: bufio.NewReader(os.Stdin), approvalOutput: os.Stdout, execute: zcodeExecute}
	_, file, _, _ := runtime.Caller(0)
	stable, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.prepareHome(os.Getenv("ZCODE_DATA_BASE_DIR"), stable, filepath.Dir(filepath.Dir(filepath.Dir(file)))); err != nil {
		t.Fatal(err)
	}
	resources := filepath.Join(c.home, ".formal-gates-resources")
	if err := os.MkdirAll(resources, 0700); err != nil {
		t.Fatal(err)
	}
	c.project, err = os.MkdirTemp(resources, "engine-canary-")
	if err != nil {
		t.Fatal(err)
	}
	evidence := os.Getenv("FORMAL_GATES_ENGINE_CANARY_DIR")
	if evidence == "" {
		evidence = filepath.Join(c.home, "canary-evidence", fmt.Sprintf("engine-canary-%d", time.Now().UnixNano()))
	}
	for name, root := range c.separateRoots {
		if canaryPathWithin(root, evidence) || canaryPathWithin(evidence, root) {
			t.Fatalf("evidence overlaps %s", name)
		}
	}
	c.jobs = filepath.Join(evidence, "jobs")
	if err := os.MkdirAll(c.jobs, 0700); err != nil {
		t.Fatal(err)
	}
	base, intake := c.buildSplitFixture()
	c.install(base, intake)
	c.drive()
	c.loop()
	c.assertSplitTerminal(base)
	t.Logf("ordinary split live evidence: %s", evidence)
}
func (c *engineCanary) buildSplitFixture() (string, facade.IntakeConfirmationReceipt) {
	c.initFixtureRepository()
	files := map[string]string{".gitignore": ".gates/\n", "go.mod": "module frob\n\ngo 1.22\n", "requirements.md": canarySplitRequirement, "frob.go": canarySplitMain, "uppercase.go": "package main\nfunc UpperWord(word string)string{return word}\n", "size.go": "package main\nfunc WordSize(word string)int{return 0}\n"}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(c.project, name), []byte(body), 0600); err != nil {
			c.fail("fixture %s: %v", name, err)
		}
	}
	c.doc = []byte(canarySplitRequirement)
	sum := sha256.Sum256(c.doc)
	c.revision = hex.EncodeToString(sum[:])
	solution := sha256.Sum256([]byte(canarySplitMain))
	solutionRevision := hex.EncodeToString(solution[:])
	intake := facade.IntakeConfirmationReceipt{Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport, RequirementSource: "requirements.md", RequirementRevision: c.revision, Artifacts: []facade.IntakeArtifact{{Path: "requirements.md", Revision: c.revision}, {Path: "frob.go", Revision: solutionRevision}}, SolutionRevision: solutionRevision, SolutionDigest: "sha256:" + solutionRevision}
	c.git("add", "--all")
	c.git("commit", "-m", "independent command functions")
	return c.git("rev-parse", "HEAD"), intake
}

func (c *engineCanary) assertSplitTerminal(base string) {
	final := c.terminalRun()
	if final.Status != "COMPLETE" || final.Termination == nil || !final.Termination.ResourcesCleaned() {
		c.fail("split has no complete cleanup proof")
	}
	terminal := final.Termination
	if len(terminal.SliceChildren) != 2 || len(terminal.SliceReceipts) != 2 {
		c.fail("terminal lost released child map or receipts")
	}
	head, tree := c.git("rev-parse", "HEAD"), c.git("rev-parse", "HEAD^{tree}")
	if terminal.CandidatePromotion == nil || terminal.CandidatePromotion.Final != head || terminal.CandidatePromotion.FinalDigest != tree || terminal.CandidatePromotion.SourceDigest != tree || final.PromotedSnapshot != head {
		c.fail("promotion does not bind actual final tree")
	}
	if c.git("rev-list", "--count", base+".."+head) != "1" || c.git("status", "--porcelain") != "" {
		c.fail("promotion is not one clean organized commit")
	}
	for _, path := range []string{"uppercase.go", "size.go"} {
		if !strings.Contains(c.git("diff", "--name-only", base, head), path) {
			c.fail("final candidate misses %s", path)
		}
	}
	for slice, runID := range terminal.SliceChildren {
		receipt := terminal.SliceReceipts[runID]
		child := receipt.Receipt
		if receipt.Digest != child.CanonicalDigest() || child.ChildRunID != runID || child.MasterRunID != final.RunID || child.SliceID != slice || child.Envelope != final.Envelope || child.RequirementRevision != c.revision || child.Status != "SLICE_READY" || child.Validation.Status != "COMPLETE" || len(child.SelectedQA) != 0 || len(child.ApprovedCases) != 0 {
			c.fail("child %s has inconsistent frozen receipt", slice)
		}
		expected := []string{"correctness", "regression"}
		if slice == "word-size" {
			expected = []string{"correctness"}
			if child.RouteMode != "custom" {
				c.fail("custom child route changed")
			}
		}
		actualGates := slices.Clone(child.SelectedGates)
		slices.Sort(actualGates)
		if !slices.Equal(actualGates, expected) {
			c.fail("child selected gates %v, want %v", child.SelectedGates, expected)
		}
		gates := map[string]bool{}
		for _, result := range child.Validation.Results {
			if result.Target.Kind == "GATE" && result.Target.Status == protocol.OutcomePass {
				gates[result.Target.Gate] = true
			}
			if result.Target.Kind == "QA" {
				c.fail("ordinary child QA ran")
			}
		}
		for _, gate := range expected {
			if !gates[gate] {
				c.fail("child omitted selected gate %s", gate)
			}
		}
		if child.Cost == nil {
			c.fail("child cost evidence missing")
		}
		for action, entry := range child.Cost.Dispatches {
			if actual, ok := final.Cost.Dispatches[runID+"/"+action]; !ok || !canaryValuesEqual(actual, entry) {
				c.fail("namespaced child cost differs for %s/%s", runID, action)
			}
		}
		for _, name := range []string{"state.json", "slice-receipt.json"} {
			if _, err := os.Stat(filepath.Join(c.project, ".gates", "engine", runID, name)); !os.IsNotExist(err) {
				c.fail("child checkpoint remains %s", name)
			}
		}
		if _, err := os.Stat(filepath.Join(c.project, ".gates", "tmp", runID, "lifecycle", "active")); !os.IsNotExist(err) {
			c.fail("child lifecycle active residue")
		}
	}
	set := terminal.Validation.Sets[coverage.ReviewMerge]
	if set == nil || set.Status != "FROZEN" || len(set.Manifest.Cases) == 0 {
		c.fail("merge cases not frozen")
	}
	casesPath := ".gates/results/" + final.RunID + ".merge-cases.md"
	if c.git("show", head+":"+casesPath) != strings.TrimSpace(protocol.MergeCasesMarkdown(final.RunID, set.Manifest)) {
		c.fail("final merge artifact differs from approved manifest")
	}
	wave := terminal.Validation.Waves[len(terminal.Validation.Waves)-1]
	mergeGate, mergeQA := false, false
	for _, target := range wave.Targets {
		if target.Status != protocol.OutcomePass {
			c.fail("final target %s did not pass", target.Task.String())
		}
		mergeGate = mergeGate || target.Kind == "GATE" && target.Gate == "merge-gate"
		mergeQA = mergeQA || target.Kind == "QA" && target.Mode == coverage.ReviewMerge && target.Execution != nil
	}
	if !mergeGate || !mergeQA {
		c.fail("master omitted mandatory merge gate or merge QA")
	}
	binary := c.buildCandidate(c.project, "final-probe")
	for _, sample := range []struct{ word, output string }{{"héLLo", "frob: HÉLLO bytes=6"}, {"", "frob:  bytes=0"}} {
		probe := c.observe("composed final command", c.project, []string{binary, sample.word})
		if probe.ExitCode != 0 || probe.Output != sample.output {
			c.fail("combined candidate mismatch: %+v", probe)
		}
	}
	if strings.Count(c.git("worktree", "list", "--porcelain"), "worktree ") != 1 {
		c.fail("native worktree residue")
	}
	for _, resource := range terminal.NativeResources {
		if _, err := os.Stat(resource.Path); !os.IsNotExist(err) {
			c.fail("native resource remains %s", resource.Path)
		}
		if c.git("for-each-ref", "--format=%(objectname)", resource.Ref) != "" {
			c.fail("native resource ref remains")
		}
	}
	c.assertTerminalReplay(final)
	for _, key := range c.dispatched {
		runID, action, _ := strings.Cut(key, "/")
		ledgerKey := action
		if runID != final.RunID {
			ledgerKey = key
		}
		if _, ok := final.Cost.Dispatches[ledgerKey]; !ok {
			c.fail("dispatch cost missing %s", key)
		}
	}
	c.t.Logf("ordinary split completed: run=%s tree=%s package=%s installed=%s dispatches=%d", final.RunID, tree, c.installed.PackageDigest, c.installed.Targets[0].InstalledDigest, len(c.dispatched))
}
func canaryValuesEqual(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
