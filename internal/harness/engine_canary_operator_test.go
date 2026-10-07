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
