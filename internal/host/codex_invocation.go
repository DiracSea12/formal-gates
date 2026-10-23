package host

import "fmt"

// CodexInvocation identifies original native facts for one issued worker call.
// Its turn is an observation boundary, not a workflow completion flag.
type CodexInvocation struct {
	Transcript string `json:"transcript"`
	Session    string `json:"session"`
	Parent     string `json:"parent"`
	Turn       string `json:"turn"`
}

func (ref CodexInvocation) Observe() (CodexThread, CodexTurn, error) {
	thread, err := ReadCodexThread(ref.Transcript)
	if err != nil {
		return CodexThread{}, CodexTurn{}, err
	}
	if err := thread.ValidateIdentity(ref.Session, ref.Parent); err != nil {
		return CodexThread{}, CodexTurn{}, err
	}
	for _, turn := range thread.Turns {
		if turn.ID == ref.Turn {
			return thread, turn, nil
		}
	}
	return CodexThread{}, CodexTurn{}, fmt.Errorf("original Codex worker turn %s is unavailable", ref.Turn)
}
