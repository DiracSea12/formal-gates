package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestNativeConfirmationAcceptsOnlyCurrentHostResponse(t *testing.T) {
	for _, test := range []struct {
		name, capabilities, response, problem string
		accepted                              bool
	}{
		{"accept", `{"elicitation":{"form":{}}}`, `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`, "", true},
		{"legacy form capability", `{"elicitation":{}}`, `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`, "", true},
		{"decline", `{"elicitation":{"form":{}}}`, `{"id":"formal-gates-confirmation-1","result":{"action":"decline"}}`, "not executed", false},
		{"cancel", `{"elicitation":{"form":{}}}`, `{"id":"formal-gates-confirmation-1","result":{"action":"cancel"}}`, "not executed", false},
		{"missing capability", `{}`, "", "NATIVE_CONFIRMATION_UNAVAILABLE", false},
		{"url only", `{"elicitation":{"url":{}}}`, "", "NATIVE_CONFIRMATION_UNAVAILABLE", false},
		{"another identity", `{"elicitation":{"form":{}}}`, `{"id":"another-request","result":{"action":"accept","content":{"confirmOperation":true}}}`, "another request", false},
		{"request cannot answer", `{"elicitation":{"form":{}}}`, `{"id":"formal-gates-confirmation-1","method":"tools/call","result":{"action":"accept","content":{"confirmOperation":true}}}`, "not received", false},
		{"disconnect", `{"elicitation":{"form":{}}}`, "", "not received", false},
		{"empty acceptance", `{"elicitation":{"form":{}}}`, `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{}}}`, "explicitly confirm", false},
		{"cancelled original call", `{"elicitation":{"form":{}}}`, `{"method":"notifications/cancelled","params":{"requestId":2}}` + "\n" + `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`, "not executed", false},
		{"another cancellation", `{"elicitation":{"form":{}}}`, `{"method":"notifications/cancelled","params":{"requestId":99}}` + "\n" + `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`, "", true},
		{"host ping", `{"elicitation":{"form":{}}}`, `{"id":"host-ping","method":"ping"}` + "\n" + `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":` + test.capabilities + `}}` + "\n" +
				`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"confirm","arguments":{}}}` + "\n" + test.response + "\n"
			var out, errors bytes.Buffer
			executed := false
			code := ServeTools(strings.NewReader(wire), &out, &errors, nil, func(name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
				if name != "confirm" || string(arguments) != "{}" {
					t.Fatalf("unexpected original call: %s %s", name, arguments)
				}
				if err := confirm(map[string]any{"operation": "original operation", "revision": "original revision"}); err != nil {
					return nil, err
				}
				executed = true
				return map[string]any{"executed": true}, nil
			}, nil)
			if code != 0 || executed != test.accepted || errors.Len() != 0 {
				t.Fatalf("code=%d executed=%v stderr=%s stdout=%s", code, executed, errors.String(), out.String())
			}
			if test.problem != "" && !strings.Contains(out.String(), test.problem) {
				t.Fatal("missing rejection evidence", out.String())
			}
			if test.name == "host ping" && !strings.Contains(out.String(), `"id":"host-ping","jsonrpc":"2.0","result":{}`) {
				t.Fatal("host ping was not answered while waiting", out.String())
			}
			if test.name != "missing capability" && test.name != "url only" {
				var requests []map[string]any
				decoder := json.NewDecoder(&out)
				for decoder.More() {
					var message map[string]any
					if err := decoder.Decode(&message); err != nil {
						t.Fatal(err)
					}
					if message["method"] == "elicitation/create" {
						requests = append(requests, message)
					}
				}
				if len(requests) != 1 || requests[0]["id"] != "formal-gates-confirmation-1" || getOf(getOf(requests[0], "params"), "mode") != "form" {
					t.Fatal("confirmation did not use its original MCP request", requests)
				}
				message := textOf(getOf(getOf(requests[0], "params"), "message"))
				if !strings.Contains(message, "original operation") || !strings.Contains(message, "original revision") {
					t.Fatal("host did not receive the exact operation", message)
				}
			}
		})
	}
}

func TestNativeConfirmationKeepsOriginalCallWhileAnotherCallIsBusy(t *testing.T) {
	wire := `{"id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"elicitation":{"form":{}}}}}` + "\n" +
		`{"id":2,"method":"tools/call","params":{"name":"original","arguments":{"operation":"first"}}}` + "\n" +
		`{"id":3,"method":"tools/call","params":{"name":"second","arguments":{"operation":"second"}}}` + "\n" +
		`{"id":4,"method":"ping"}` + "\n" +
		`{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}` + "\n"
	var output, errors bytes.Buffer
	calls := 0
	code := ServeTools(strings.NewReader(wire), &output, &errors, nil, func(name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
		calls++
		if name != "original" || string(arguments) != `{"operation":"first"}` {
			t.Fatalf("another request replaced the original operation: %s %s", name, arguments)
		}
		if err := confirm(json.RawMessage(arguments)); err != nil {
			return nil, err
		}
		return "original accepted", nil
	}, nil)
	if code != 0 || calls != 1 || errors.Len() != 0 {
		t.Fatal(code, calls, errors.String())
	}
	responses := map[float64]map[string]any{}
	decoder := json.NewDecoder(&output)
	for decoder.More() {
		var frame map[string]any
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if id, ok := frame["id"].(float64); ok {
			responses[id] = frame
		}
	}
	if getOf(responses[2]["result"], "isError") != false || getOf(responses[3]["error"], "code") != float64(-32000) || responses[4]["result"] == nil {
		t.Fatal("pending confirmation did not preserve each caller's response", responses)
	}
}

// A stale form reply for an already-resolved confirmation owns no waiter and
// must not poison a later one.
func TestNativeConfirmationIgnoresStaleFormReplies(t *testing.T) {
	wire := `{"id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"elicitation":{"form":{}}}}}` + "\n" +
		`{"id":2,"method":"tools/call","params":{"name":"confirm","arguments":{}}}` + "\n" +
		`{"id":"formal-gates-confirmation-9","result":{"action":"accept","content":{"confirmOperation":true}}}` + "\n" +
		`{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}` + "\n"
	var out, errors bytes.Buffer
	executed := false
	code := ServeTools(strings.NewReader(wire), &out, &errors, nil, func(name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
		if err := confirm(map[string]any{"operation": "original"}); err != nil {
			return nil, err
		}
		executed = true
		return "done", nil
	}, nil)
	if code != 0 || !executed || errors.Len() != 0 {
		t.Fatalf("code=%d executed=%v stderr=%s stdout=%s", code, executed, errors.String(), out.String())
	}
}

// Cancelling the transport context stops the running tool call; the host
// gets the callback's own error result before the service exits.
func TestServeToolsWithContextCancelStopsActiveCall(t *testing.T) {
	wire := `{"id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"elicitation":{"form":{}}}}}` + "\n" +
		`{"id":2,"method":"tools/call","params":{"name":"wait","arguments":{}}}` + "\n"
	var out, errors bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan int, 1)
	go func() {
		done <- ServeToolsWithContext(ctx, strings.NewReader(wire), &out, &errors, nil,
			func(callCtx context.Context, name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
				close(started)
				<-callCtx.Done()
				return nil, callCtx.Err()
			}, nil)
	}()
	<-started
	cancel()
	select {
	case code := <-done:
		if code != 0 || errors.Len() != 0 {
			t.Fatalf("code=%d stderr=%s", code, errors.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("transport context cancellation did not stop the call")
	}
	if !strings.Contains(out.String(), "context canceled") {
		t.Fatal("call result was not reported back to the host", out.String())
	}
}

// Closing the transport's stdin cancels the running callback with io.EOF and
// lets the service exit, without touching the caller's own context.
func TestServeToolsWithContextEOFCancelsActiveCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, feed := io.Pipe()
	var out, errors bytes.Buffer
	started := make(chan struct{})
	done := make(chan int, 1)
	go func() {
		done <- ServeToolsWithContext(ctx, input, &out, &errors, nil,
			func(callCtx context.Context, name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
				close(started)
				<-callCtx.Done()
				return nil, callCtx.Err()
			}, nil)
	}()
	fmt.Fprint(feed, `{"id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`+"\n")
	fmt.Fprint(feed, `{"id":2,"method":"tools/call","params":{"name":"wait","arguments":{}}}`+"\n")
	<-started
	if err := feed.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 || errors.Len() != 0 {
			t.Fatalf("code=%d stderr=%s", code, errors.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stdin EOF did not cancel the active call")
	}
	if ctx.Err() != nil {
		t.Fatal("EOF cancelled the caller's context")
	}
}

// A confirmation reply decoded before stdin closes is still consumed, so a
// buffered accept completes the operation exactly once at EOF.
func TestNativeConfirmationAcceptsBufferedReplyAtEOF(t *testing.T) {
	wire := `{"id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"elicitation":{"form":{}}}}}` + "\n" +
		`{"id":2,"method":"tools/call","params":{"name":"confirm","arguments":{}}}` + "\n" +
		`{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}` + "\n"
	var out, errors bytes.Buffer
	executed := 0
	code := ServeTools(strings.NewReader(wire), &out, &errors, nil, func(name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
		if err := confirm(map[string]any{"operation": "original"}); err != nil {
			return nil, err
		}
		executed++
		return "done", nil
	}, nil)
	if code != 0 || executed != 1 || errors.Len() != 0 {
		t.Fatalf("code=%d executed=%d stderr=%s stdout=%s", code, executed, errors.String(), out.String())
	}
}

// An explicit request cancellation refuses an approval that arrives after it;
// the operation never executes.
func TestNativeConfirmationRejectsReplyAfterCancel(t *testing.T) {
	wire := `{"id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"elicitation":{"form":{}}}}}` + "\n" +
		`{"id":2,"method":"tools/call","params":{"name":"confirm","arguments":{}}}` + "\n" +
		`{"method":"notifications/cancelled","params":{"requestId":2}}` + "\n" +
		`{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}` + "\n"
	var out, errors bytes.Buffer
	executed := false
	code := ServeTools(strings.NewReader(wire), &out, &errors, nil, func(name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
		if err := confirm(map[string]any{"operation": "original"}); err != nil {
			return nil, err
		}
		executed = true
		return "done", nil
	}, nil)
	if code != 0 || executed {
		t.Fatalf("code=%d executed=%v stdout=%s", code, executed, out.String())
	}
	if !strings.Contains(out.String(), "not executed") {
		t.Fatal("the cancelled call did not report its refusal", out.String())
	}
}

// Legacy transports queue an ordinary call behind a running one instead of
// refusing it; both run exactly once in order.
func TestServeToolsQueuesOrdinaryCallWhileActive(t *testing.T) {
	wire := `{"id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}` + "\n" +
		`{"id":2,"method":"tools/call","params":{"name":"first","arguments":{}}}` + "\n" +
		`{"id":3,"method":"tools/call","params":{"name":"second","arguments":{}}}` + "\n"
	var out, errors bytes.Buffer
	release := make(chan struct{})
	started := make(chan struct{})
	var order []string
	done := make(chan int, 1)
	go func() {
		done <- ServeTools(strings.NewReader(wire), &out, &errors, nil, func(name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
			order = append(order, name)
			if name == "first" {
				close(started)
				<-release
			}
			return name + " done", nil
		}, nil)
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("first call never started")
	}
	close(release)
	select {
	case code := <-done:
		if code != 0 || errors.Len() != 0 {
			t.Fatalf("code=%d stderr=%s", code, errors.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("queued call never ran")
	}
	// ServeTools only returns after both callbacks finished, so reading the
	// recorded order below races with nothing.
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatal("calls did not run once each in order:", order)
	}
	responses := map[float64]map[string]any{}
	decoder := json.NewDecoder(&out)
	for decoder.More() {
		var frame map[string]any
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if id, ok := frame["id"].(float64); ok {
			responses[id] = frame
		}
	}
	if getOf(responses[2]["result"], "isError") != false || getOf(responses[3]["result"], "isError") != false {
		t.Fatal("queued calls did not both succeed", responses)
	}
}

// Confirmation bookkeeping is scoped to the in-flight call: a stray response
// recorded while an earlier call ran must not poison the next call's form.
func TestNativeConfirmationScopingAcrossCalls(t *testing.T) {
	input, feed := io.Pipe()
	var errors bytes.Buffer
	// Replies stream through a pipe the test decodes serially, so observing the
	// id:3 response never races the encoder writing it.
	outR, outW := io.Pipe()
	frames := make(chan map[string]any, 32)
	decoded := make(chan struct{})
	go func() {
		defer close(decoded)
		decoder := json.NewDecoder(outR)
		for {
			var frame map[string]any
			if err := decoder.Decode(&frame); err != nil {
				return
			}
			frames <- frame
		}
	}()
	confirming := make(chan struct{})
	done := make(chan int, 1)
	go func() {
		done <- ServeTools(input, outW, &errors, nil, func(name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
			if name == "noop" {
				return "ok", nil
			}
			close(confirming)
			if err := confirm(map[string]any{"operation": "second"}); err != nil {
				return nil, err
			}
			return "confirmed", nil
		}, nil)
	}()
	fmt.Fprint(feed, `{"id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"elicitation":{"form":{}}}}}`+"\n")
	fmt.Fprint(feed, `{"id":2,"method":"tools/call","params":{"name":"noop","arguments":{}}}`+"\n")
	fmt.Fprint(feed, `{"id":"stray-foreign","result":{"action":"accept","content":{"confirmOperation":true}}}`+"\n")
	fmt.Fprint(feed, `{"id":"formal-gates-confirmation-9","result":{"action":"accept","content":{"confirmOperation":true}}}`+"\n")
	fmt.Fprint(feed, `{"id":3,"method":"tools/call","params":{"name":"confirm","arguments":{}}}`+"\n")
	<-confirming
	fmt.Fprint(feed, `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`+"\n")
	var final map[string]any
	deadline := time.After(10 * time.Second)
	for final == nil {
		select {
		case frame := <-frames:
			if id, ok := frame["id"].(float64); ok && id == 3 {
				final = frame
			}
		case <-deadline:
			t.Fatal("second call never completed")
		}
	}
	_ = feed.Close()
	select {
	case code := <-done:
		if code != 0 || errors.Len() != 0 {
			t.Fatalf("code=%d stderr=%s", code, errors.String())
		}
	case <-deadline:
		t.Fatal("service did not exit after input close")
	}
	_ = outR.Close()
	<-decoded
	if getOf(final["result"], "isError") != false {
		t.Fatal("leftover replies poisoned the next call's confirmation", final)
	}
}
