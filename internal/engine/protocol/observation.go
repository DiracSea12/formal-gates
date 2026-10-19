package protocol

import "formal-gates/internal/engine/decision"

// Observation carries the sampled facts and the reader of their exact sources.
// It belongs to one write operation; State and CAS retain workflow authority.
type Observation struct {
	Facts decision.Observation
	Read  func() (decision.Observation, error)
	// Confirmation is the channel that confirmed this write; empty means the
	// interactive form (or no confirmation happened for this event).
	Confirmation string
}

// Fingerprint re-reads the exact source set for the Store transaction.
func (o Observation) Fingerprint() (string, error) {
	facts := decision.Observation{}
	if o.Read != nil {
		var err error
		facts, err = o.Read()
		if err != nil {
			return "", err
		}
	}
	return facts.Digest()
}

func (e *Engine) facts() decision.Observation {
	if e.observation == nil {
		return decision.Observation{}
	}
	return e.observation.Facts
}

// observing projects the original readers onto operation-local typed samples.
// The Store reads those sources again; business callbacks share their first value.
func (e *Engine) observing(input Observation) *Engine {
	if e.observation != nil {
		return e
	}
	e = e.evaluatingConfiguration()
	local := *e
	local.sourceFacts = input.Facts
	input.Facts.Facts = append([]decision.Fact(nil), input.Facts.Facts...)
	local.observation = &input
	if read := e.cfg.CostSource; read != nil {
		var samples []struct {
			ref   CostSourceRef
			value ResolvedCostSource
		}
		local.cfg.CostSource = func(ref CostSourceRef) ResolvedCostSource {
			for _, sample := range samples {
				if sample.ref == ref {
					return sample.value
				}
			}
			value := read(ref)
			samples = append(samples, struct {
				ref   CostSourceRef
				value ResolvedCostSource
			}{ref, value})
			key := digestOfCanonical(ref)
			input.Facts.Facts = append(input.Facts.Facts, decision.Fact{Source: decision.SourceLifecycle, Key: "cost:" + key, Value: digestOfCanonical(value)})
			previous := input.Read
			input.Read = func() (decision.Observation, error) {
				facts := decision.Observation{}
				if previous != nil {
					var err error
					facts, err = previous()
					if err != nil {
						return facts, err
					}
				}
				facts.Facts = append(facts.Facts, decision.Fact{Source: decision.SourceLifecycle, Key: "cost:" + key, Value: digestOfCanonical(read(ref))})
				return facts, nil
			}
			return value
		}
	}
	if read := e.cfg.WorkerExecution; read != nil {
		var samples []struct {
			key   string
			value *SpawnReceiptPayload
		}
		local.cfg.WorkerExecution = func(pending PendingAction, attempt Attempt) (*SpawnReceiptPayload, error) {
			key := digestOfCanonical(struct {
				Action  string
				Attempt string
			}{pending.ActionID, attempt.ID})
			for _, sample := range samples {
				if sample.key == key {
					return sample.value, nil
				}
			}
			value, err := read(pending, attempt)
			if err != nil {
				return nil, err
			}
			samples = append(samples, struct {
				key   string
				value *SpawnReceiptPayload
			}{key, value})
			input.Facts.Facts = append(input.Facts.Facts, decision.Fact{Source: decision.SourceHost, Key: "execution:" + key, Value: digestOfCanonical(value)})
			previous := input.Read
			input.Read = func() (decision.Observation, error) {
				facts := decision.Observation{}
				if previous != nil {
					var err error
					facts, err = previous()
					if err != nil {
						return facts, err
					}
				}
				value, err := read(pending, attempt)
				if err != nil {
					return facts, err
				}
				facts.Facts = append(facts.Facts, decision.Fact{Source: decision.SourceHost, Key: "execution:" + key, Value: digestOfCanonical(value)})
				return facts, nil
			}
			return value, nil
		}
	}
	return &local
}
