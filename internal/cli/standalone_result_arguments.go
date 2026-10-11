package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strconv"

	"formal-gates/internal/engine/worker"
)

type standaloneResultArguments struct {
	fs           *flag.FlagSet
	provided     bool
	complete     bool
	message      string
	interruption *string
	findings     []worker.Finding
	finding      *worker.Finding
	location     *worker.Location
}

func registerStandaloneResultArguments(fs *flag.FlagSet) *standaloneResultArguments {
	a := &standaloneResultArguments{fs: fs}
	fs.BoolVar(&a.complete, "review-complete", false, "the independent review was completed")
	text := func(name, help string, set func(string) error) {
		fs.Func(name, help, func(value string) error {
			a.provided = true
			return set(value)
		})
	}
	text("message", "review summary", func(value string) error { a.message = value; return nil })
	text("interruption-reason", "actual reason the review could not be performed", func(value string) error { a.interruption = &value; return nil })
	text("finding-severity", "start a finding with severity P0, P1, P2 or P3 (repeatable)", func(value string) error {
		a.findings = append(a.findings, worker.Finding{Severity: value})
		a.finding = &a.findings[len(a.findings)-1]
		a.location = nil
		return nil
	})
	findingText := func(name, help string, set func(*worker.Finding, string)) {
		text(name, help, func(value string) error {
			if a.finding == nil {
				return fmt.Errorf("--%s requires a preceding --finding-severity", name)
			}
			set(a.finding, value)
			return nil
		})
	}
	findingText("finding-message", "finding description", func(f *worker.Finding, v string) { f.Message = v })
	findingText("finding-severity-rationale", "severity rationale", func(f *worker.Finding, v string) { f.SeverityRationale = v })
	findingText("finding-category", "finding category", func(f *worker.Finding, v string) { f.Category = v })
	findingText("finding-requirement", "confirmed requirement evidence", func(f *worker.Finding, v string) { f.Requirement = v })
	findingText("finding-normal-path", "normal public reproduction path", func(f *worker.Finding, v string) { f.NormalPath = v })
	findingText("finding-actual-error", "observed error", func(f *worker.Finding, v string) { f.ActualError = v })
	findingText("finding-deferral-impact", "consequence of deferring the finding", func(f *worker.Finding, v string) { f.DeferralImpact = v })
	findingText("finding-verify-scope", "scope verification evidence", func(f *worker.Finding, v string) { f.Verification.Scope = v })
	findingText("finding-verify-severity", "severity verification evidence", func(f *worker.Finding, v string) { f.Verification.Severity = v })
	findingText("finding-verify-causality", "causality verification evidence", func(f *worker.Finding, v string) { f.Verification.Causality = v })
	findingText("finding-verify-reproduction", "reproduction verification evidence", func(f *worker.Finding, v string) { f.Verification.Reproduction = v })
	findingText("finding-verify-classification", "classification verification evidence", func(f *worker.Finding, v string) { f.Verification.Classification = v })
	for _, name := range []string{"finding-read", "finding-listing", "finding-line", "finding-entry"} {
		text(name, "original published source position for the current finding", func(value string) error {
			position, err := strconv.Atoi(value)
			if err != nil || position < 1 {
				return fmt.Errorf("--%s requires a positive displayed position", name)
			}
			if a.finding == nil {
				return fmt.Errorf("--%s requires a preceding --finding-severity", name)
			}
			if name == "finding-read" || name == "finding-listing" {
				a.finding.Locations = append(a.finding.Locations, worker.Location{})
				a.location = &a.finding.Locations[len(a.finding.Locations)-1]
			} else if a.location == nil {
				return fmt.Errorf("--%s requires a preceding source selector", name)
			}
			switch name {
			case "finding-read":
				a.location.Read = position
			case "finding-listing":
				a.location.Listing = position
			case "finding-line":
				a.location.Line = position
			case "finding-entry":
				a.location.Entry = position
			}
			return nil
		})
	}
	return a
}

func (a *standaloneResultArguments) input() ([]byte, bool, error) {
	a.fs.Visit(func(f *flag.Flag) {
		if f.Name == "review-complete" {
			a.provided = true
		}
	})
	if !a.provided {
		return nil, false, nil
	}
	var findings *[]worker.Finding
	if a.complete || len(a.findings) != 0 {
		if a.findings == nil {
			a.findings = []worker.Finding{}
		}
		findings = &a.findings
	}
	value := struct {
		Message            string            `json:"message,omitempty"`
		Findings           *[]worker.Finding `json:"findings,omitempty"`
		InterruptionReason *string           `json:"interruptionReason,omitempty"`
	}{a.message, findings, a.interruption}
	body, err := json.Marshal(value)
	return body, true, err
}
