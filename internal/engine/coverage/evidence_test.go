package coverage

import "testing"

func TestExecutionEvidenceRequiresFieldsWithoutInterpretingProse(t *testing.T) {
	if err := ValidateEvidence("command", "./command", "ok", "matched"); err != nil {
		t.Fatalf("valid short command output was classified semantically: %v", err)
	}
	for _, fields := range [][3]string{{"", "ok", "matched"}, {"./command", " ", "matched"}, {"./command", "ok", ""}} {
		if err := ValidateEvidence("command", fields[0], fields[1], fields[2]); err == nil {
			t.Fatal("missing execution field was accepted")
		}
	}
}
