# 平台接入与协议更新

`internal/host.Descriptor` 是配置、安装、生命周期和用量的同一描述对象。底层内建描述可供独立的配置/生命周期/计量调用读取。`internal/harness/native_provider.go` 的组合登记把该对象与原生执行能力绑定，所有执行和恢复都从 `host.LookupAdapter` 取得同一对象。

新增平台在 harness 中实现一个薄 `nativeImplementation`：捕获有效配置、验证绑定、核实原始来源和启动身份、执行、从原始完成事实恢复；沿原 `ExecutionServices` 接纳权限和消息。提供一个完整 `*host.Descriptor`，含该平台的配置默认值/校验、支持的安装路径和 hooks、`Lifecycle` 的原生事件/身份解释，以及可用时的 `ReadUsage`。只在组合登记位置调用一次 `registerNativePlatform(descriptor, implementation)`；不要向配置、生命周期或计量包增添名单。没有安装或用量能力的平台不声明相应能力。

已有平台协议更新只修改该平台的 native 文件、host 中的平台解析文件和对应测试/说明。用量解析读原始记录，保留准确计数、重复记录去重和结构性错误；恢复核实原执行身份及来源，不能制造新结果或计量。若公共能力合同确实改变，先明确影响。

验证新增/更新接缝：

```sh
go test -timeout=0 ./internal/harness -run '^(TestNativePlatformRegistrationCarriesAllPublicCapabilities|TestNativeAdapterEntriesImplementWholeBoundary)$'
go test -timeout=0 ./internal/host ./internal/cost ./internal/lifecycle ./internal/settings
```

本地假平台在一个隔离进程里真实登记一次，经公共配置加载、受管 native 探测/启动/续用、原始身份/来源恢复、生命周期绑定和计量入口核验。两次不同输入产生不同实际子进程输出；同一对象接纳一次结果格式更新，不增加其他名单。该测试不代表新增真实宿主支持，也不声明假平台安装支持。
