package encoder_test

// 外部测试包：本文件需要 definition 包的定义表作为真实编译输入，而
// definition import encoder——包内测试会构成导入环，外部测试包不会。

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
)

// compileWorkflow 编译 definition 表（覆盖全部六变体的真实定义）。
func compileWorkflow(t *testing.T) *compiler.CompiledDefinition {
	t.Helper()
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatalf("compile workflow: %v", err)
	}
	return cd
}

func encodeWorkflow(t *testing.T) []byte {
	t.Helper()
	data, err := encoder.Encode(compileWorkflow(t))
	if err != nil {
		t.Fatalf("encode workflow: %v", err)
	}
	return data
}

// TestEncodeDeterministic：重复编码同一 IR 产出相同字节（无时间/路径/map
// 遍历序进入输出）。
func TestEncodeDeterministic(t *testing.T) {
	first := encodeWorkflow(t)
	second := encodeWorkflow(t)
	if !bytes.Equal(first, second) {
		t.Error("repeated encoding of the same IR produced different bytes")
	}
	if !bytes.HasSuffix(first, []byte("}\n")) || bytes.Count(first, []byte("\n")) != bytes.Count(bytes.TrimSuffix(first, []byte("\n")), []byte("\n"))+1 {
		t.Error("canonical form must end with exactly one trailing newline")
	}
}

// TestEncodeDecodeRoundTrip：decode→encode 字节不变（ADR-001 验收 3），且
// 解码 IR 与原 IR 深度相等。
func TestEncodeDecodeRoundTrip(t *testing.T) {
	cd := compileWorkflow(t)
	data, err := encoder.Encode(cd)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := encoder.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(cd, decoded) {
		t.Error("decoded IR differs from original IR")
	}
	reencoded, err := encoder.Encode(decoded)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(data, reencoded) {
		t.Errorf("round-trip changed bytes:\n--- original ---\n%s\n--- re-encoded ---\n%s", data, reencoded)
	}
}

// TestDigestFormat：digest 是 sha256: 前缀 + 64 位十六进制（仓库现行格式）。
func TestDigestFormat(t *testing.T) {
	data := encodeWorkflow(t)
	digest := encoder.Digest(data)
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		t.Fatalf("digest %q is not sha256-prefixed 64-hex", digest)
	}
	if encoder.Digest([]byte("x")) == encoder.Digest([]byte("y")) {
		t.Error("digest collided for different inputs")
	}
}

// mutateJSON 把制品按通用 JSON 解析后施加改动再压回紧凑 JSON，用于构造
// 非制品输入（decode 不依赖空白形态，紧凑重排不影响负例语义）。
func mutateJSON(t *testing.T, data []byte, mutate func(doc map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	mutate(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestDecodeRejectsUnknownFields：两级严格——外层与 payload 各自
// DisallowUnknownFields，closed world 不被未知字段穿透。
func TestDecodeRejectsUnknownFields(t *testing.T) {
	data := encodeWorkflow(t)

	outer := mutateJSON(t, data, func(doc map[string]any) { doc["surprise"] = true })
	if _, err := encoder.Decode(outer); err == nil {
		t.Error("decode accepted an unknown top-level field")
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	steps := payload["steps"].([]any)
	first := steps[0].(map[string]any)
	firstStepKind := first["kind"].(string)
	idx := 0
	if firstStepKind == "HUMAN_ASK" || firstStepKind == "PARALLEL" {
		idx = 1 // 未知字段负例需要一个带 payload 对象的步骤，任意变体均可
	}
	target := steps[idx].(map[string]any)
	payloadObj := target["payload"].(map[string]any)
	payloadObj["surprise"] = 1
	inner, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encoder.Decode(inner); err == nil {
		t.Error("decode accepted an unknown payload field")
	}
}

// TestDecodeRejectsNonCanonicalShapes：尾随内容、缺失 io、多余 io、null
// payload 等常见操作失误一律拒绝，不得静默"修复"。
func TestDecodeRejectsNonCanonicalShapes(t *testing.T) {
	data := encodeWorkflow(t)
	if _, err := encoder.Decode(append(append([]byte{}, data...), []byte("\n{}")...)); err == nil {
		t.Error("decode accepted trailing content")
	}

	// 可执行变体缺 io 键。
	missing := mutateJSON(t, data, func(doc map[string]any) {
		for _, s := range doc["steps"].([]any) {
			step := s.(map[string]any)
			if step["kind"] == "LOCAL" && step["id"] == "entry.parse" {
				delete(step, "io")
			}
		}
	})
	if _, err := encoder.Decode(missing); err == nil {
		t.Error("decode accepted a LOCAL step without io")
	}

	// human 变体携带 io 键。
	extra := mutateJSON(t, data, func(doc map[string]any) {
		for _, s := range doc["steps"].([]any) {
			step := s.(map[string]any)
			if step["kind"] == "HUMAN_ASK" {
				step["io"] = map[string]any{"inputCodec": "codec.any.in", "outputCodec": "codec.any.out"}
			}
		}
	})
	if _, err := encoder.Decode(extra); err == nil {
		t.Error("decode accepted a HUMAN_ASK step with an io block")
	}

	// null payload。
	nullPayload := mutateJSON(t, data, func(doc map[string]any) {
		doc["steps"].([]any)[0].(map[string]any)["payload"] = nil
	})
	if _, err := encoder.Decode(nullPayload); err == nil {
		t.Error("decode accepted a null payload")
	}
}

// TestDecodeRejectsEnvelopeAndCoherenceTampering：信封身份字段与物化维度
// （kind/authority/definitionVersion）被篡改的制品必须拒绝，decode 不得
// 静默归一化改写身份。
func TestDecodeRejectsEnvelopeAndCoherenceTampering(t *testing.T) {
	data := encodeWorkflow(t)
	cases := []struct {
		name   string
		mutate func(doc map[string]any)
	}{
		{"writer", func(doc map[string]any) { doc["writer"] = "legacy" }},
		{"stateSchemaVersion", func(doc map[string]any) { doc["stateSchemaVersion"] = "2" }},
		{"entrypoints", func(doc map[string]any) { doc["entrypoints"] = []any{"workflow start"} }},
		{"schema url", func(doc map[string]any) { doc["$schema"] = "https://elsewhere" }},
		{"step authority", func(doc map[string]any) {
			for _, s := range doc["steps"].([]any) {
				step := s.(map[string]any)
				if step["kind"] == "LOCAL" {
					step["authority"] = "HUMAN"
				}
			}
		}},
		{"kind without io", func(doc map[string]any) {
			// HUMAN_ASK -> LOCAL：LOCAL 必带 io，篡改后的步骤没有。
			for _, s := range doc["steps"].([]any) {
				step := s.(map[string]any)
				if step["kind"] == "HUMAN_ASK" {
					step["kind"] = "LOCAL"
					step["authority"] = "ENGINE"
					step["runner"] = "ENGINE_LOCAL"
				}
			}
		}},
		{"step definition version", func(doc map[string]any) {
			doc["steps"].([]any)[0].(map[string]any)["definitionVersion"] = "1"
		}},
		{"empty steps", func(doc map[string]any) { doc["steps"] = []any{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := encoder.Decode(mutateJSON(t, data, tc.mutate)); err == nil {
				t.Errorf("decode accepted tampered artifact (%s)", tc.name)
			}
		})
	}
}

// TestDecodeSecondLineRejects：decode 二次防线（封板后审计 H3）——durable
// payload 缺 retry 键不得静默归一为零值（零值 re-encode 会补写
// "retry":{"maxAttempts":0} 改写制品字节）；重复 step id、重复 ordinal、
// 悬空 dependency 的制品一律拒绝。合法制品的 round-trip 不回归由
// TestEncodeDecodeRoundTrip/TestCheckedInArtifactRoundTrip 覆盖。
func TestDecodeSecondLineRejects(t *testing.T) {
	data := encodeWorkflow(t)
	cases := []struct {
		name   string
		mutate func(doc map[string]any)
		want   string
	}{
		{"durable missing retry key", func(doc map[string]any) {
			for _, s := range doc["steps"].([]any) {
				step := s.(map[string]any)
				if step["kind"] == "DURABLE" {
					delete(step["payload"].(map[string]any), "retry")
				}
			}
		}, "requires a retry object"},
		{"durable null retry", func(doc map[string]any) {
			for _, s := range doc["steps"].([]any) {
				step := s.(map[string]any)
				if step["kind"] == "DURABLE" {
					step["payload"].(map[string]any)["retry"] = nil
				}
			}
		}, "requires a retry object"},
		{"duplicate step id", func(doc map[string]any) {
			steps := doc["steps"].([]any)
			steps[1].(map[string]any)["id"] = steps[0].(map[string]any)["id"]
		}, "duplicate step id"},
		{"duplicate ordinal", func(doc map[string]any) {
			steps := doc["steps"].([]any)
			steps[1].(map[string]any)["ordinal"] = steps[0].(map[string]any)["ordinal"]
		}, "share ordinal"},
		{"dangling dependency", func(doc map[string]any) {
			steps := doc["steps"].([]any)
			steps[0].(map[string]any)["dependencies"] = []any{"ghost.step"}
		}, `dependency "ghost.step" not found`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := encoder.Decode(mutateJSON(t, data, tc.mutate))
			if err == nil {
				t.Fatalf("decode accepted tampered artifact (%s)", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %q", tc.want, err.Error())
			}
		})
	}
}

// TestEncodeRejectsMarkerAndIncoherentIR：MISSING_ENGINE_ADAPTER marker 定义
// 不得物化为 canonical 制品；kind/payload/物化维度不一致的 IR 拒绝编码。
func TestEncodeRejectsMarkerAndIncoherentIR(t *testing.T) {
	marked := compileWorkflow(t)
	marked.MissingEngineAdapter = true
	if _, err := encoder.Encode(marked); err == nil {
		t.Error("encode accepted a MISSING_ENGINE_ADAPTER definition")
	}

	tampered := compileWorkflow(t)
	// Steps[0] 是 ask.decide（HUMAN/HOST_ADAPTER）；改成 ENGINE 即物化不一致。
	tampered.Steps[0].Header.Authority = "ENGINE"
	if _, err := encoder.Encode(tampered); err == nil {
		t.Error("encode accepted an IR with tampered authority")
	}

	unbound := compileWorkflow(t)
	unbound.Steps[0].Header.DefinitionVersion = "1"
	if _, err := encoder.Encode(unbound); err == nil {
		t.Error("encode accepted an IR with unbound step version")
	}

	if _, err := encoder.Encode(nil); err == nil {
		t.Error("encode accepted nil definition")
	}
}

// TestDigestSeparationBasis：digest 只覆盖制品字节——同一定义重复编译/编码
// digest 不变；改变拓扑（增删依赖等语义变化）digest 必变。handler 实现字节
// 不进入制品，PackageDigest 路径由后续批次承载（本测试固化制品级分离基础）。
func TestDigestSeparationBasis(t *testing.T) {
	stable := encoder.Digest(encodeWorkflow(t))
	if again := encoder.Digest(encodeWorkflow(t)); again != stable {
		t.Error("digest of the same definition changed between runs")
	}

	// 语义变化：给 report.cost 增加一条依赖（同时补 typed input binding，
	// 保持 "inputs == dependencies" 不变量）——digest 必变。
	altered, err := compiler.Compile(alteredWorkflow(), definition.Registry())
	if err != nil {
		t.Fatalf("compile altered workflow: %v", err)
	}
	alteredData, err := encoder.Encode(altered)
	if err != nil {
		t.Fatalf("encode altered workflow: %v", err)
	}
	if encoder.Digest(alteredData) == stable {
		t.Error("digest did not change after a semantic topology change")
	}
}

// alteredWorkflow 复制定义表并给 report.cost 增加 review.worker 依赖与
// 对应 typed input binding（其余内容不变）。
func alteredWorkflow() *compiler.Definition {
	def := definition.Workflow()
	steps := make([]authoring.Step, len(def.Steps))
	copy(steps, def.Steps)
	for i, s := range steps {
		cost, ok := s.(authoring.LocalStep)
		if !ok || cost.ID != "report.cost" {
			continue
		}
		cost.Dependencies = append(cost.Dependencies, "review.worker")
		cost.Inputs = append(cost.Inputs, authoring.InputBinding{
			From: "review.worker", OutputField: "out", ToField: "reviewIn",
		})
		steps[i] = cost
	}
	return &compiler.Definition{Version: def.Version, EntryNode: def.EntryNode, Steps: steps}
}
