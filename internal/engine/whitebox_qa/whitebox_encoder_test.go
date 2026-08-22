package whitebox_qa

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/encoder"
)

// 编码"<" "&"字样的单步夹具：HTML 不转义是 canonical 形态的一部分。
func weirdFixture(t *testing.T) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	step := mkLocalStep(t, fxHeader("s.1", "n0"),
		authoring.IO{InputCodec: "c.<in>", OutputCodec: "c.out"},
		authoring.LocalSpec{Handler: "h.<weird>&name"})
	reg := compiler.NewRegistry()
	if err := reg.RegisterHandler("h.<weird>&name", authoring.RunnerEngineLocal); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterCodec("c.<in>"); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterCodec("c.out"); err != nil {
		t.Fatal(err)
	}
	return fxCompile(t, fxDefinition(t, step), reg), reg
}

// 用例：canonical 编码形态——JSON、2 空格缩进、不转义 HTML、恰一个尾随
// 换行；重复编码逐字节一致；Digest 是制品字节的 SHA-256（sha256: 前缀）。
func TestEncodeCanonicalFormIsStableJSON(t *testing.T) {
	cd, _ := weirdFixture(t)
	b1, err := encoder.Encode(cd)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b2, err := encoder.Encode(cd)
	if err != nil {
		t.Fatalf("encode twice: %v", err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatal("same IR must encode to identical bytes")
	}
	if !bytes.HasSuffix(b1, []byte("}\n")) || bytes.HasSuffix(b1, []byte("\n\n")) {
		t.Fatalf("canonical form must end with exactly one trailing newline, got tail %q", b1[len(b1)-4:])
	}
	if !strings.Contains(string(b1), "\n  \"id\": \"formal-gates.workflow\"") {
		t.Fatal("canonical form must use 2-space indentation")
	}
	if !strings.Contains(string(b1), `"h.<weird>&name"`) {
		t.Fatal("canonical form must not HTML-escape (< &, e.g. \\u003c absent)")
	}
	if strings.Contains(string(b1), `\u003`) || strings.Contains(string(b1), `\u0026`) {
		t.Fatal("canonical form must not contain HTML-escaped runes")
	}

	d := encoder.Digest(b1)
	if !strings.HasPrefix(d, "sha256:") || len(d) != len("sha256:")+64 {
		t.Fatalf("digest %q must be sha256: + 64 hex chars", d)
	}
	sum := sha256.Sum256(b1)
	if want := "sha256:" + hex.EncodeToString(sum[:]); d != want {
		t.Fatalf("digest = %s, want %s", d, want)
	}
}

// 用例：decode → encode 字节不变（round-trip），且解码后的 IR 与编译产物
// 深度相等（公共头、IO 段、六变体 payload 全保留）。
func TestEncodeDecodeRoundTripPreservesIR(t *testing.T) {
	cd := fxCompile(t, fxDefinition(t), fxRegistry(t))
	data, err := encoder.Encode(cd)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := encoder.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(decoded, cd) {
		t.Fatalf("decoded IR differs from compiled IR:\n got %#v\nwant %#v", decoded, cd)
	}
	reencoded, err := encoder.Encode(decoded)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(reencoded, data) {
		t.Fatalf("round-trip bytes differ:\n got %s\nwant %s", reencoded, data)
	}
	if encoder.Digest(reencoded) != encoder.Digest(data) {
		t.Fatal("round-trip digest must be identical")
	}
}

// 用例：Decode 两级严格（外层与 payload 各自 DisallowUnknownFields）、
// 拒绝尾随内容、信封常量精确比对、human/parallel 不得携带 io、可执行变体
// 必带 io、payload 必须是对象。
func TestDecodeRejectsMalformedArtifacts(t *testing.T) {
	cd := fxCompile(t, fxDefinition(t), fxRegistry(t))
	base, err := encoder.Encode(cd)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := encoder.Decode(base); err != nil {
		t.Fatalf("baseline artifact must decode: %v", err)
	}

	rows := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"unknown top-level field", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) { m["bonus"] = 1 })
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "bonus")
		}},
		{"trailing content", func(t *testing.T) {
			_, err := encoder.Decode(append(append([]byte{}, base...), '\n', '{', '}', '\n'))
			wantErrContaining(t, err, "trailing content")
		}},
		{"envelope $schema mismatch", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) { m["$schema"] = "https://elsewhere/x.json" })
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "envelope", "$schema")
		}},
		{"envelope id mismatch", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) { m["id"] = "other.workflow" })
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "envelope", "id")
		}},
		{"envelope writer mismatch", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) { m["writer"] = "human" })
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "envelope", "writer")
		}},
		{"envelope entrypoints mismatch", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) { m["entrypoints"] = []any{"workflow start"} })
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "envelope", "entrypoints")
		}},
		{"empty steps", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) { m["steps"] = []any{} })
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "entryNode", "steps")
		}},
		{"unknown step field", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) {
				m["steps"].([]any)[0].(map[string]any)["bonus"] = 1
			})
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "bonus")
		}},
		{"human ask carries io", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) {
				for _, s := range m["steps"].([]any) {
					sm := s.(map[string]any)
					if sm["kind"] == "HUMAN_ASK" {
						sm["io"] = map[string]any{"inputCodec": "c.in", "outputCodec": "c.out"}
					}
				}
			})
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "must not carry an io block")
		}},
		{"local step missing io", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) {
				for _, s := range m["steps"].([]any) {
					sm := s.(map[string]any)
					if sm["kind"] == "LOCAL" && sm["id"] == "s.parse" {
						delete(sm, "io")
					}
				}
			})
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "requires an io block")
		}},
		{"unknown payload field", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) {
				m["steps"].([]any)[0].(map[string]any)["payload"].(map[string]any)["bonus"] = "x"
			})
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "bonus")
		}},
		{"null payload", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) {
				m["steps"].([]any)[0].(map[string]any)["payload"] = nil
			})
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "null")
		}},
		{"unknown kind", func(t *testing.T) {
			data := tamperArtifact(t, base, func(m map[string]any) {
				m["steps"].([]any)[0].(map[string]any)["kind"] = "MAGIC"
			})
			_, err := encoder.Decode(data)
			wantErrContaining(t, err, "unknown kind")
		}},
	}
	for _, row := range rows {
		t.Run(row.name, row.run)
	}
}

// 用例：Encode 的 IR 一致性二次防线——marker 定义、kind/payload 不符、
// authority/runner 篡改、步骤版本未绑定、nil payload 均拒绝物化为 canonical
// 制品身份。
func TestEncodeRejectsIncoherentIR(t *testing.T) {
	fresh := func(t *testing.T) *compiler.CompiledDefinition {
		return fxCompile(t, fxDefinition(t), fxRegistry(t))
	}

	cd := fresh(t)
	cd.MissingEngineAdapter = true
	_, err := encoder.Encode(cd)
	wantErrContaining(t, err, "MISSING_ENGINE_ADAPTER")

	cd = fresh(t)
	findStep(t, cd, "s.parse").Header.Authority = "AGENT"
	_, err = encoder.Encode(cd)
	wantErrContaining(t, err, "authority/runner")

	cd = fresh(t)
	findStep(t, cd, "s.parse").Header.Kind = compiler.KindAgent
	_, err = encoder.Encode(cd)
	wantErrContaining(t, err, "payload", "does not match kind")

	cd = fresh(t)
	findStep(t, cd, "s.parse").Header.Kind = "MAGIC"
	_, err = encoder.Encode(cd)
	wantErrContaining(t, err, "unknown kind")

	cd = fresh(t)
	findStep(t, cd, "s.parse").Header.DefinitionVersion = "9"
	_, err = encoder.Encode(cd)
	wantErrContaining(t, err, "definitionVersion")

	cd = fresh(t)
	findStep(t, cd, "s.parse").Payload = nil
	_, err = encoder.Encode(cd)
	wantErrContaining(t, err, "payload")

	cd = fresh(t)
	cd.Version = ""
	_, err = encoder.Encode(cd)
	wantErrContaining(t, err, "requires version")

	_, err = encoder.Encode(nil)
	wantErrContaining(t, err, "nil definition")
}
