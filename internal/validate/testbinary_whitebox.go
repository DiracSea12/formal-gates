//go:build phase0whitebox

package validate

// whiteboxTestHarnessBuild 标记以 phase0whitebox QA 标签编译的二进制。白盒
// QA 会把测试二进制复制到 launcher/candidate 路径并以 -test.* 参数驱动子进
// 程复用测试逻辑：这些改名副本无法靠可执行文件名识别，但其编译期必然携带
// 本标签。仅在该标签下放宽 compact record 的准入校验；生产构建（无标签）
// 无论 argv 如何一律严格，-test.* 前缀参数不再构成放宽条件。
const whiteboxTestHarnessBuild = true
