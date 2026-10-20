package pkit

import (
	"errors"
	"fmt"
)

// CompositionError is a composition that does not resolve, as values a caller
// reaches with errors.As. Validate, Explain and Describe return it for every
// refusal; Plan, Build and Run do too, except for Plan's own checks (an empty
// name, the settings, the ports and roles) and the engine's, which stay plain.
// Problems is one entry per refusal, in the order found; Suppressed names the
// modules whose build did not run because a module they wait on refused. Error is
// the text those methods always returned, one "pkit: <app>: <cause>: <sentence>"
// line per problem; Unwrap returns those lines, so errors.Is reaches a build's error.
type CompositionError struct {
	App        string
	Problems   []DescribedProblem
	Suppressed []DescribedSuppression
	lines      []error
}

func (e *CompositionError) Error() string   { return errors.Join(e.lines...).Error() }
func (e *CompositionError) Unwrap() []error { return e.lines }

// DescribedSuppression is a module whose build did not run because a module it
// waits on, directly or through another suppressed module, refused. It adds no
// line to the error's text: the refused module's sentence is the one that is
// true. A module that waits on two refused modules is attributed to the first in
// Use order.
type DescribedSuppression struct {
	Module string `json:"module"`
	By     string `json:"by"`
}

// joinIssues is the one constructor of a CompositionError: one line per issue,
// prefixed with the app and the method that caused it, and the same issues as
// fields. The sentence a DescribedProblem carries and the sentence in the text
// come from the same fmt.Errorf call, which is why a machine view needs no message
// format of its own. Call it only with at least one issue, so a clean composition
// returns a nil error rather than a typed nil.
func joinIssues(app string, issues []issue, suppressed []DescribedSuppression) *CompositionError {
	e := &CompositionError{App: app, Problems: describedProblems(issues), Suppressed: suppressed}
	for _, in := range issues {
		e.lines = append(e.lines, fmt.Errorf("pkit: %s: %s: %w", app, in.method, in.err))
	}
	return e
}
