package compiler

import (
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
)

// wantErr 匹配错误消息子串：子串取自目标分支的 distinguishing 文案，确保
// 命中的是目标分支而不是偶然的非 nil 错误。
func wantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("want error containing %q, got %q", substr, err.Error())
	}
}

// registry 三类拒绝（缺失/重复/kind mismatch）+ 合法解析。跨类复用同一 ID
// 在注册期拒绝（单一命名空间），槽位错用在解析期报 kind 不匹配。
func TestRegistryRegister(t *testing.T) {
	t.Run("empty id", func(t *testing.T) {
		reg := NewRegistry()
		wantErr(t, reg.RegisterHandler("", authoring.RunnerEngineLocal), "empty id")
		wantErr(t, reg.RegisterPredicate(""), "empty id")
		wantErr(t, reg.RegisterCodec(""), "empty id")
		wantErr(t, reg.RegisterReconciler(""), "empty id")
		wantErr(t, reg.RegisterSchema(""), "empty id")
		wantErr(t, reg.RegisterOperation(""), "empty id")
		wantErr(t, reg.RegisterAskKind(""), "empty id")
	})
	t.Run("duplicate same kind", func(t *testing.T) {
		reg := NewRegistry()
		if err := reg.RegisterHandler("engine.dup", authoring.RunnerEngineLocal); err != nil {
			t.Fatalf("first register: %v", err)
		}
		wantErr(t, reg.RegisterHandler("engine.dup", authoring.RunnerDurableActivity), `duplicate id "engine.dup"`)
	})
	t.Run("duplicate cross kind", func(t *testing.T) {
		// 跨类复用同一 ID 同样是注册期重复：单一命名空间不按 kind 分桶。
		reg := NewRegistry()
		if err := reg.RegisterHandler("engine.cross", authoring.RunnerEngineLocal); err != nil {
			t.Fatalf("first register: %v", err)
		}
		for _, row := range []struct {
			name string
			reg  func() error
		}{
			{"predicate", func() error { return reg.RegisterPredicate("engine.cross") }},
			{"codec", func() error { return reg.RegisterCodec("engine.cross") }},
			{"reconciler", func() error { return reg.RegisterReconciler("engine.cross") }},
			{"schema", func() error { return reg.RegisterSchema("engine.cross") }},
			{"operation", func() error { return reg.RegisterOperation("engine.cross") }},
			{"askKind", func() error { return reg.RegisterAskKind("engine.cross") }},
		} {
			wantErr(t, row.reg(), `duplicate id "engine.cross"`)
		}
	})
	t.Run("invalid handler runner", func(t *testing.T) {
		reg := NewRegistry()
		wantErr(t, reg.RegisterHandler("engine.bad", authoring.RunnerKind("")), "invalid runner kind")
		wantErr(t, reg.RegisterHandler("engine.bad", authoring.RunnerKind("CONTROL")), "invalid runner kind")
	})
}

func TestRegistryResolve(t *testing.T) {
	t.Run("happy per kind", func(t *testing.T) {
		reg := NewRegistry()
		for _, row := range []struct {
			id     authoring.HandlerID
			runner authoring.RunnerKind
		}{
			{"engine.local.h", authoring.RunnerEngineLocal},
			{"engine.durable.h", authoring.RunnerDurableActivity},
			{"engine.host.h", authoring.RunnerHostAdapter},
			{"engine.agent.h", authoring.RunnerAgentWorker},
		} {
			if err := reg.RegisterHandler(row.id, row.runner); err != nil {
				t.Fatalf("register handler: %v", err)
			}
			got, err := reg.ResolveHandler(row.id)
			if err != nil {
				t.Fatalf("resolve %q: %v", row.id, err)
			}
			if got != row.runner {
				t.Fatalf("resolve %q = runner %q, want %q", row.id, got, row.runner)
			}
		}
		if err := reg.RegisterPredicate("pred.x"); err != nil {
			t.Fatalf("register predicate: %v", err)
		}
		if err := reg.ResolvePredicate("pred.x"); err != nil {
			t.Fatalf("resolve predicate: %v", err)
		}
	})
	t.Run("missing id", func(t *testing.T) {
		reg := NewRegistry()
		_, err := reg.ResolveHandler("engine.ghost")
		wantErr(t, err, `handler "engine.ghost" not found (closed world)`)
		wantErr(t, reg.ResolvePredicate("pred.ghost"), "not found (closed world)")
		wantErr(t, reg.ResolveCodec("codec.ghost"), "not found (closed world)")
		wantErr(t, reg.ResolveReconciler("reconcile.ghost"), "not found (closed world)")
		wantErr(t, reg.ResolveSchema("schema.ghost"), "not found (closed world)")
		wantErr(t, reg.ResolveOperation("op.ghost"), "not found (closed world)")
	})
	t.Run("kind mismatch", func(t *testing.T) {
		// 单一命名空间的收益：handler ID 填进 predicate 槽报 kind 不匹配，
		// 而不是含糊的 not found。
		reg := NewRegistry()
		if err := reg.RegisterHandler("engine.x", authoring.RunnerEngineLocal); err != nil {
			t.Fatalf("register: %v", err)
		}
		wantErr(t, reg.ResolvePredicate("engine.x"), `id "engine.x" registered as handler, want predicate`)
		wantErr(t, reg.ResolveCodec("engine.x"), "registered as handler, want codec")
		wantErr(t, reg.ResolveReconciler("engine.x"), "registered as handler, want reconciler")
		wantErr(t, reg.ResolveSchema("engine.x"), "registered as handler, want schema")
		wantErr(t, reg.ResolveOperation("engine.x"), "registered as handler, want operation")
	})
}
