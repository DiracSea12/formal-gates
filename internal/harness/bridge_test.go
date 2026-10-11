package harness

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBridgeRejectsUnknownOperation(t *testing.T) {
	var out bytes.Buffer
	code := Bridge(strings.NewReader(`{"operation":"skip","payload":{}}`), &out)
	if code != 2 {
		t.Fatal(code)
	}
	var r map[string]any
	if e := json.Unmarshal(out.Bytes(), &r); e != nil || r["ok"] != false {
		t.Fatal(out.String())
	}
}

func TestBridgeRejectsInputAboveCeiling(t *testing.T) {
	oversized := `{"operation":"digest","payload":{"value":"` +
		strings.Repeat("x", 8*1024*1024) + `"}}`
	var out bytes.Buffer
	if code := Bridge(strings.NewReader(oversized), &out); code != 2 {
		t.Fatalf("oversized request accepted: code=%d %s", code, out.String())
	}
	if !strings.Contains(out.String(), "bridge input exceeds 8 MiB") {
		t.Fatalf("wrong refusal: %s", out.String())
	}
}
