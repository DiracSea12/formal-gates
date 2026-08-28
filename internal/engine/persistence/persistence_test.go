package persistence

import (
	"errors"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/validate"
)

// 测试用安装身份（packageDigest 的实际计算随阶段 3+ 安装事务交付，
// 本批用固定注入值验证信封校验侧）。
const testPackageDigest = "sha256:test-package-digest"

// TestFrozenConstantAlignment 钉死常量同源与语义对齐：
//   - stateSchemaVersion 复用的 encoder 引擎侧常量必须等于 phase0 冻结
//     常量 CurrentStateSchemaVersion（master-requirements §1 语义）；
//   - UnsupportedRunVersionCode 与 phase0 冻结错误码同值；
//   - 信封的 definition 版本与摘要直接同源 definition 生成常量。
//
// validate 只在测试中导入：生产代码不依赖 legacy（阶段 5 删除 legacy
// 时不影响本包），测试钉死的是语义对齐本身。
func TestFrozenConstantAlignment(t *testing.T) {
	if encoder.StateSchemaVersion != validate.CurrentStateSchemaVersion {
		t.Fatalf("engine stateSchemaVersion %q != phase0 frozen %q",
			encoder.StateSchemaVersion, validate.CurrentStateSchemaVersion)
	}
	if UnsupportedRunVersionCode != validate.UnsupportedRunVersionCode {
		t.Fatalf("engine unsupported-run-version code %q != phase0 frozen %q",
			UnsupportedRunVersionCode, validate.UnsupportedRunVersionCode)
	}
	if Writer != "engine" {
		t.Fatalf("writer constant %q != %q", Writer, "engine")
	}
	env := expectedEnvelope(Config{PackageDigest: testPackageDigest})
	if env.WorkflowDefinitionVersion != definition.WorkflowDefinitionVersion {
		t.Fatalf("envelope definition version %q != identity_gen %q",
			env.WorkflowDefinitionVersion, definition.WorkflowDefinitionVersion)
	}
	if env.DefinitionDigest != definition.WorkflowDefinitionDigest {
		t.Fatalf("envelope definition digest %q != identity_gen %q",
			env.DefinitionDigest, definition.WorkflowDefinitionDigest)
	}
}

// TestValidateEnvelopeAcceptsExactIdentity：当前 engine 身份的信封精确
// 通过校验。
func TestValidateEnvelopeAcceptsExactIdentity(t *testing.T) {
	env := expectedEnvelope(Config{PackageDigest: testPackageDigest})
	if err := validateEnvelope(env, Config{PackageDigest: testPackageDigest}); err != nil {
		t.Fatalf("exact envelope rejected: %v", err)
	}
}

// TestValidateEnvelopeMissingField：任一字段缺失（零值）即拒绝，错误是
// UnsupportedRunVersionError 且定位到具体字段。
func TestValidateEnvelopeMissingField(t *testing.T) {
	for _, field := range []string{
		"writer", "stateSchemaVersion", "workflowDefinitionVersion", "definitionDigest", "packageDigest",
	} {
		env := expectedEnvelope(Config{PackageDigest: testPackageDigest})
		switch field {
		case "writer":
			env.Writer = ""
		case "stateSchemaVersion":
			env.StateSchemaVersion = ""
		case "workflowDefinitionVersion":
			env.WorkflowDefinitionVersion = ""
		case "definitionDigest":
			env.DefinitionDigest = ""
		case "packageDigest":
			env.PackageDigest = ""
		}
		err := validateEnvelope(env, Config{PackageDigest: testPackageDigest})
		if err == nil {
			t.Fatalf("missing %s accepted", field)
		}
		var unsupported *UnsupportedRunVersionError
		if !errors.As(err, &unsupported) {
			t.Fatalf("missing %s: error is %T, want *UnsupportedRunVersionError", field, err)
		}
		if unsupported.Field != field {
			t.Fatalf("missing %s: error field %q", field, unsupported.Field)
		}
		if !strings.Contains(err.Error(), UnsupportedRunVersionCode) {
			t.Fatalf("missing %s: error %q lacks code %q", field, err, UnsupportedRunVersionCode)
		}
	}
}

// TestValidateEnvelopeImpreciseValues：任一字段值不精确匹配（旧版本、
// 别的 writer、别的摘要、空白填充）一律拒绝——不接受“前缀匹配”或
// “忽略空白”之类的宽松解释。
func TestValidateEnvelopeImpreciseValues(t *testing.T) {
	wrong := map[string]func(*Envelope){
		"writer=legacy":                 func(e *Envelope) { e.Writer = "legacy" },
		"writer=engine (padded)":        func(e *Envelope) { e.Writer = " engine " },
		"stateSchemaVersion=2":          func(e *Envelope) { e.StateSchemaVersion = "2" },
		"workflowDefinitionVersion=1":   func(e *Envelope) { e.WorkflowDefinitionVersion = "1" },
		"definitionDigest=other":        func(e *Envelope) { e.DefinitionDigest = "sha256:0000" },
		"definitionDigest=no-prefix":    func(e *Envelope) { e.DefinitionDigest = strings.TrimPrefix(e.DefinitionDigest, "sha256:") },
		"packageDigest=stale":           func(e *Envelope) { e.PackageDigest = "sha256:stale-runtime" },
		"packageDigest=empty":           func(e *Envelope) { e.PackageDigest = " " },
		"packageDigest=config-mismatch": func(e *Envelope) { e.PackageDigest = testPackageDigest },
	}
	for name, mutate := range wrong {
		env := expectedEnvelope(Config{PackageDigest: testPackageDigest})
		mutate(&env)
		cfg := Config{PackageDigest: testPackageDigest}
		if name == "packageDigest=config-mismatch" {
			// 同一信封换个 runtime 身份校验：安装身份不同必须拒绝。
			cfg.PackageDigest = "sha256:other-runtime"
		}
		if err := validateEnvelope(env, cfg); err == nil {
			t.Fatalf("imprecise value accepted: %s", name)
		}
	}
}

// TestErrorCodesInMessages：四类协议错误的文案都以各自错误码开头，供
// 调用方机械分类。
func TestErrorCodesInMessages(t *testing.T) {
	checks := []struct {
		code string
		err  error
	}{
		{UnsupportedRunVersionCode, (&UnsupportedRunVersionError{Field: "writer", Expected: "engine", Observed: ""})},
		{StateIntegrityCode, (&IntegrityMismatchError{Path: "p", Expected: "a", Observed: "b"})},
		{RevisionConflictCode, (&RevisionConflictError{Path: "p", Expected: 1, Observed: 2})},
		{FingerprintChangedCode, (&FingerprintChangedError{Phase: FingerprintPhaseBefore, Expected: "a", Observed: "b"})},
		{FingerprintChangedCode, (&FingerprintChangedError{Phase: FingerprintPhaseAfter, Committed: true, Expected: "a", Observed: "b"})},
	}
	for _, check := range checks {
		if !strings.HasPrefix(check.err.Error(), check.code) {
			t.Fatalf("error %q lacks code prefix %q", check.err, check.code)
		}
	}
}
