package lifecycle

import (
	"testing"

	"formal-gates/internal/host"
)

func TestNativeLifecycleRetainsBoundIdentityAcrossRepeatedObservations(t *testing.T) {
	for _, descriptor := range host.All() {
		if !descriptor.Installable {
			continue
		}
		t.Run(descriptor.ID, func(t *testing.T) {
			root := t.TempDir()
			if err := BindDispatchWithProvider(root, "run", "dispatch", "identity", descriptor.ID); err != nil {
				t.Fatal(err)
			}
			if err := ObserveNativeStart(root, "run", "dispatch", descriptor.ID, "identity"); err != nil {
				t.Fatal(err)
			}
			start, err := VerifyDispatch(root, "run", "dispatch")
			if err != nil || start.Outcome != Rejected || !start.StartObserved || start.StopObserved || start.Diagnostic != "missing matching stop event" {
				t.Fatal(start, err)
			}
			for range 2 {
				if err := ObserveNativeCompletion(root, "run", "dispatch", descriptor.ID, "identity"); err != nil {
					t.Fatal(err)
				}
				got, err := VerifyDispatch(root, "run", "dispatch")
				if err != nil || got.Outcome != Verified || got.Provider != descriptor.ID || got.Identity != "identity" || !got.StartObserved || !got.StopObserved {
					t.Fatal(got, err)
				}
			}
			for _, event := range []string{eventStart, eventStop} {
				record, err := readEvent(runEventPath(root, "run", "dispatch", event))
				if err != nil || record.Provider != descriptor.ID || record.Event != event || record.Identity != "identity" {
					t.Fatal(record, err)
				}
			}
			if err := ObserveNativeCompletion(root, "run", "dispatch", descriptor.ID, "different"); err == nil {
				t.Fatal("different identity accepted")
			}
			if err := BindDispatchWithProvider(root, "run", "dispatch", "different", descriptor.ID); err == nil {
				t.Fatal("binding identity changed")
			}
		})
	}
}

func TestNativeLifecycleRequiresOriginalBinding(t *testing.T) {
	root := t.TempDir()
	if err := ObserveNativeStart(root, "run", "dispatch", host.Codex, "identity"); err == nil {
		t.Fatal("unbound observation accepted")
	}
	if err := BindDispatchWithProvider(root, "run", "dispatch", "identity", host.Codex); err != nil {
		t.Fatal(err)
	}
	if err := ObserveNativeCompletion(root, "run", "dispatch", host.Claude, "identity"); err == nil {
		t.Fatal("different provider accepted")
	}
	got, err := VerifyDispatch(root, "run", "dispatch")
	if err != nil || got.StartObserved || got.StopObserved || got.Outcome != Rejected || got.Diagnostic != "missing matching start and stop event" {
		t.Fatal(got, err)
	}
}

func TestRunLifecycleActiveMarkerEndsIdempotently(t *testing.T) {
	root := t.TempDir()
	for range 2 {
		if err := BeginRun(root, "run"); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := ActiveRunIDs(root)
	if err != nil || len(ids) != 1 || ids[0] != "run" {
		t.Fatal(ids, err)
	}
	for range 2 {
		if err := EndRun(root, "run"); err != nil {
			t.Fatal(err)
		}
	}
	ids, err = ActiveRunIDs(root)
	if err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
}

func TestLifecycleUnavailableProviderDoesNotInventEvents(t *testing.T) {
	root := t.TempDir()
	if err := BindDispatchWithProvider(root, "run", "dispatch", "identity", host.Default); err != nil {
		t.Fatal(err)
	}
	got, err := VerifyDispatch(root, "run", "dispatch")
	if err != nil || got.Outcome != Unavailable || got.StartObserved || got.StopObserved {
		t.Fatal(got, err)
	}
}
