//go:build !phase0whitebox

package validate

// whiteboxTestHarnessBuild 在生产构建（未携带 phase0whitebox QA 标签）下恒为
// false：放宽条件只能来自测试二进制的可执行文件名（.test/.test.exe），任何
// argv 形态都不再触发放宽。
const whiteboxTestHarnessBuild = false
