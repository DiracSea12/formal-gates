package protocol

import (
	"bytes"
	"reflect"
	"testing"

	"formal-gates/internal/engine/decision"
)

func TestProjectionMatchesReadOnlyViewsAcrossRestart(t *testing.T) {
	t.Parallel()
	engine, dir := workEngine(t, "develop", "verify")
	action := issueReady(t, engine)[0]
	submitPlainResult(t, engine, "develop-completed", action)
	input := Observation{Facts: decision.Observation{Facts: []decision.Fact{{Source: decision.SourceHost, Key: "host", Value: "available"}}}}
	before := stateBytesOf(t, dir)
	projection, err := engine.Project(input)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := engine.LoadMustSucceed(t)
	plan, revision, err := engine.Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	work, err := engine.RequirementWork()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projection.Snapshot, snapshot) || revision != snapshot.Revision || !reflect.DeepEqual(projection.Plan, plan) || !reflect.DeepEqual(projection.RequirementWork, work) {
		t.Fatal("projection differs from the current snapshot, next boundary or work inventory")
	}
	restarted, err := New(mustStore(t, dir), engine.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		repeated, err := restarted.Project(input)
		if err != nil || !reflect.DeepEqual(projection, repeated) {
			t.Fatalf("reopened projection changed: %v", err)
		}
	}
	if !bytes.Equal(before, stateBytesOf(t, dir)) {
		t.Fatal("read-only projection changed the durable state")
	}
}

func TestProjectionKeepsOneRevisionDuringConcurrentProgress(t *testing.T) {
	t.Parallel()
	engine, _ := workEngine(t, "develop", "verify")
	action := issueReady(t, engine)[0]
	if _, err := engine.Submit(spawnReceipt(t, "develop-spawned", action, "developer"), Observation{}); err != nil {
		t.Fatal(err)
	}
	before, err := engine.Project(Observation{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewWorkerResultEvent("develop-completed", action, testProvider, OutcomePass, "sha256:development", "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var progressErr error
	go func() {
		_, progressErr = engine.Submit(result, Observation{})
		close(done)
	}()
	defer func() {
		<-done
		if progressErr != nil {
			t.Error(progressErr)
		}
	}()
	for range 20 {
		projection, err := engine.Project(Observation{})
		if err != nil {
			t.Fatal(err)
		}
		view := projection.Snapshot.State.decisionView(engine.cfg.Definition)
		digest, err := view.Digest()
		if err != nil || projection.Plan.StateDigest != digest {
			t.Fatalf("next boundary belongs to a different state: %v", err)
		}
		for _, work := range projection.RequirementWork {
			if work.ActionID == action && work.ResultDigest != projection.Snapshot.State.Results[action].Digest {
				t.Fatal("work inventory belongs to a different state")
			}
		}
	}
	<-done
	after, err := engine.Project(Observation{})
	if err != nil {
		t.Fatal(err)
	}
	if after.Snapshot.Revision <= before.Snapshot.Revision || after.Snapshot.State.Results[action].Digest == "" {
		t.Fatal("fresh projection did not observe committed progress")
	}
	if before.Snapshot.State.Results[action].Digest != "" {
		t.Fatal("concurrent progress mutated an earlier projection")
	}
}
