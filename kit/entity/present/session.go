package present

import (
	"slices"
	"strings"
)

// SessionCapability distinguishes absent commands from declared but denied ones.
type SessionCapability struct {
	Declared bool    `json:"declared"`
	Command  *string `json:"command"`
}

// sessionKeyPrefix is the catalogue namespace the words a guided screen says are
// translated under, and the name each label keeps inside ResolvedSession.Labels.
const sessionKeyPrefix = "screens.session."

// ResolvedSession contains final session copy and request-scoped capabilities.
// Labels are keyed by the last part of the catalogue key they were translated
// from, so a renderer asks for "step" and never spells the namespace.
type ResolvedSession struct {
	ContractVersion int               `json:"contractVersion"`
	Complete        SessionCapability `json:"complete"`
	Finish          SessionCapability `json:"finish"`
	Labels          map[string]string `json:"labels" nullable:"true"`
	Motion          string            `json:"motion"`
}

// sessionPolicy resolves the guided contract: which commands this caller is
// offered, and the final copy in the request language.
func sessionPolicy(in Input) *ResolvedSession {
	if !in.Hints.Session {
		return nil
	}
	r := &ResolvedSession{ContractVersion: 1, Motion: "fade", Labels: map[string]string{}}
	capability := func(declared bool, verb string) SessionCapability {
		c := SessionCapability{Declared: declared}
		if declared && slices.ContainsFunc(in.Commands, func(c CommandInput) bool { return c.Verb == verb && !c.Hints.System }) {
			c.Command = new(verb)
		}
		return c
	}
	r.Complete = capability(in.SessionCommands[0], "complete-set")
	r.Finish = capability(in.SessionCommands[1], "finish")
	for key, english := range in.SessionWords {
		value := english
		if translated := in.word(key, english); slices.Equal(sessionSlots(translated), sessionSlots(english)) {
			value = translated
		}
		r.Labels[strings.TrimPrefix(key, sessionKeyPrefix)] = value
	}
	return r
}

func sessionSlots(text string) []string {
	var slots []string
	for {
		before, rest, found := strings.Cut(text, "{")
		if strings.Contains(before, "}") {
			return []string{"invalid"}
		}
		if !found {
			break
		}
		name, after, closed := strings.Cut(rest, "}")
		if !closed || strings.Contains(name, "{") {
			return []string{"invalid"}
		}
		slots = append(slots, name)
		text = after
	}
	slices.Sort(slots)
	return slots
}
