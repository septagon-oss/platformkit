package screens_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/problem"
)

func TestFinalStepAuthorityAndInfrastructureFailuresDoNotEchoAnswers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		status  int
	}{
		{"revoked", problem.New(403, "Access revoked"), 403},
		{"missing", problem.New(404, "No longer present"), 404},
		{"outage", errors.New("private-database-canary"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := steppedNote()
			r.Create = func(context.Context, map[string]any) (map[string]any, error) { return nil, tc.failure }
			h := formResourceServer(t, r)
			s := continuation(t, stepRequest(h, r.Screen+"/new", nil), 200, 0)
			s = continuation(t, postStep(h, r.Screen+"/new/steps", s, "next", url.Values{"title": {"private-answer-canary"}}), 200, 1)
			s = continuation(t, postStep(h, r.Screen+"/new/steps", s, "next", nil), 200, 2)
			got := postStep(h, r.Screen, s, "submit", nil)
			if got.Code != tc.status || strings.Contains(got.Body.String(), "private-answer-canary") || strings.Contains(got.Body.String(), "private-database-canary") {
				t.Fatalf("failure exposed answers: %d %s", got.Code, got.Body)
			}
		})
	}
}

func TestChangedStepSchemaOffersRestartWithoutEchoingAnswers(t *testing.T) {
	r := steppedNote()
	h := formResourceServer(t, r)
	s := continuation(t, stepRequest(h, r.Screen+"/new", nil), 200, 0)
	s.Schema = "changed"
	got := postStep(h, r.Screen+"/new/steps", s, "next", url.Values{"title": {"private-answer-canary"}})
	if got.Code != 409 || strings.Contains(got.Body.String(), "private-answer-canary") || !strings.Contains(got.Body.String(), `href="`+r.Screen+`/new"`) || !strings.Contains(got.Body.String(), "Start again") {
		t.Fatalf("restart: %d %s", got.Code, got.Body)
	}
}
