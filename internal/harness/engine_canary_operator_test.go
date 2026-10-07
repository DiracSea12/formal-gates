//go:build !windows

package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

// Callers serialize human input with approvalMu; the pass owns its deadline.
func (c *engineCanary) readHumanDecision(purpose string) (string, error) {
	if !time.Now().Before(c.deadline) {
		return "", fmt.Errorf("%s exceeded the %s pass budget", purpose, canaryRunBudget)
	}
	decision := make(chan struct {
		line string
		err  error
	}, 1)
	go func() {
		line, err := c.approvalInput.ReadString('\n')
		decision <- struct {
			line string
			err  error
		}{line, err}
	}()
	timer := time.NewTimer(time.Until(c.deadline))
	defer timer.Stop()
	select {
	case <-timer.C:
		return "", fmt.Errorf("%s exceeded the %s pass budget", purpose, canaryRunBudget)
	case answer := <-decision:
		if answer.err != nil {
			return "", fmt.Errorf("read %s: %w", purpose, answer.err)
		}
		if !time.Now().Before(c.deadline) {
			return "", fmt.Errorf("%s exceeded the %s pass budget", purpose, canaryRunBudget)
		}
		return answer.line, nil
	}
}

// The handoff uses the public observation payload. Semantic dimensions and
// problem identities come from the Operator and the core, without flag edits.
func (c *engineCanary) readAdjudication(state *protocol.State, wave *protocol.ValidationWave, reason string) (protocol.OperatorObservationPayload, error) {
	materials := map[string]string{}
	for _, target := range wave.Targets {
		if target.ResultAction == "" {
			continue
		}
		dir, err := canaryFindingMaterialDirectory(c.jobs, state, target)
		if err != nil {
			return protocol.OperatorObservationPayload{}, err
		}
		materials[target.ResultAction] = filepath.Join(dir, "materials.json")
	}
	paths, err := json.Marshal(materials)
	if err != nil {
		return protocol.OperatorObservationPayload{}, err
	}
	statePath := filepath.Join(c.project, filepath.FromSlash(facade.EngineNamespace), c.run.RunID, "state.json")
	if _, err := fmt.Fprintf(c.approvalOutput, "canary Operator adjudication: run=%q wave=%q candidate=%q state=%q materials=%s\nAutomatic decision unavailable: %s\nEnter one JSON OperatorObservationPayload line with Subject set to this wave and Validation.Kind=ADJUDICATE; supply every finding dimension yourself. NEW receives its problemId from the core; use the accepted ID for later SAME_AS observations.\n", c.run.RunID, wave.ID, wave.Candidate.Identity, statePath, paths, reason); err != nil {
		return protocol.OperatorObservationPayload{}, err
	}
	line, err := c.readHumanDecision("Operator adjudication")
	if err != nil {
		return protocol.OperatorObservationPayload{}, err
	}
	var observation protocol.OperatorObservationPayload
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&observation); err != nil {
		return observation, fmt.Errorf("Operator adjudication JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return observation, fmt.Errorf("Operator adjudication needs exactly one JSON observation")
	}
	if observation.Subject != wave.ID || observation.Validation == nil || observation.Validation.Kind != "ADJUDICATE" {
		return observation, fmt.Errorf("Operator adjudication must address this wave with ADJUDICATE")
	}
	return observation, nil
}

func (c *engineCanary) adjudicateWithOperator(state *protocol.State, wave *protocol.ValidationWave, reason string) {
	c.t.Helper()
	if c.approvalMu != nil {
		c.approvalMu.Lock()
		defer c.approvalMu.Unlock()
	}
	pending := func() int {
		count := 0
		for _, finding := range c.run.Validation.Findings {
			if finding.WaveID == wave.ID && len(finding.History) == 0 {
				count++
			}
		}
		return count
	}
	for remaining := pending(); remaining > 0; remaining = pending() {
		observation, err := c.readAdjudication(state, wave, reason)
		if err != nil {
			c.fail("Operator adjudication: %v", err)
		}
		acceptance := c.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &observation})
		resolutions := []protocol.FindingResolution{}
		for _, item := range observation.Validation.Findings {
			history := c.run.Validation.Findings[item.FindingID].History
			resolutions = append(resolutions, history[len(history)-1])
		}
		accepted, err := json.Marshal(struct {
			Acceptance protocol.Acceptance          `json:"acceptance"`
			Findings   []protocol.FindingResolution `json:"findings"`
		}{acceptance, resolutions})
		if err != nil {
			c.fail("Operator adjudication receipt: %v", err)
		}
		if _, err := fmt.Fprintf(c.approvalOutput, "canary adjudication accepted: %s\n", accepted); err != nil {
			c.fail("Operator adjudication receipt: %v", err)
		}
		if pending() >= remaining {
			c.fail("Operator adjudication made no progress on this wave's pending findings")
		}
	}
}

// Review findings retain their native evidence; only explicit Operator input
// supplies a correction through the core's existing request and decision path.
func (c *engineCanary) readReviewCorrection(action facade.ReviewActionFace, candidate facade.ReviewCandidateFace, finding facade.ReviewFindingFace) (review.CorrectionInput, error) {
	statePath := filepath.Join(c.project, filepath.FromSlash(facade.EngineNamespace), c.run.RunID, "state.json")
	packet, err := json.Marshal(struct {
		Action    facade.ReviewActionFace    `json:"action"`
		Candidate facade.ReviewCandidateFace `json:"candidate"`
		Finding   facade.ReviewFindingFace   `json:"finding"`
	}{action, candidate, finding})
	if err != nil {
		return review.CorrectionInput{}, err
	}
	if _, err := fmt.Fprintf(c.approvalOutput, "canary Operator review finding: run=%q state=%q nativeEvidence=%q binding=%s\nEnter one JSON review.CorrectionInput line for this exact candidate or finding; supply your own decision and reason. INVALIDATE_RESULT requires empty findingId and supersedes plus an explicit invalidity. No finding is automatically dismissed.\n", c.run.RunID, statePath, canaryJobDirectory(c.jobs, c.run.RunID, candidate.DispatchID, ""), packet); err != nil {
		return review.CorrectionInput{}, err
	}
	line, err := c.readHumanDecision("Operator review correction")
	if err != nil {
		return review.CorrectionInput{}, err
	}
	var correction review.CorrectionInput
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&correction); err != nil {
		return correction, fmt.Errorf("Operator review correction JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return correction, fmt.Errorf("Operator review correction needs exactly one JSON value")
	}
	if string(correction.Action) != action.Action || correction.DispatchID != candidate.DispatchID || correction.ResultDigest != candidate.ResultDigest || correction.RequirementRevision != candidate.RequirementRevision || correction.ExpectedEffectiveRevision != action.EffectiveRevision {
		return correction, fmt.Errorf("Operator review correction differs from the displayed finding binding")
	}
	if strings.EqualFold(strings.TrimSpace(correction.Decision), "INVALIDATE_RESULT") {
		if correction.FindingID != "" || correction.Supersedes != "" {
			return correction, fmt.Errorf("Operator whole-result invalidation must not carry findingId or supersedes")
		}
	} else if correction.FindingID != finding.ID || correction.Supersedes != finding.EffectiveDecisionID {
		return correction, fmt.Errorf("Operator review correction differs from the displayed finding binding")
	}
	return correction, nil
}

func (c *engineCanary) reviewFindingsWithOperator() bool {
	for _, action := range c.run.Review.Actions {
		if len(action.Candidates) == 0 {
			continue
		}
		boundRevision := action.Candidates[len(action.Candidates)-1].RequirementRevision
		for _, candidate := range action.Candidates {
			if candidate.Excluded || boundRevision == "" || candidate.RequirementRevision != boundRevision {
				continue
			}
			for _, finding := range candidate.Findings {
				if finding.EffectiveDecision != "" {
					continue
				}
				explicitAsk := false
				for _, ask := range c.run.PendingAsks {
					if ask.Control == protocol.ControlReviewFindingDecision {
						for _, option := range ask.Options {
							if strings.HasSuffix(string(option.ID), ":"+finding.ID) {
								explicitAsk = true
							}
						}
					}
				}
				if action.EffectiveStatus != "FAIL" && !explicitAsk {
					continue
				}
				if c.approvalMu != nil {
					c.approvalMu.Lock()
					defer c.approvalMu.Unlock()
				}
				correction, err := c.readReviewCorrection(action, candidate, finding)
				if err != nil {
					c.fail("Operator review correction: %v", err)
				}
				choice := protocol.AskOptionID(correction.Decision)
				if !strings.EqualFold(strings.TrimSpace(correction.Decision), "INVALIDATE_RESULT") {
					choice += protocol.AskOptionID(":" + correction.FindingID)
				}
				var requestID protocol.RequestID
				var token string
				for _, ask := range c.run.PendingAsks {
					if ask.Control == protocol.ControlReviewFindingDecision {
						for _, option := range ask.Options {
							if option.ID == choice {
								requestID, token = protocol.RequestID(ask.RequestID), ask.Token
								break
							}
						}
					}
				}
				if requestID == "" {
					request, err := protocol.NewRequestEvent(c.eventID(), protocol.ControlReviewFindingDecision, protocol.AskOption{ID: choice, Label: string(choice)})
					if err != nil {
						c.fail("Operator review request: %v", err)
					}
					c.submit(request)
					for _, ask := range c.run.PendingAsks {
						if ask.RequestID == string(request.ID) {
							requestID, token = protocol.RequestID(ask.RequestID), ask.Token
							break
						}
					}
				}
				if requestID == "" || token == "" {
					c.fail("Operator review request has no current Ask")
				}
				event, err := protocol.NewReviewDecideEvent(c.eventID(), requestID, token, choice, protocol.ReviewDecisionPayload{Correction: &correction})
				if err != nil {
					c.fail("Operator review decision: %v", err)
				}
				acceptance := c.submit(event)
				for _, updated := range c.run.Review.Actions {
					if updated.Action == action.Action && updated.EffectiveRevision <= action.EffectiveRevision {
						c.fail("Operator review correction made no progress on its finding")
					}
				}
				receipt, err := json.Marshal(acceptance)
				if err != nil {
					c.fail("Operator review receipt: %v", err)
				}
				if _, err := fmt.Fprintf(c.approvalOutput, "canary review correction accepted: %s\n", receipt); err != nil {
					c.fail("Operator review receipt: %v", err)
				}
				return true
			}
		}
	}
	return false
}
