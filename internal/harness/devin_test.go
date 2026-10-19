package harness

import (
	"strings"
	"testing"
)

func TestDevinNativeExecutionIsUnsupported(t *testing.T) {
	_, err := nativeImplementationFor("devin")
	if err == nil || !strings.HasPrefix(err.Error(), "UNSUPPORTED_NATIVE_HOST: devin; execution adapters: ") {
		t.Fatalf("native execution must remain explicitly unsupported: %v", err)
	}
	if strings.Contains(strings.TrimPrefix(err.Error(), "UNSUPPORTED_NATIVE_HOST: devin; execution adapters: "), "devin") {
		t.Fatalf("host-only adapter advertised as an execution worker: %v", err)
	}
}
