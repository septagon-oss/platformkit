package internal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The routes of the passkey half, mounted in the same group as the TOTP half
// because they belong to the same capability: what a person proves beside their
// password.
//
// Two of the six are about the caller themselves and declare no permission, the
// rule every session, password and TOTP route follows. The four sign-in legs are
// httpx.Public() and live on a surface that may set a cookie, for the reason
// factor_routes.go gives at length: the caller has no session by design, and the
// answer to a right answer is a session cookie — which the anonymous surface,
// which strips every Set-Cookie, could not deliver.
//
// Public means "no session is needed to ask", not "nothing need be proved". What
// the second-factor leg authenticates is the refusal behind the request — the
// window /login minted when it refused to finish without a second factor — and
// what the usernameless leg authenticates is a signature over a nonce this server
// minted for this tenant's host, and nothing else. Both check the site before
// anything else: a route that mints a credential is a CSRF surface.
//
// None of these is mounted on whether auth.factor_key was set, unlike the TOTP
// enrolment routes: a passkey writes no secret, so a deployment with no factor key
// has nothing to be unable to do here (contracts.ErrNoFactorKey says so).
func RegisterPasskeyRoutes(surfaces httpx.Surfaces, passkeys contracts.Passkeys, cookies Cookies) {
	app := surfaces.App

	httpx.Register(app, huma.Operation{
		OperationID: "auth-factor-passkey-begin",
		Method:      http.MethodPost,
		Path:        "/factors/passkey/begin",
		Summary:     "Begin enrolling a passkey",
		Description: "Asks this person's device for a passkey and returns the options a browser hands to navigator.credentials.create. Beyond the challenge, nothing is written: the passkey exists only when the finish leg has seen a signature nobody else could have produced, so closing this tab leaves no factor and changes nothing about how this person signs in.",
		Tags:        []string{"auth"},
	}, httpx.SignedIn(), handleBeginPasskeyRegistration(passkeys))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-factor-passkey-finish",
		Method:      http.MethodPost,
		Path:        "/factors/passkey/finish",
		Summary:     "Finish enrolling a passkey",
		Description: "Enrols the credential the ceremony answered, named what its owner called it. The response holds the public key's identifier to nobody: the factor, as the list shows it. A passkey already enrolled here for another account is refused with the reason, and enrols nothing.",
		Tags:        []string{"auth"}, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusUnprocessableEntity},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventFactorEnrolled,
		}},
	}, httpx.SignedIn(), handleFinishPasskeyRegistration(passkeys))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-challenge-passkey-begin",
		Method:      http.MethodPost,
		Path:        "/challenge/passkey/begin",
		Summary:     "Begin answering a second factor with a passkey",
		Description: "The passkey half of the sign-in /login refused to finish. The request is the same one whether or not this address holds a passkey - no allow list is sent, so this leg cannot be asked who has one.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusTooManyRequests},
	}, httpx.Public(), handleBeginPasskeyAssertion(passkeys))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-challenge-passkey-verify",
		Method:      http.MethodPost,
		Path:        "/challenge/passkey/verify",
		Summary:     "Answer a second factor with a passkey",
		Description: "The second half of a sign-in, and only ever of one: the first-factor proof /login minted is spent here before the passkey opens anything, so a signature over a challenge this server minted is still not a sign-in for an account that never offered its password. An unknown passkey, a passkey of another tenant's host, a person who cannot sign in, a wrong signature and an expired prompt are one answer at one cost.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventLoggedIn, contracts.EventFactorUsed, contracts.EventFactorSuspect,
		}},
	}, httpx.Public(), handleFinishPasskeyAssertion(passkeys, cookies))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-login-passkey-begin",
		Method:      http.MethodPost,
		Path:        "/login/passkey/begin",
		Summary:     "Begin signing in with a passkey alone",
		Description: "The usernameless ceremony: no address is offered and none is learned. A tenant that has not enabled passkey sign-in is refused with the reason, which is the one thing this leg refuses on its own.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusForbidden, http.StatusTooManyRequests},
	}, httpx.Public(), handleBeginPasskeySignIn(passkeys))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-login-passkey-verify",
		Method:      http.MethodPost,
		Path:        "/login/passkey/verify",
		Summary:     "Sign in with a passkey alone",
		Description: "Opens the session the passkey earned, for a tenant that enabled the door. Which door a ceremony belongs to is the server's own record from the moment it was begun, so a prompt begun where a password was expected cannot be answered here instead.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventLoggedIn, contracts.EventFactorUsed, contracts.EventFactorSuspect,
		}},
	}, httpx.Public(), handleFinishPasskeyAssertion(passkeys, cookies))
}

func handleBeginPasskeyRegistration(passkeys contracts.Passkeys) func(context.Context, *struct{}) (*passkeyChallengeOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*passkeyChallengeOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		userID, err := callerOf(ctx)
		if err != nil {
			return nil, err
		}
		challenge, err := passkeys.BeginPasskeyRegistration(ctx, tx, userID)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return passkeyChallenge(challenge), nil
	}
}

func handleFinishPasskeyRegistration(passkeys contracts.Passkeys) func(context.Context, *passkeyFinishInput) (*factorOutput, error) {
	return func(ctx context.Context, in *passkeyFinishInput) (*factorOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		// The command's own authorization recheck: the ceremony's owner is the
		// caller, and the ceremony's kind is the server's own record.
		userID, err := callerOf(ctx)
		if err != nil {
			return nil, err
		}
		response, err := json.Marshal(in.Body.Response)
		if err != nil {
			return nil, problem.New(http.StatusUnprocessableEntity, "that is not a credential response")
		}
		factor, err := passkeys.FinishPasskeyRegistration(ctx, tx, userID, in.Body.Ceremony, response, in.Body.Name)
		if err != nil {
			return nil, passkeyRefusal(err)
		}
		out := &factorOutput{}
		out.Body = *factor
		return out, nil
	}
}

func handleBeginPasskeyAssertion(passkeys contracts.Passkeys) func(context.Context, *passkeyBeginAssertionInput) (*passkeyChallengeOutput, error) {
	return func(ctx context.Context, in *passkeyBeginAssertionInput) (*passkeyChallengeOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		challenge, err := passkeys.BeginPasskeyAssertion(ctx, tx, in.Body.Email)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return passkeyChallenge(challenge), nil
	}
}

func handleBeginPasskeySignIn(passkeys contracts.Passkeys) func(context.Context, *struct{}) (*passkeyChallengeOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*passkeyChallengeOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		challenge, err := passkeys.BeginPasskeySignIn(ctx, tx)
		if err != nil {
			return nil, passkeyRefusal(err)
		}
		return passkeyChallenge(challenge), nil
	}
}

// handleFinishPasskeyAssertion mounts both sign-in legs. Neither route says which
// door it is: the ceremony's row does, written when the ceremony was begun, and the
// command reads it back. A caller cannot pick the door by what it sends, and a
// prompt begun where a password was expected is refused where none is — at the same
// cost and with the same answer as a bad signature.
func handleFinishPasskeyAssertion(passkeys contracts.Passkeys, cookies Cookies) func(context.Context, *passkeyVerifyInput) (*sessionOutput, error) {
	return func(ctx context.Context, in *passkeyVerifyInput) (*sessionOutput, error) {
		r, _ := httpx.RequestFrom(ctx)
		if !httpx.SameSite(r) {
			return nil, problem.New(http.StatusForbidden,
				"answer the passkey prompt from the sign-in page itself")
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		response, err := json.Marshal(in.Body.Response)
		if err != nil {
			return nil, problem.New(http.StatusUnprocessableEntity, "that is not a credential response")
		}
		session, identity, err := passkeys.FinishPasskeyAssertion(ctx, tx, in.Body.Ceremony, response, ClientOf(r))
		if err != nil {
			return nil, refusal(err)
		}
		return &sessionOutput{
			SetCookie: cookies.Session(session.ID, session.ExpiresAt),
			Body:      identity,
		}, nil
	}
}

// passkeyRefusal is the mapping these operations share: every refusal this half
// can answer, with the sentence a person can act on, and the status that says who
// can act on it.
func passkeyRefusal(err error) error {
	switch {
	case errors.Is(err, contracts.ErrPasskeySignInOff):
		// 403, and named: this is not the caller's fault and not the server's —
		// it is a tenant's decision, correctable by that tenant's administrator.
		return problem.New(http.StatusForbidden,
			"this organisation does not sign in with a passkey on its own; sign in with your password, then add a passkey")
	case errors.Is(err, contracts.ErrPasskeyExpired):
		return problem.New(http.StatusNotFound,
			"that enrolment has expired or was never begun here; begin again")
	case errors.Is(err, contracts.ErrPasskeyExists):
		return problem.New(http.StatusUnprocessableEntity,
			"that passkey is already enrolled for another account on this site; remove it there first")
	case errors.Is(err, contracts.ErrCredentials):
		return problem.New(http.StatusUnauthorized,
			"that passkey prompt expired or did not answer; try again")
	}
	return refusal(err)
}

func passkeyChallenge(challenge *contracts.PasskeyChallenge) *passkeyChallengeOutput {
	out := &passkeyChallengeOutput{}
	out.Body.Ceremony, out.Body.Options = challenge.Ceremony, challenge.Options
	return out
}

// The bodies. Each is its own type rather than a shared envelope because the
// catalogue reads these, and a response that could carry two shapes is a client
// that has to guess.

type passkeyChallengeOutput struct {
	Body contracts.PasskeyChallenge
}

type passkeyFinishInput struct {
	Body struct {
		Ceremony uuid.UUID `json:"ceremony" format:"uuid" doc:"The ceremony begin returned"`
		Response any       `json:"response" doc:"The credential response navigator.credentials.create handed back, verbatim"`
		Name     string    `json:"name,omitempty" maxLength:"40" doc:"What to call this passkey, up to 40 characters"`
	} `required:"true"`
}

type passkeyBeginAssertionInput struct {
	Body struct {
		Email string `json:"email" format:"email" maxLength:"320" doc:"The address that was just refused its second factor"`
	} `required:"true"`
}

type passkeyVerifyInput struct {
	Body struct {
		Ceremony uuid.UUID `json:"ceremony" format:"uuid" doc:"The ceremony begin returned"`
		Response any       `json:"response" doc:"The credential assertion navigator.credentials.get handed back, verbatim"`
	} `required:"true"`
}

type factorOutput struct {
	Body contracts.Factor
}
