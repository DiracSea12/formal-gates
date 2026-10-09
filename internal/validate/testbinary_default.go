//go:build !qastructure

package validate

// qastructureBuild 在生产构建（未携带 qastructure QA 标签）下恒为
// false：放宽条件只能来自测试二进制的可执行文件名（.test/.test.exe），任何
// argv 形态都不触发放宽。
const qastructureBuild = false
