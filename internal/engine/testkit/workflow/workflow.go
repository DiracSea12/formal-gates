// Package workflow provides the six-step-kind protocol test graph.
package workflow

import (
	"fmt"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/encoder"
)

const Version authoring.DefinitionVersion = "3"

const entryNode authoring.NodeID = "entry"

var (
	workflowHandlers = []struct {
		id     authoring.HandlerID
		runner authoring.RunnerKind
	}{
		{"engine.entry.parse", authoring.RunnerEngineLocal},
		{"engine.entry.persist", authoring.RunnerDurableActivity},
		{"engine.review.worker", authoring.RunnerAgentWorker},
		{"engine.fan.slice", authoring.RunnerEngineLocal},
		{"engine.fan.transport", authoring.RunnerHostAdapter},
		{"engine.fan.join", authoring.RunnerEngineLocal},
		{"engine.report.cost", authoring.RunnerEngineLocal},
	}
	workflowCodecs     = []authoring.CodecID{"codec.any.in", "codec.any.out"}
	workflowPredicates = []authoring.PredicateID{"pred.review.post"}
	workflowReconciles = []authoring.ReconcileID{"reconcile.entry.persist"}
	workflowSchemas    = []authoring.SchemaID{"schema.ask.decision.request", "schema.ask.decision.response", "schema.candidate.review-range"}
	workflowOperations = []authoring.OperationID{"op.fan.transport"}
	workflowAskKinds   = []authoring.AskKindID{"decision"}
)

func Registry() *compiler.Registry {
	reg := compiler.NewRegistry()
	for _, h := range workflowHandlers {
		must(h.id, reg.RegisterHandler(h.id, h.runner))
	}
	for _, id := range workflowCodecs {
		must(id, reg.RegisterCodec(id))
	}
	for _, id := range workflowPredicates {
		must(id, reg.RegisterPredicate(id))
	}
	for _, id := range workflowReconciles {
		must(id, reg.RegisterReconciler(id))
	}
	for _, id := range workflowSchemas {
		must(id, reg.RegisterSchema(id))
	}
	for _, id := range workflowOperations {
		must(id, reg.RegisterOperation(id))
	}
	for _, id := range workflowAskKinds {
		must(id, reg.RegisterAskKind(id))
	}
	return reg
}

func Workflow() *compiler.Definition {
	parse, err := authoring.NewLocalStep(header("entry.parse", "entry"), ioWith(),
		authoring.LocalSpec{Handler: "engine.entry.parse"})
	must("entry.parse", err)
	persist, err := authoring.NewDurableStep(header("entry.persist", "entry", "entry.parse"),
		ioWith("entry.parse"), authoring.DurableSpec{
			Handler: "engine.entry.persist", Idempotency: authoring.IdempotencyDeterministicInput,
			Reconcile: "reconcile.entry.persist", Timeout: 30 * time.Second,
			Retry: authoring.RetryPolicy{MaxAttempts: 3, Backoff: 2 * time.Second},
		})
	must("entry.persist", err)
	review, err := authoring.NewAgentStep(header("review.worker", "review", "entry.persist"),
		reviewIOWith("entry.persist"), authoring.AgentSpec{
			Handler: "engine.review.worker", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: authoring.ReviewSeriesProductReview, Timeout: time.Minute,
		})
	must("review.worker", err)
	readiness, err := authoring.NewAgentStep(header("review.readiness", "review", "review.worker"),
		reviewIOWith("review.worker"), authoring.AgentSpec{
			Handler: "engine.review.worker", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: authoring.ReviewSeriesStartReadiness, Timeout: time.Minute,
		})
	must("review.readiness", err)
	ask, err := authoring.NewHumanAskStep(header("ask.decide", "ask", "entry.persist"),
		authoring.HumanAskSpec{
			AskKind: "decision", RequestSchema: "schema.ask.decision.request",
			ResponseSchema: "schema.ask.decision.response", FreshnessTTL: 15 * time.Minute,
		})
	must("ask.decide", err)
	split, err := authoring.NewParallelStep(header("fan.split", "fan", "entry.parse"),
		authoring.ParallelSpec{
			Children: []authoring.StepID{"fan.transport", "fan.slice"},
			Join:     authoring.JoinPolicy{JoinStep: "fan.join", Mode: authoring.JoinAll},
			Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
		})
	must("fan.split", err)
	slice, err := authoring.NewLocalStep(header("fan.slice", "fan", "fan.split"),
		ioWith("fan.split"), authoring.LocalSpec{Handler: "engine.fan.slice"})
	must("fan.slice", err)
	transport, err := authoring.NewHostActionStep(header("fan.transport", "fan", "fan.split"),
		ioWith("fan.split"), authoring.HostActionSpec{
			Handler: "engine.fan.transport", Boundary: authoring.BoundaryAgentDispatchAPI,
			Operation: "op.fan.transport", Schema: "schema.candidate.review-range", Timeout: 10 * time.Second,
		})
	must("fan.transport", err)
	join, err := authoring.NewLocalStep(header("fan.join", "fan", "fan.slice", "fan.transport"),
		ioWith("fan.slice", "fan.transport"), authoring.LocalSpec{Handler: "engine.fan.join"})
	must("fan.join", err)
	cost, err := authoring.NewLocalStep(header("report.cost", "report", "fan.join", "ask.decide"),
		ioWith("fan.join", "ask.decide"), authoring.LocalSpec{
			Handler: "engine.report.cost", Retry: &authoring.RetryPolicy{MaxAttempts: 2},
		})
	must("report.cost", err)
	return &compiler.Definition{Version: Version, EntryNode: entryNode,
		Steps: []authoring.Step{parse, persist, review, readiness, ask, split, slice, transport, join, cost}}
}

func must(id any, err error) {
	if err != nil {
		panic(fmt.Sprintf("definition: %v: %v", id, err))
	}
}

func header(id, node string, deps ...string) authoring.Header {
	ds := make([]authoring.StepID, 0, len(deps))
	for _, d := range deps {
		ds = append(ds, authoring.StepID(d))
	}
	return authoring.Header{
		ID:                authoring.StepID(id),
		NodeID:            authoring.NodeID(node),
		Dependencies:      ds,
		DefinitionVersion: Version,
	}
}

func ioWith(bindings ...string) authoring.IO {
	inputs := make([]authoring.InputBinding, 0, len(bindings))
	for _, b := range bindings {
		inputs = append(inputs, authoring.InputBinding{
			From: authoring.StepID(b), OutputField: "out", ToField: "in",
		})
	}
	return authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out", Inputs: inputs}
}

func reviewIOWith(bindings ...string) authoring.IO {
	review := ioWith(bindings...)
	review.Postconditions = []authoring.PredicateRef{{ID: "pred.review.post"}}
	return review
}

const WorkflowDefinitionVersion = string(Version)

var WorkflowDefinitionDigest = func() string {
	cd, err := compiler.Compile(Workflow(), Registry())
	if err != nil {
		panic(err)
	}
	data, err := encoder.Encode(cd)
	if err != nil {
		panic(err)
	}
	return encoder.Digest(data)
}()
