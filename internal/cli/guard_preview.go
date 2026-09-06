package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"formal-gates/internal/engine/guard"
)

// REQ-014 explicitly requires this public acceptance entry. It is independent of
// run loading/admission and delegates all allowance and digest work to production
// guard.Calculate. Success is a non-authoritative preview, not runtime enforcement.
func runWorkflowGuardPreview(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow guard-preview", streams)
	input := fs.String("input", "", "basis JSON file (non-authoritative, read-only calculation)")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if *input == "" || fs.NArg() != 0 {
		return 1, fmt.Errorf("workflow guard-preview requires --input <basis.json> and no positional arguments")
	}
	file, err := os.Open(*input)
	if err != nil {
		return 1, fmt.Errorf("guard-preview input %q: %w", *input, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var basis guard.Basis
	if err := decoder.Decode(&basis); err != nil {
		return 1, fmt.Errorf("guard-preview input %q: %w", *input, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("expected exactly one JSON object")
		}
		return 1, fmt.Errorf("guard-preview input %q: %w", *input, err)
	}
	result, err := guard.Calculate(basis)
	if err != nil {
		return 1, fmt.Errorf("guard-preview input %q: %w", *input, err)
	}
	return printJSON(streams.Stdout, struct {
		guard.Result
		RuntimeIntegration string `json:"runtimeIntegration"`
	}{result, "NOT_INSTALLED"})
}
