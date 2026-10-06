//go:build !windows

package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/requirement"
)

// The authorized canary user stand-in confirms one complete requirements and
// solution document. The production bootstrap derives activation from it.
func (c *engineCanary) confirmedIntake() facade.IntakeConfirmationReceipt {
	c.t.Helper()
	if _, err := requirement.ParseRequirementProjection("requirements.md", c.doc); err != nil {
		c.fail("canonical requirement precheck: %v", err)
	}
	c.revision = canaryBytesRevision(c.doc)
	return facade.IntakeConfirmationReceipt{Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport, RequirementSource: "requirements.md", RequirementRevision: c.revision, Artifacts: []facade.IntakeArtifact{{Path: "requirements.md", Revision: c.revision}}, SolutionRevision: c.revision, SolutionDigest: "sha256:" + c.revision}
}

func TestCanaryCanonicalIntakeBindsCompleteDocument(t *testing.T) {
	for _, document := range []string{canaryRequirementDocument, canarySplitRequirement} {
		c := &engineCanary{t: t, doc: []byte(document)}
		receipt := c.confirmedIntake()
		projection, err := requirement.ParseRequirementProjection(receipt.RequirementSource, c.doc)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(projection.Content, "## 方案") || projection.Content != document || receipt.RequirementRevision != projection.ContentDigest || receipt.SolutionRevision != receipt.RequirementRevision || receipt.SolutionDigest != "sha256:"+projection.ContentDigest || len(receipt.Artifacts) != 1 || receipt.Artifacts[0].Revision != projection.ContentDigest {
			t.Fatal("complete requirement/solution identity changed")
		}
	}
}

func (c *engineCanary) assertCanonicalGuarantee(final facade.Run) {
	c.t.Helper()
	terminal := final.Termination
	projection, err := requirement.ParseRequirementProjection("requirements.md", c.doc)
	if err != nil {
		c.fail("confirmed terminal document: %v", err)
	}
	expected := protocol.NewGuaranteeActivation(c.revision, c.revision, projection)
	receipt := c.confirmedIntake()
	receipt.Digest, err = facade.IntakeDigest(receipt)
	if err != nil {
		c.fail("canonical confirmation digest: %v", err)
	}
	if terminal == nil || !terminal.GuaranteeActive() || !canaryValuesEqual(terminal.RequirementGuarantee, expected) || terminal.IntakeConfirmationReceipt == nil || !canaryValuesEqual(*terminal.IntakeConfirmationReceipt, receipt) {
		c.fail("final full canary omitted its actual initial guarantee activation")
	}
	var summary facade.TerminalSummary
	data, err := os.ReadFile(final.SummaryPath)
	if err != nil || json.Unmarshal(data, &summary) != nil || summary.Termination == nil || !canaryValuesEqual(summary.Termination.RequirementGuarantee, terminal.RequirementGuarantee) {
		c.fail("immutable summary lost initial guarantee identity: %v", err)
	}
	if !c.split {
		if summary.SliceGuarantee != nil {
			c.fail("no-split produced a slice guarantee without child receipts")
		}
		return
	}
	if summary.SliceGuarantee == nil {
		c.fail("active retained master omitted slice guarantee material")
	}
	actual, err := os.ReadFile(summary.SliceGuarantee.Path)
	if err != nil {
		c.fail("slice guarantee material: %v", err)
	}
	var material struct {
		RunID      string                        `json:"runId"`
		Activation *protocol.GuaranteeActivation `json:"activation"`
		Receipts   []protocol.SliceReceipt       `json:"receipts"`
		Validation *protocol.Validation          `json:"validation"`
	}
	if err := json.Unmarshal(actual, &material); err != nil {
		c.fail("slice guarantee material decode: %v", err)
	}
	var receipts []protocol.SliceReceipt
	for _, slice := range terminal.Topology.Slices {
		receipts = append(receipts, terminal.SliceReceipts[terminal.SliceChildren[slice]])
	}
	digest := sha256.Sum256(actual)
	if material.RunID != final.RunID || !canaryValuesEqual(material.Activation, terminal.RequirementGuarantee) || !canaryValuesEqual(material.Receipts, receipts) || !canaryValuesEqual(material.Validation, terminal.Validation) || summary.SliceGuarantee.Digest != hex.EncodeToString(digest[:]) {
		c.fail("slice guarantee bytes differ from activation/current child checkpoints/final validation")
	}
	c.t.Logf("actual canonical intake guarantee: revision=%s active=true material=%s digest=%s", c.revision, summary.SliceGuarantee.Path, summary.SliceGuarantee.Digest)
}

// Concluded split evidence can be checked without rerunning its native flow.
func TestCanaryCanonicalGuaranteeReadsConcludedEvidence(t *testing.T) {
	root := os.Getenv("FORMAL_GATES_CANARY_LOCAL_DIR")
	if root == "" {
		t.Skip("concluded canary evidence directory required")
	}
	project := filepath.Join(root, "split", "home", ".formal-gates-resources", "project")
	path := filepath.Join(project, ".gates", "engine", "engine-canary", "terminal-summary.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var final facade.Run
	if err := json.Unmarshal(data, &final); err != nil {
		t.Fatal(err)
	}
	final.SummaryPath = path
	document, err := os.ReadFile(filepath.Join(project, "requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	c := &engineCanary{t: t, split: true, doc: document, revision: canaryBytesRevision(document)}
	c.assertCanonicalGuarantee(final)
}
