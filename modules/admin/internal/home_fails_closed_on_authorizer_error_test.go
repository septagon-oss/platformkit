package internal

// home_fails_closed_on_authorizer_error_test.go pins the home's refusal when the
// grant check cannot answer: an unavailable authorizer shows neither the primary
// action nor any destination, and counts nothing.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// brokenAuthorizer fails every grant check.
type brokenAuthorizer struct{}

func (brokenAuthorizer) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return false, errors.New("grant store unavailable")
}

func TestAnAuthorizerThatCannotAnswerRevealsNoActionAndNoCounter(t *testing.T) {
	counted := false
	body := homeWith(t, brokenAuthorizer{}, primaryOf(goToTasks), &counted)
	if primaryButton.MatchString(body) || strings.Contains(body, `href="/app/task/tasks"`) {
		t.Errorf("the home draws the primary action while the grant check fails:\n%s", body)
	}
	if counted {
		t.Error("the home counted people while the grant check fails")
	}
	for _, href := range []string{`href="/app/user/users"`, `href="/app/auth/roles"`} {
		if strings.Contains(body, href) {
			t.Errorf("the home shows %s while the grant check fails", href)
		}
	}
}
