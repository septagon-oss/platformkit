package pkit

import (
	"errors"
	"fmt"
)

// CompositionError is a composition that does not resolve, as values. Validate,
// Plan, Build, Run, Explain and Describe return it when the resolver, the app's
// name, a module's build or the manifest gates refuse; a caller reaches it with
// errors.As. Problems is one entry per refusal, in the order they were found, and
// Suppressed names the modules whose build did not run because a module they wait
// on refused. Error is the text those methods have always returned, one line per
// problem, "pkit: <app>: <cause>: <sentence>", and Unwrap returns those lines, so
// errors.Is still reaches the error a module's build returned.
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
