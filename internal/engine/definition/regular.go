package definition

import (
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"time"
)

// The closed regular table binds intake and candidate preparation handlers.
// The controller groups dynamic Batch and QA tasks through existing TaskKeys.
const RegularVersion authoring.DefinitionVersion = "5"

func RegularWorkflow() *compiler.Definition {
	var steps []authoring.Step
	previous := ""
	for _, item := range []struct {
		id   string
		kind string
	}{
		{"intake.precheck", "local"}, {"intake.confirm", "ask"}, {"intake.mode", "ask"},
		{"product-review", "agent"}, {"product.adjudicate", "ask"},
		{"start-readiness", "agent"}, {"technical.adjudicate", "ask"},
		{"topology.confirm", "ask"}, {"route.confirm", "ask"}, {"development.ready", "ask"},
		{"development-worker", "agent"}, {"blackbox-design", "agent"}, {"blackbox-review", "agent"}, {"whitebox-design", "agent"}, {"whitebox-review", "agent"}, {"git.prepare", "host"}, {"review.ready", "ask"}, {"blackbox-execution", "agent"}, {"whitebox-execution", "agent"}, {"complexity-gate", "agent"}, {"implementation-quality-gate", "agent"}, {"repair-worker", "agent"},
	} {
		deps := []string{}
		if previous != "" {
			deps = append(deps, previous)
		}
		h := header(item.id, "entry", deps...)
		h.DefinitionVersion = RegularVersion
		var step authoring.Step
		var err error
		switch item.kind {
		case "local":
			step, err = authoring.NewLocalStep(h, ioWith(deps...), authoring.LocalSpec{Handler: "engine.intake.precheck"})
		case "agent":
			reviewIO := ioWith(deps...)
			reviewIO.Postconditions = []authoring.PredicateRef{{ID: "pred.review.post"}}
			step, err = authoring.NewAgentStep(h, reviewIO, authoring.AgentSpec{Handler: authoring.HandlerID("engine." + item.id), Reason: authoring.ReasonIndependentReview, Timeout: 30 * time.Minute, Retry: &authoring.RetryPolicy{MaxAttempts: 2}})
		case "host":
			step, err = authoring.NewHostActionStep(h, ioWith(deps...), authoring.HostActionSpec{Handler: "engine.git.prepare", Boundary: authoring.BoundaryAgentDispatchAPI, Operation: "git.prepare", Schema: "schema.git.prepare", Timeout: 30 * time.Minute})
		case "ask":
			step, err = authoring.NewHumanAskStep(h, authoring.HumanAskSpec{AskKind: "regular", RequestSchema: "schema.regular.request", ResponseSchema: "schema.regular.response", FreshnessTTL: 15 * time.Minute})
		}
		must(item.id, err)
		steps = append(steps, step)
		previous = item.id
	}
	return &compiler.Definition{Version: RegularVersion, EntryNode: entryNode, Steps: steps}
}

func registerRegular(reg *compiler.Registry) {
	for _, op := range []authoring.OperationID{"model.run", "qa.prepare", "qa.target", "qa.collect"} {
		must(op, reg.RegisterOperation(op))
	}
	must("schema.task.operation", reg.RegisterSchema("schema.task.operation"))
	must("developer.replace", reg.RegisterOperation("developer.replace"))
	must("schema.developer.recovery", reg.RegisterSchema("schema.developer.recovery"))
	must("git.prepare", reg.RegisterOperation("git.prepare"))
	must("engine.git.prepare", reg.RegisterHandler("engine.git.prepare", authoring.RunnerHostAdapter))
	must("schema.git.prepare", reg.RegisterSchema("schema.git.prepare"))
	must("regular", reg.RegisterAskKind("regular"))
	must("schema.regular.request", reg.RegisterSchema("schema.regular.request"))
	must("schema.regular.response", reg.RegisterSchema("schema.regular.response"))
	must("engine.intake.precheck", reg.RegisterHandler("engine.intake.precheck", authoring.RunnerEngineLocal))
	for _, id := range []authoring.HandlerID{"engine.product-review", "engine.start-readiness", "engine.development-worker", "engine.blackbox-design", "engine.blackbox-review", "engine.whitebox-design", "engine.whitebox-review", "engine.blackbox-execution", "engine.whitebox-execution", "engine.complexity-gate", "engine.implementation-quality-gate", "engine.repair-worker"} {
		must(id, reg.RegisterHandler(id, authoring.RunnerAgentWorker))
	}
}
