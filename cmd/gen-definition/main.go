// gen-definition 是 canonical 制品/身份常量的生成器入口（批次 1c）：
// 从 internal/engine/definition 的 authoring 定义表编译→编码→写盘，一次
// 动作产出 definitions/workflow.json 与 identity_gen.go。
//
// 用法：在仓库根目录执行
//
//	go run ./cmd/gen-definition
package main

import (
	"fmt"
	"os"

	"formal-gates/internal/engine/definition"
)

func main() {
	if err := definition.Generate("."); err != nil {
		fmt.Fprintln(os.Stderr, "gen-definition:", err)
		os.Exit(1)
	}
}
