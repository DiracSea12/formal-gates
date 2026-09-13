package host

import "testing"

func TestPayloadHTTPErrorCodeStandaloneToken(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"standalone code in prose", map[string]any{"detail": "request throttled with 429 errors"}, "HTTP 429"},
		{"dedicated error code field", map[string]any{"status_code": "the http 503 code"}, "HTTP 503"},
		{"code at string edge", "429", "HTTP 429"},
		{"substring inside longer number ignored", map[string]any{"detail": "model id 4290 rejected"}, ""},
		{"substring inside word ignored", map[string]any{"detail": "http429proxy"}, ""},
		{"code as part of decimal ignored", map[string]any{"detail": "value 3.429 in logs"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := payloadHTTPErrorCode(tc.value); got != tc.want {
				t.Fatalf("payloadHTTPErrorCode(%v)=%q want %q", tc.value, got, tc.want)
			}
		})
	}
}
