package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/requirement"
	"os"
	"path/filepath"
)

// initialGuarantee freezes activation only at the explicit initial confirmation.
// A structured document observed later cannot create this authority.
func initialGuarantee(root, route string, receipt IntakeConfirmationReceipt) (*protocol.GuaranteeActivation, error) {
	if route != "regular" || len(receipt.Artifacts) != 1 {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(root, receipt.RequirementSource))
	if err != nil {
		return nil, err
	}
	doc, err := requirement.ParseRequirementProjection(receipt.RequirementSource, data)
	if err != nil {
		return nil, nil
	}
	return protocol.NewGuaranteeActivation(receipt.RequirementRevision, receipt.SolutionRevision, doc), nil
}

// materializeSliceGuarantee publishes only evidence, outside the verified VCS
// candidate. Readback proves the complete frozen map and final master evidence.
func (f *Facade) materializeSliceGuarantee(s *protocol.State) (*TerminalMaterial, error) {
	if s.SliceChild != nil || s.ChildCreation == nil || !s.GuaranteeActive() {
		return nil, nil
	}
	if !s.FinalValidationComplete(f.definition) {
		return nil, fmt.Errorf("active slice guarantee requires complete final FULL validation")
	}
	receipts := make([]protocol.SliceReceipt, 0, len(s.Topology.Slices))
	for _, slice := range s.Topology.Slices {
		r, ok := s.SliceReceipts[s.SliceChildren[slice]]
		if !ok || r.Receipt.Status != "SLICE_READY" || !s.SliceReceiptCurrent(r) {
			return nil, fmt.Errorf("active slice guarantee lacks current successful receipt %s", slice)
		}
		receipts = append(receipts, r)
	}
	value := struct {
		RunID      string                        `json:"runId"`
		Activation *protocol.GuaranteeActivation `json:"activation"`
		Receipts   []protocol.SliceReceipt       `json:"receipts"`
		Validation *protocol.Validation          `json:"validation"`
	}{s.RunID, s.RequirementGuarantee, receipts, s.Validation}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(f.stateDir, "slice-guarantee.json")
	if err = f.admission.write(func() error { return writeJSONAtomic(path, value) }); err != nil {
		return nil, err
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var loaded any
	var expected any
	if err = json.Unmarshal(actual, &loaded); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &expected); err != nil {
		return nil, err
	}
	a, _ := json.Marshal(loaded)
	b, _ := json.Marshal(expected)
	if string(a) != string(b) {
		return nil, fmt.Errorf("slice guarantee readback differs from frozen evidence")
	}
	sum := sha256.Sum256(actual)
	return &TerminalMaterial{Path: path, Digest: hex.EncodeToString(sum[:])}, nil
}
