// Package acceptance 承载阶段 1 批 3 的验收套件与收口段可复用的
// MISSING_ENGINE_ADAPTER marker 扫描入口。
//
// 本包不含生产逻辑：ScanNoMissingEngineAdapter 是收口段（closure）对最终
// 候选 definition 的机械检查入口；验收用例全部以 *_test.go 固化，可被
// go test -run TestAcceptance* 逐类选定。
package acceptance

import (
	"fmt"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/encoder"
)

// ScanNoMissingEngineAdapter 扫描一个 CompiledDefinition（全部步骤的编译
// 产物），断言它不携带 MISSING_ENGINE_ADAPTER marker 且可进入 executable
// 路径：marker 定义只有 CompileDiagnostic 能产出，而 canonical encoder 对
// marker 定义硬拒绝，因此"marker 位为零 + Encode 成功"即"全步骘认证完备、
// 无 MISSING_ENGINE_ADAPTER、可执行"的机械证明。
//
// 收口段调用方式（二选一）：
//  1. 测试内 import 本包后对最终候选调用：
//     err := acceptance.ScanNoMissingEngineAdapter(cd)
//     err 非 nil 即候选携带 marker 或不可编码，不得进入 executable plan / Seal。
//  2. 命令行（对 checked-in definitions/workflow.json 的编译产物执行同一扫描）：
//     go test ./internal/engine/acceptance -run TestAcceptanceMarkerScan -v
//
// 本函数是拒绝性扫描，永远不产出 marker 定义；diagnostic 模式的 fixture
// 只存在于测试内（CompileDiagnostic 产物），不经本函数放行。
func ScanNoMissingEngineAdapter(cd *compiler.CompiledDefinition) error {
	if cd == nil {
		return fmt.Errorf("acceptance: marker scan: nil definition")
	}
	if cd.MissingEngineAdapter {
		return fmt.Errorf("acceptance: marker scan: definition of %d steps carries MISSING_ENGINE_ADAPTER marker", len(cd.Steps))
	}
	// executable 路径二次断言：encoder 拒绝携带 marker 的定义（checkCoherence），
	// 也顺带拒绝任何物化不一致的 IR——扫描通过即候选可编码为 canonical 制品。
	if _, err := encoder.Encode(cd); err != nil {
		return fmt.Errorf("acceptance: marker scan: definition rejected by executable path: %w", err)
	}
	return nil
}
