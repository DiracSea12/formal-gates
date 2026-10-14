package decision

import "formal-gates/internal/engine/encoder"

// FactSource 是 VCS、文件、宿主、lifecycle、receipt 和容量事实的封闭来源枚举。
// source binding 是事实可信度与对账的锚点；零值与其他字符串非法。
type FactSource string

const (
	SourceVCS       FactSource = "VCS"       // provider 探测、workspace、diff、identity
	SourceFile      FactSource = "FILE"      // 本地文件/artifact 事实
	SourceHost      FactSource = "HOST"      // 宿主与 bridge 可用性、provider 配对
	SourceLifecycle FactSource = "LIFECYCLE" // agent lifecycle 事件
	SourceReceipt   FactSource = "RECEIPT"   // SpawnReceipt/HostAction receipt 等
	SourceCapacity  FactSource = "CAPACITY"  // 可用签发容量
)

// Valid 报告 f 是否属于六值封闭集合。
func (f FactSource) Valid() bool {
	switch f {
	case SourceVCS, SourceFile, SourceHost, SourceLifecycle, SourceReceipt, SourceCapacity:
		return true
	}
	return false
}

// Fact 是一条带 source binding 的外部事实。Key 在来源内唯一；Value 是
// 该事实的规范字符串值（结构化值由各来源的收集器负责规范化）。
type Fact struct {
	Source FactSource `json:"source"`
	Key    string     `json:"key"`
	Value  string     `json:"value"`
}

// Observation 是携带有序外部事实的决策输入。canonical 编码保留输入顺序，
// 相同有序输入得到相同字节与 digest。
type Observation struct {
	Facts []Fact
}

// observationWire 是 Observation 的 canonical wire 形态。
type observationWire struct {
	Facts []factWire `json:"facts"`
}

type factWire struct {
	Source string `json:"source"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}

// CanonicalBytes 返回 Observation 的 canonical 字节（形态与 encoder 制品
// 一致：JSON、2 空格缩进、不转义 HTML、恰一个尾随换行；空集编码为
// "facts": [] 而非 null）。
func (o Observation) CanonicalBytes() ([]byte, error) {
	w := observationWire{Facts: make([]factWire, 0, len(o.Facts))}
	for _, f := range o.Facts {
		w.Facts = append(w.Facts, factWire{Source: string(f.Source), Key: f.Key, Value: f.Value})
	}
	return canonicalJSON(w)
}

// Digest 返回 canonical 字节的 SHA-256 摘要（sha256: 前缀，复用 encoder
// 的摘要格式；ObservationDigest 即本函数结果）。
func (o Observation) Digest() (string, error) {
	data, err := o.CanonicalBytes()
	if err != nil {
		return "", err
	}
	return encoder.Digest(data), nil
}
