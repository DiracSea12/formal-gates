package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/host"
)

// PrepareHostWorker freezes only this drive caller's observed native context.
// The original request remains unclaimed until a real child receives it.
func (f *Facade) PrepareHostWorker(material WorkerMaterial, resumeActionID, session, nativeHome string) error {
	request, _, directory, original, err := f.WorkerExecutionRequest(material, resumeActionID)
	if err != nil {
		return err
	}
	if original != material {
		return fmt.Errorf("host worker material changed before preparation")
	}
	if request["provider"] != "codex" {
		return fmt.Errorf("HOST_SUBAGENT_UNAVAILABLE: native controller facts are unavailable for %v", request["provider"])
	}
	if request["host"] != nil {
		return nil
	}
	thread, err := host.FindCodexThread(nativeHome, session)
	if err != nil {
		return err
	}
	if len(thread.Turns) == 0 {
		return fmt.Errorf("native controller has no observed local turn")
	}
	controller := host.CodexInvocation{Transcript: thread.Path, Session: thread.ID, Parent: thread.ParentThreadID, Turn: thread.Turns[len(thread.Turns)-1].ID}
	release, err := f.acquireWriteWindow()
	if err != nil {
		return err
	}
	defer release()
	if body, readErr := os.ReadFile(filepath.Join(directory, "native-request.json")); readErr == nil {
		var prepared map[string]any
		if err := json.Unmarshal(body, &prepared); err != nil {
			return err
		}
		if prepared["host"] != nil {
			return nil
		}
		if prepared["controller"] != nil {
			body, err := json.Marshal(prepared["controller"])
			if err != nil {
				return err
			}
			var bound host.CodexInvocation
			if err := json.Unmarshal(body, &bound); err != nil {
				return err
			}
			if _, _, err := bound.Observe(); err != nil {
				return err
			}
			if bound.Session != controller.Session {
				return fmt.Errorf("host worker already has another actual prepared controller")
			}
			return nil
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	snap, err := f.engine.Load()
	if err != nil {
		return err
	}
	if _, _, err := snap.State.WorkerExecutionIdentity(material.ActionID, material.AttemptID, resumeActionID, f.definition); err != nil {
		return err
	}
	request["controller"] = controller
	return f.admission.write(func() error {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return err
		}
		body, err := json.Marshal(request)
		if err != nil {
			return err
		}
		return writeFileAtomic(filepath.Join(directory, "native-request.json"), body, 0600)
	})
}
