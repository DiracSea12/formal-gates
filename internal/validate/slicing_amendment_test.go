package validate

import (
	"strings"
	"testing"
)

// TestSlicingUserConfirmedAmendmentNoToSplit drives the full amendment flow on a
// run that declared --split no at start: without user confirmation the split
// decision stays rejected (pointing at --user-confirm instead of a restart);
// with confirmation and an amendment reason the decision is recorded, the run
// self-promotes to retained-overall (no restart, no overall-review redo) and
// takes the merge route; the binding point (no re-cut) is unchanged.
func TestSlicingUserConfirmedAmendmentNoToSplit(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "amend-no-split"))
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	if _, err := RecordSlicing(root, pkg, state.RunID, "split", 2, []string{"slice-a", "slice-b"}, "slice-a and slice-b can run in parallel", "reason", ""); err == nil || !strings.Contains(err.Error(), "--user-confirm") {
		t.Fatalf("split on a --split no run without user confirmation was accepted: %v", err)
	}
	if _, err := RecordSlicing(root, pkg, state.RunID, "split", 2, []string{"slice-a", "slice-b"}, "slice-a and slice-b can run in parallel", "", "", SlicingAmendOptions{UserConfirm: true}); err == nil || !strings.Contains(err.Error(), "amendment reason") {
		t.Fatalf("amendment without a reason note was accepted: %v", err)
	}
	recorded, err := RecordSlicing(root, pkg, state.RunID, "split", 2, []string{"slice-a", "slice-b"}, "slice-a and slice-b can run in parallel", "tech review revealed two independently verifiable units", "", SlicingAmendOptions{UserConfirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !recorded.RetainedOverall || recorded.SplitDeclaration != "yes" || recorded.SplitAmendment == "" {
		t.Fatalf("amendment did not promote the run to retained-overall: retained=%v declaration=%q amendment=%q", recorded.RetainedOverall, recorded.SplitDeclaration, recorded.SplitAmendment)
	}
	if recorded.Slicing == nil || recorded.Slicing.Decision != "split" || recorded.RouteMode != "merge" {
		t.Fatalf("amended split decision was not recorded on the merge route: %#v", recorded.Slicing)
	}
	if _, err := RecordSlicing(root, pkg, state.RunID, "no-split", 0, nil, "", "re-cut", ""); err == nil || !strings.Contains(err.Error(), "already recorded") {
		t.Fatalf("amended slicing decision was re-cut: %v", err)
	}
}

// TestSlicingUserConfirmedAmendmentRetainedToNoSplit asserts the retained-run
// dead end is unlocked by the user-confirmed demotion: no-split without
// confirmation stays rejected; with confirmation and a reason the run is
// demoted to a plain no-split run and development is no longer blocked.
func TestSlicingUserConfirmedAmendmentRetainedToNoSplit(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStartRetained(t, root, pkg, "amend-retained"))
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	if _, err := RecordSlicing(root, pkg, state.RunID, "no-split", 0, nil, "", "reason", ""); err == nil || !strings.Contains(err.Error(), "--user-confirm") {
		t.Fatalf("no-split on a retained run without user confirmation was accepted: %v", err)
	}
	recorded, err := RecordSlicing(root, pkg, state.RunID, "no-split", 0, nil, "", "single coherent bounded unit; no split needed", "", SlicingAmendOptions{UserConfirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if recorded.RetainedOverall || recorded.SplitDeclaration != "no" || recorded.SplitAmendment == "" {
		t.Fatalf("amendment did not demote the retained run: retained=%v declaration=%q amendment=%q", recorded.RetainedOverall, recorded.SplitDeclaration, recorded.SplitAmendment)
	}
	recorded = setRoute(t, root, pkg, recorded, "full", nil)
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("development stayed blocked after the user-confirmed demotion: %v", err)
	}
}

// TestSlicingAmendmentDoesNotUnlockDeclaredSlice asserts a slice instance
// started with --split yes --master cannot drift away from its master, even
// with the user-confirmed amendment flag.
func TestSlicingAmendmentDoesNotUnlockDeclaredSlice(t *testing.T) {
	root, pkg := workflowFixture(t)
	master := sliceMaster(t, root, pkg, "amend-slice-master")
	slice := confirmRequirement(t, root, pkg, mustStartSlice(t, root, pkg, "amend-slice", master))
	if _, err := RecordSlicing(root, pkg, slice.RunID, "no-split", 0, nil, "", "reason", "", SlicingAmendOptions{UserConfirm: true}); err == nil || !strings.Contains(err.Error(), "--master") {
		t.Fatalf("declared slice instance was allowed to drift from its master: %v", err)
	}
}
