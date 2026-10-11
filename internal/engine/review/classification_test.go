package review

import (
	"reflect"
	"strings"
	"testing"
)

func TestTechnicalFindingClassificationAtCandidateAndCorrection(t *testing.T) {
	for _, category := range []string{"", "unknown", "requirement-conflict", "responsibility-dependency", "acceptance-entry", "implementation-detail"} {
		for _, severity := range []string{"P0", "P1", "P2", "P3"} {
			t.Run(category+"/"+severity, func(t *testing.T) {
				validCategory := category != "" && category != "unknown"
				valid := validCategory && !(category == "implementation-detail" && (severity == "P0" || severity == "P1"))
				base := NewSet()
				saved := cloneSet(t, base)
				got, _, err := RecordCandidate(base, "run", StartReadiness, Dispatch{ID: "dispatch", RequirementRevision: "revision"}, Result{Status: "FAIL", Findings: []Finding{{Category: category, Severity: severity, Message: "documented acceptance concern"}}}, verifiedByOperator())
				if (err == nil) != valid {
					t.Fatalf("candidate accepted=%v want=%v: %v", err == nil, valid, err)
				}
				if !reflect.DeepEqual(base, saved) {
					t.Fatal("candidate changed original series")
				}
				if !valid {
					return
				}
				if got.StartReadiness.Completed != 1 || len(got.StartReadiness.RawCandidates) != 1 {
					t.Fatal("valid finding did not occupy exactly one semantic round")
				}
				wantStatus := "PASS"
				if severity == "P0" || severity == "P1" {
					wantStatus = "FAIL"
				}
				if got.StartReadiness.Effective.Status != wantStatus {
					t.Fatal("candidate classification projected the wrong status", got.StartReadiness.Effective)
				}
				set, candidate := mustRecord(t, NewSet(), "run", StartReadiness, Dispatch{ID: "dispatch", RequirementRevision: "revision"}, Result{Status: "PASS", Findings: []Finding{{Category: category, Severity: "P2", Message: "documented acceptance concern"}}})
				for _, adjudicated := range []bool{false, true} {
					current := cloneSet(t, set)
					if adjudicated {
						current = mustApply(t, current, "run", "revision", CorrectionInput{Action: StartReadiness, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: "revision", FindingID: candidate.Findings[0].ID, Decision: "CONFIRM", Reason: "verified concern", Source: "user", UserAuthorized: true})
					}
					original := cloneSet(t, current)
					input := CorrectionInput{Action: StartReadiness, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: "revision", FindingID: candidate.Findings[0].ID, Decision: "RESEVERITIZE", Severity: severity, SeverityRationale: "The documented entry has the reported observed impact", Reason: "verified impact", Source: "user", UserAuthorized: true, ExpectedEffectiveRevision: current.StartReadiness.Effective.Revision, Supersedes: LatestDecisionID(current.StartReadiness, candidate.ResultDigest, candidate.Findings[0].ID)}
					corrected, err := ApplyCorrection(current, "run", "revision", input)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(current, original) {
						t.Fatal("correction changed original series")
					}
					if corrected.StartReadiness.Completed != 1 || len(corrected.StartReadiness.RawCandidates) != 1 {
						t.Fatal("correction consumed a semantic round")
					}
					if corrected.StartReadiness.Effective.Status != wantStatus {
						t.Fatal("correction classification projected the wrong status", corrected.StartReadiness.Effective)
					}
					if err := Validate("run", corrected); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestImplementationDetailCannotBecomeABlockerByCorrection(t *testing.T) {
	set, candidate := mustRecord(t, NewSet(), "run", StartReadiness, Dispatch{ID: "dispatch", RequirementRevision: "revision"}, Result{Status: "PASS", Findings: []Finding{{Category: "implementation-detail", Severity: "P2", Message: "helper duplicates an existing native capability"}}})
	for _, adjudicated := range []bool{false, true} {
		current := cloneSet(t, set)
		if adjudicated {
			current = mustApply(t, current, "run", "revision", CorrectionInput{Action: StartReadiness, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: "revision", FindingID: candidate.Findings[0].ID, Decision: "CONFIRM", Reason: "verified concern", Source: "user", UserAuthorized: true})
		}
		for _, severity := range []string{"P0", "P1"} {
			original := cloneSet(t, current)
			_, err := ApplyCorrection(current, "run", "revision", CorrectionInput{Action: StartReadiness, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: "revision", FindingID: candidate.Findings[0].ID, Decision: "RESEVERITIZE", Severity: severity, SeverityRationale: "Observed concern in the documented entry", Reason: "verified impact", Source: "user", UserAuthorized: true, ExpectedEffectiveRevision: current.StartReadiness.Effective.Revision, Supersedes: LatestDecisionID(current.StartReadiness, candidate.ResultDigest, candidate.Findings[0].ID)})
			if err == nil || !strings.Contains(err.Error(), "implementation-detail is at most P2") {
				t.Fatalf("expected classification rejection: %v", err)
			}
			if !reflect.DeepEqual(current, original) {
				t.Fatal("rejected correction changed original series")
			}
		}
	}
}
