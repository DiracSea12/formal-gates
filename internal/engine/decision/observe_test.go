package decision

import (
	"strings"
	"testing"
)

// fakeCollector 是测试用只读收集器：返回固定事实，可注入错误与非法输出。
type fakeCollector struct {
	src   FactSource
	facts []Fact
	err   error
	// wrongSource 使 Collect 返回与 Source() 不符的事实来源。
	wrongSource FactSource
	calls       int
}

func (c *fakeCollector) Source() FactSource { return c.src }

func (c *fakeCollector) Collect(*State) ([]Fact, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	out := make([]Fact, 0, len(c.facts))
	for _, f := range c.facts {
		if c.wrongSource != "" {
			f.Source = c.wrongSource
		}
		out = append(out, f)
	}
	return out, nil
}

func TestObserve(t *testing.T) {
	state := newTestState(t)
	vcs := &fakeCollector{src: SourceVCS, facts: []Fact{
		{Source: SourceVCS, Key: "head", Value: "abc"},
		{Source: SourceVCS, Key: "clean", Value: "true"},
	}}
	host := &fakeCollector{src: SourceHost, facts: []Fact{
		{Source: SourceHost, Key: "bridge", Value: "installed"},
	}}

	// 汇聚 + 规范排序 + 收集器顺序无关的字节稳定性。
	o1, err := Observe(state, []Collector{vcs, host})
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	o2, err := Observe(state, []Collector{host, vcs})
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	want := []string{"HOST/bridge", "VCS/clean", "VCS/head"}
	if len(o1.Facts) != len(want) {
		t.Fatalf("facts = %v, want %v", o1.Facts, want)
	}
	for i, f := range o1.Facts {
		if got := string(f.Source) + "/" + f.Key; got != want[i] {
			t.Fatalf("fact[%d] = %s, want %s", i, got, want[i])
		}
	}
	b1, _ := o1.CanonicalBytes()
	b2, _ := o2.CanonicalBytes()
	if string(b1) != string(b2) {
		t.Fatal("collector order must not leak into canonical bytes")
	}
	d1, _ := o1.Digest()
	d2, _ := o2.Digest()
	if d1 != d2 {
		t.Fatal("observation digest must be content-only")
	}
	if vcs.calls != 2 || host.calls != 2 {
		t.Fatal("collectors must be called once per Observe")
	}

	// 空收集器 → 空观察（稳定 digest，facts 为空数组而非 null）。
	empty, err := Observe(state, nil)
	if err != nil {
		t.Fatalf("observe empty: %v", err)
	}
	eb, err := empty.CanonicalBytes()
	if err != nil {
		t.Fatalf("bytes: %v", err)
	}
	if !strings.Contains(string(eb), `"facts": []`) {
		t.Fatalf("empty observation bytes = %s, want empty array", eb)
	}
	ed, _ := empty.Digest()
	ed2, _ := mustObservation(t).Digest()
	if ed == ed2 {
		t.Fatal("empty and non-empty observations must differ in digest")
	}
}

func mustObservation(t *testing.T) Observation {
	t.Helper()
	o, err := Observe(newTestState(t), []Collector{
		&fakeCollector{src: SourceCapacity, facts: []Fact{{Source: SourceCapacity, Key: "slots", Value: "2"}}},
	})
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	return o
}

func TestObserveRejections(t *testing.T) {
	state := newTestState(t)
	cases := map[string]struct {
		collectors []Collector
		want       string
	}{
		"nil collector": {[]Collector{nil}, "nil collector"},
		"invalid source": {[]Collector{
			&fakeCollector{src: "NOT_A_SOURCE"},
		}, "invalid"},
		"source mismatch": {[]Collector{
			&fakeCollector{src: SourceVCS, wrongSource: SourceFile, facts: []Fact{{Key: "k"}}},
		}, "source"},
		"empty key": {[]Collector{
			&fakeCollector{src: SourceVCS, facts: []Fact{{Source: SourceVCS, Key: "", Value: "v"}}},
		}, "empty fact key"},
		"duplicate fact": {[]Collector{
			&fakeCollector{src: SourceVCS, facts: []Fact{
				{Source: SourceVCS, Key: "head", Value: "a"},
				{Source: SourceVCS, Key: "head", Value: "b"},
			}},
		}, "duplicate"},
		"cross-collector duplicate": {[]Collector{
			&fakeCollector{src: SourceVCS, facts: []Fact{{Source: SourceVCS, Key: "head", Value: "a"}}},
			&fakeCollector{src: SourceVCS, facts: []Fact{{Source: SourceVCS, Key: "head", Value: "a"}}},
		}, "duplicate"},
		"collector error": {[]Collector{
			&fakeCollector{src: SourceVCS, err: errTestObserve},
		}, "collector"},
	}
	for name, tc := range cases {
		_, err := Observe(state, tc.collectors)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", name, err, tc.want)
		}
	}
	if _, err := Observe(nil, nil); err == nil {
		t.Error("nil state must be rejected")
	}
}

var errTestObserve = &testError{"boom"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestFactSourceValid(t *testing.T) {
	for _, f := range []FactSource{SourceVCS, SourceFile, SourceHost, SourceLifecycle, SourceReceipt, SourceCapacity} {
		if !f.Valid() {
			t.Errorf("source %q should be valid", f)
		}
	}
	for _, f := range []FactSource{"", "vcs", "CAPACITY "} {
		if f.Valid() {
			t.Errorf("source %q should be invalid", f)
		}
	}
}
