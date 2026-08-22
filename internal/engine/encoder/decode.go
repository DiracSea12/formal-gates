package encoder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
)

// decode.go 承载 Decode 的 wire→IR 方向：单步重组与 payload 严格解析。
// payload 判别按公共头 kind 路由（与 encode 侧 switch 对称），每变体单独
// DisallowUnknownFields 解析——closed world 不被未知字段穿透（spike 坑 3）。

// stepFromWire 把单步 wire 重组为 CompiledStep。version 是制品信封版本，
// 用于步骤版本绑定（checkCoherence 复核其与信封一致）。
func stepFromWire(version string, sw *stepWire) (*compiler.CompiledStep, error) {
	kind := compiler.StepKind(sw.Kind)
	if _, ok := kindProfiles[kind]; !ok {
		return nil, fmt.Errorf("encoder: step %q: unknown kind %q", sw.ID, sw.Kind)
	}
	cs := &compiler.CompiledStep{
		Header: compiler.CompiledHeader{
			ID:                authoring.StepID(sw.ID),
			NodeID:            authoring.NodeID(sw.NodeID),
			Ordinal:           sw.Ordinal,
			Kind:              kind,
			DefinitionVersion: authoring.DefinitionVersion(sw.DefinitionVersion),
			Authority:         authoring.DecisionAuthority(sw.Authority),
			Runner:            authoring.RunnerKind(sw.Runner),
		},
	}
	if len(sw.Dependencies) > 0 {
		cs.Header.Dependencies = stepIDs(sw.Dependencies)
	}
	// human/parallel 制品中不携带 io 键；可执行变体必带（缺失即非制品形态）。
	if sw.IO != nil {
		switch kind {
		case compiler.KindHumanAsk, compiler.KindParallel:
			return nil, fmt.Errorf("encoder: step %q: kind %s must not carry an io block", sw.ID, kind)
		}
		cs.IO = ioFromWire(sw.IO)
	} else {
		switch kind {
		case compiler.KindLocal, compiler.KindDurable, compiler.KindHostAction, compiler.KindAgent:
			return nil, fmt.Errorf("encoder: step %q: kind %s requires an io block", sw.ID, kind)
		}
	}
	payload, err := payloadFromWire(sw)
	if err != nil {
		return nil, err
	}
	cs.Payload = payload
	return cs, nil
}

func ioFromWire(w *ioWire) authoring.IO {
	out := authoring.IO{InputCodec: authoring.CodecID(w.InputCodec), OutputCodec: authoring.CodecID(w.OutputCodec)}
	if len(w.Preconditions) > 0 {
		out.Preconditions = predicateRefs(w.Preconditions)
	}
	if len(w.Postconditions) > 0 {
		out.Postconditions = predicateRefs(w.Postconditions)
	}
	if len(w.Inputs) > 0 {
		out.Inputs = make([]authoring.InputBinding, 0, len(w.Inputs))
		for _, b := range w.Inputs {
			out.Inputs = append(out.Inputs, authoring.InputBinding{
				From: authoring.StepID(b.From), OutputField: b.Output, ToField: b.To,
			})
		}
	}
	return out
}

func predicateRefs(wire []predicateWire) []authoring.PredicateRef {
	out := make([]authoring.PredicateRef, 0, len(wire))
	for _, p := range wire {
		out = append(out, authoring.PredicateRef{ID: authoring.PredicateID(p.ID), Negated: p.Negated})
	}
	return out
}

func stepIDs(ids []string) []authoring.StepID {
	out := make([]authoring.StepID, 0, len(ids))
	for _, id := range ids {
		out = append(out, authoring.StepID(id))
	}
	return out
}

// payloadFromWire 是 decode 侧唯一变体感知点：按 kind 严格解析 payload。
// raw 必须是 JSON 对象；每变体单独 DisallowUnknownFields。
func payloadFromWire(sw *stepWire) (compiler.Payload, error) {
	raw := sw.Payload
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("encoder: step %q: payload must be a %s object, got null", sw.ID, sw.Kind)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	bad := func(err error) error {
		return fmt.Errorf("encoder: step %q: decode %s payload: %w", sw.ID, sw.Kind, err)
	}
	switch compiler.StepKind(sw.Kind) {
	case compiler.KindLocal:
		var w localPayloadWire
		if err := dec.Decode(&w); err != nil {
			return nil, bad(err)
		}
		return localPayloadFromWire(&w), nil
	case compiler.KindDurable:
		var w durablePayloadWire
		if err := dec.Decode(&w); err != nil {
			return nil, bad(err)
		}
		return compiler.CompiledDurableStep{
			Handler: authoring.HandlerID(w.Handler), Idempotency: authoring.IdempotencyKeyStrategy(w.Idempotency),
			Reconcile: authoring.ReconcileID(w.Reconcile), Timeout: time.Duration(w.TimeoutNs),
			Retry: authoring.RetryPolicy{MaxAttempts: w.Retry.MaxAttempts, Backoff: time.Duration(w.Retry.BackoffNs)},
		}, nil
	case compiler.KindHostAction:
		var w hostPayloadWire
		if err := dec.Decode(&w); err != nil {
			return nil, bad(err)
		}
		return compiler.CompiledHostActionStep{
			Handler: authoring.HandlerID(w.Handler), Boundary: authoring.HostBoundaryReason(w.Boundary),
			Operation: authoring.OperationID(w.Operation), Timeout: time.Duration(w.TimeoutNs),
		}, nil
	case compiler.KindAgent:
		var w agentPayloadWire
		if err := dec.Decode(&w); err != nil {
			return nil, bad(err)
		}
		return agentPayloadFromWire(&w), nil
	case compiler.KindHumanAsk:
		var w humanPayloadWire
		if err := dec.Decode(&w); err != nil {
			return nil, bad(err)
		}
		return compiler.CompiledHumanAskStep{
			AskKind: w.AskKind, RequestSchema: authoring.SchemaID(w.RequestSchema),
			ResponseSchema: authoring.SchemaID(w.ResponseSchema), FreshnessTTL: time.Duration(w.FreshnessTtlNs),
		}, nil
	case compiler.KindParallel:
		var w parallelPayloadWire
		if err := dec.Decode(&w); err != nil {
			return nil, bad(err)
		}
		return compiler.CompiledParallelStep{
			Children: stepIDs(w.Children),
			Join:     authoring.JoinPolicy{JoinStep: authoring.StepID(w.Join.JoinStep), Mode: authoring.JoinMode(w.Join.Mode)},
			Failure:  authoring.FailurePolicy{Mode: authoring.ParallelFailureMode(w.Failure.Mode), Escalate: authoring.FailureClass(w.Failure.Escalate)},
		}, nil
	default:
		return nil, fmt.Errorf("encoder: step %q: unknown kind %q", sw.ID, sw.Kind)
	}
}

func localPayloadFromWire(w *localPayloadWire) compiler.CompiledLocalStep {
	return compiler.CompiledLocalStep{
		Handler: authoring.HandlerID(w.Handler), Timeout: time.Duration(w.TimeoutNs), Retry: retryFromWire(w.Retry),
	}
}

func agentPayloadFromWire(w *agentPayloadWire) compiler.CompiledAgentStep {
	return compiler.CompiledAgentStep{
		Handler: authoring.HandlerID(w.Handler), Reason: authoring.NonProgrammableReason(w.Reason),
		Timeout: time.Duration(w.TimeoutNs), Retry: retryFromWire(w.Retry),
	}
}

func retryFromWire(w *retryWire) *authoring.RetryPolicy {
	if w == nil {
		return nil
	}
	r := authoring.RetryPolicy{MaxAttempts: w.MaxAttempts, Backoff: time.Duration(w.BackoffNs)}
	return &r
}
