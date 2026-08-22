package decision

import (
	"fmt"
	"sort"

	"formal-gates/internal/engine/encoder"
)

// FactSource 是外部事实的封闭来源枚举（final-implementation-draft §3.1：
// Observe 只读收集 VCS、文件、宿主、lifecycle、receipt 和容量事实）。
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
	Source FactSource
	Key    string
	Value  string
}

// Observation 是一次 Observe 的产物：全部事实按 (Source, Key) 规范排序，
// 无重复键。Observation 只描述外部事实，不含函数、时间或路径遍历序，
// 因此相同事实集恒得相同 canonical 字节与 digest。
type Observation struct {
	Facts []Fact // 规范排序、非 nil（空观察为空切片）
}

// Collector 是单一来源的只读收集接口；真实收集器（VCS adapter、宿主
// bridge、lifecycle 登记、receipt 对账、容量计算）属后续批次实现。
// Collect 不得写任何状态；返回的事实 Source 必须与 Source() 一致。
type Collector interface {
	Source() FactSource
	Collect(state *State) ([]Fact, error)
}

// Observe 逐个调用收集器并汇成规范 Observation：事实按 (Source, Key)
// 排序；来源枚举非法、事实来源与收集器不符、空 Key、同 (Source, Key)
// 重复事实一律拒绝——观察本身不允许携带互相矛盾或不可寻址的事实。
func Observe(state *State, collectors []Collector) (Observation, error) {
	if state == nil {
		return Observation{}, fmt.Errorf("decision: observe: nil state")
	}
	seen := make(map[Fact]bool)
	facts := make([]Fact, 0)
	for _, c := range collectors {
		if c == nil {
			return Observation{}, fmt.Errorf("decision: observe: nil collector")
		}
		src := c.Source()
		if !src.Valid() {
			return Observation{}, fmt.Errorf("decision: observe: collector source %q invalid", src)
		}
		got, err := c.Collect(state)
		if err != nil {
			return Observation{}, fmt.Errorf("decision: observe: collector %s: %w", src, err)
		}
		for _, f := range got {
			if f.Source != src {
				return Observation{}, fmt.Errorf("decision: observe: collector %s produced fact with source %q", src, f.Source)
			}
			if f.Key == "" {
				return Observation{}, fmt.Errorf("decision: observe: collector %s produced empty fact key", src)
			}
			k := Fact{Source: f.Source, Key: f.Key}
			if seen[k] {
				return Observation{}, fmt.Errorf("decision: observe: duplicate fact %s/%s", f.Source, f.Key)
			}
			seen[k] = true
			facts = append(facts, f)
		}
	}
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Source != facts[j].Source {
			return facts[i].Source < facts[j].Source
		}
		return facts[i].Key < facts[j].Key
	})
	return Observation{Facts: facts}, nil
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
