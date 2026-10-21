package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type panelWaitBuffer struct {
	sync.Mutex
	text strings.Builder
}

func (b *panelWaitBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.text.Write(p)
}
func (b *panelWaitBuffer) String() string { b.Lock(); defer b.Unlock(); return b.text.String() }
func waitPanelOutput(t *testing.T, out *panelWaitBuffer, token string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), token) {
		if time.Now().After(deadline) {
			t.Fatalf("missing %q in %s", token, out.String())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestServeConfigureWaitDoesNotBlockNativeConfirmation(t *testing.T) {
	for _, cancelPanel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelPanel), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			in, feed := io.Pipe()
			defer in.Close()
			defer feed.Close()
			var out, stderr panelWaitBuffer
			opened := make(chan struct{})
			selected := make(chan struct{})
			done := make(chan int, 1)
			go func() {
				done <- ServeToolsWithContext(ctx, in, &out, &stderr, nil, func(callCtx context.Context, name string, _ json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
					if name == "configure" {
						close(opened)
						select {
						case <-selected:
							return map[string]any{"status": "selected"}, nil
						case <-callCtx.Done():
							return map[string]any{"status": "cancelled"}, nil
						}
					}
					if name == "confirm_start" {
						if !cancelPanel {
							close(selected)
						}
						if err := confirm(map[string]any{"operation": "start"}); err != nil {
							return nil, err
						}
						return map[string]any{"approved": true}, nil
					}
					return nil, fmt.Errorf("unexpected tool %s", name)
				}, nil, nil)
			}()
			fmt.Fprintln(feed, `{"id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"elicitation":{"form":{}}}}}`)
			fmt.Fprintln(feed, `{"id":2,"method":"tools/call","params":{"name":"configure","arguments":{}}}`)
			select {
			case <-opened:
			case <-time.After(10 * time.Second):
				t.Fatal("configure did not begin")
			}
			fmt.Fprintln(feed, `{"id":3,"method":"tools/call","params":{"name":"confirm_start","arguments":{}}}`)
			waitPanelOutput(t, &out, `"method":"elicitation/create"`)
			if cancelPanel {
				fmt.Fprintln(feed, `{"method":"notifications/cancelled","params":{"requestId":2}}`)
				waitPanelOutput(t, &out, `cancelled`)
			} else {
				waitPanelOutput(t, &out, `selected`)
			}
			fmt.Fprintln(feed, `{"id":4,"method":"tools/call","params":{"name":"confirm_control","arguments":{}}}`)
			waitPanelOutput(t, &out, `a native confirmation is pending`)
			fmt.Fprintln(feed, `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`)
			waitPanelOutput(t, &out, `approved`)
			feed.Close()
			select {
			case code := <-done:
				if code != 0 || stderr.String() != "" {
					t.Fatalf("serve: %d %s", code, stderr.String())
				}
			case <-time.After(10 * time.Second):
				t.Fatal("serve did not finish")
			}
		})
	}
}
