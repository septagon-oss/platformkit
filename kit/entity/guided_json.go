package entity

import "encoding/json"

// MarshalJSON preserves an empty item collection as an array without changing
// the provider's slice. The wire contract distinguishes absence from emptiness.
func (s GuidedStep) MarshalJSON() ([]byte, error) {
	type wire GuidedStep
	if s.Items == nil {
		s.Items = []GuidedItem{}
	}
	return json.Marshal(wire(s))
}

// MarshalJSON represents a session with no completions using an empty array.
func (p GuidedProgress) MarshalJSON() ([]byte, error) {
	type wire GuidedProgress
	if p.Completed == nil {
		p.Completed = []GuidedCompletion{}
	}
	return json.Marshal(wire(p))
}

// MarshalJSON represents a summary with no facts using an empty array.
func (s GuidedSummary) MarshalJSON() ([]byte, error) {
	type wire GuidedSummary
	if s.Facts == nil {
		s.Facts = []GuidedFact{}
	}
	return json.Marshal(wire(s))
}
