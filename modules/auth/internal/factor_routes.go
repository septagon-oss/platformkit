package internal

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The routes of the second factor. Registered as their own group, beside the
// OIDC and email-registration groups, and mounted only for a deployment that set
// a factor key — a route that could only answer "this installation cannot seal a
// secret" is a route with nothing to say, which is the rule the two OIDC legs
// already follow.
//
// Five of the six are about the caller themselves and so declare no permission,
// the rule every session and password route follows — and a credential narrowed
// to a list of scopes is refused them by the kernel, because they exercise their
// caller's whole authority: see httpx.SignedIn and tenancy.Principal.Permissions.
// The sixth, the challenge, declares httpx.Public() authorisation and mounts on
// the workspace surface, which is how /login and /password/reset already work: the
// caller has no session by design, and the answer to a right code is a session
// cookie. It cannot live on the anonymous surface, which sets no cookie and is
// skipped by Authenticate by design — buffer.withholdCookies takes every Set-Cookie
// off a response on that surface and the kernel answers such a route with a 500 —
// so a challenge answered there could sign nobody in.
func RegisterFactorRoutes(surfaces httpx.Surfaces, factors contracts.Factors, cookies Cookies) {
	app := surfaces.App

	httpx.Register(app, huma.Operation{
		OperationID: "auth-factor-totp-begin",
		Method:      http.MethodPost,
		Path:        "/factors/totp/begin",
		Summary:     "Begin enrolling a TOTP authenticator",
		Description: "Mints a secret and shows it once, as base32 and as an otpauth URI. Nothing is enrolled: the factor exists only when a code proves the secret reached the device, so closing this tab leaves no row and changes nothing about how this person signs in.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, httpx.SignedIn(), handleBeginTOTP(factors))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-factor-totp-finish",
		Method:      http.MethodPost,
		Path:        "/factors/totp/finish",
		Summary:     "Finish enrolling a TOTP authenticator",
		Description: "Enrols the secret, having checked a code against it, and hands back the recovery codes — the only time any of this is written down, and the response a person is meant to save. A wrong code enrols nothing and issues no codes.",
		Tags:        []string{"auth"}, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusUnauthorized, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventFactorEnrolled, contracts.EventRecoveryCodesIssued,
		}},
	}, httpx.SignedIn(), handleFinishTOTP(factors))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-factor-list",
		Method:      http.MethodGet,
		Path:        "/factors",
		Summary:     "List my second factors",
		Description: "What this account proves beside its password, with no secret and no material of any kind: an empty list means the password still signs this person in alone.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, httpx.SignedIn(), handleListFactors(factors))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-factor-withdraw",
		Method:      http.MethodDelete,
		Path:        "/factors/{id}",
		Summary:     "Withdraw one of my second factors",
		Description: "Stops one factor working. The last one on the account is refused: a person cannot, by one click, turn their account back into a password — enrol another first, then retire this one.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventFactorWithdrawn}},
	}, httpx.SignedIn(), handleWithdrawFactor(factors))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-recovery-codes-rotate",
		Method:      http.MethodPost,
		Path:        "/factors/recovery/rotate",
		Summary:     "Replace my recovery codes",
		Description: "Retires every unused code and issues a fresh set, shown once. For the person who has reason to think the set leaked. An account with no factor is refused: codes stand in for a factor and are not one.",
		Tags:        []string{"auth"}, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventRecoveryCodesIssued,
		}},
	}, httpx.SignedIn(), handleRotateRecoveryCodes(factors))

	httpx.Register(app, huma.Operation{
		OperationID: "auth-challenge-verify",
		Method:      http.MethodPost,
		Path:        "/challenge/verify",
		Summary:     "Answer a second factor",
		Description: "The second half of a sign-in Login refused to finish. Takes the address and the code, or the address and a recovery code, and opens the session the password had already earned. A wrong code, a replayed step, a spent recovery code, an address with no factor and an address nobody has are one answer at one cost.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventLoggedIn, contracts.EventLoginFailed, contracts.EventRecoveryCodeUsed,
		}},
	}, httpx.Public(), handleVerifyFactor(factors, cookies))
}

// refusal503 is the one mapping this capability's outage-shaped failure needs:
// ErrNoFactorKey is not the caller's fault and not a server bug — it is a
// deployment that has not decided to offer a second factor yet — and it reads as
// 503 with the reason in it, which is what "correctable, by the operator" means
// in a response.
func refusal503(err error) error {
	if errors.Is(err, contracts.ErrNoFactorKey) {
		return problem.New(http.StatusServiceUnavailable,
			"this installation sets no factor key, so no second factor can be enrolled")
	}
	return rest.Fault(err)
}

// hostOf is the host a request arrived at, without the port: the label an
// authenticator shows, and the value the OIDC leg already uses for its redirect.
func hostOf(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, _ := strings.Cut(r.Host, ":")
	return host
}

// callerOf is the signed-in person these routes are about, or the 403 that says
// a route about the caller cannot answer for nobody.
func callerOf(ctx context.Context) (uuid.UUID, error) {
	caller, ok := tenancy.PrincipalFrom(ctx)
	if !ok || caller.UserID == uuid.Nil {
		return uuid.Nil, problem.New(http.StatusForbidden, "this operation answers for the caller themselves")
	}
	return caller.UserID, nil
}

func handleBeginTOTP(factors contracts.Factors) func(context.Context, *struct{}) (*totpEnrolmentOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*totpEnrolmentOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		userID, err := callerOf(ctx)
		if err != nil {
			return nil, err
		}
		r, _ := httpx.RequestFrom(ctx)
		enrolment, err := factors.BeginTOTP(ctx, tx, userID)
		if err != nil {
			return nil, refusal503(err)
		}
		// The host this request arrived at is the issuer label, because that host
		// is how a tenant is reached here and the authenticator shows the label to
		// somebody deciding whether to trust a prompt. No constant in this module
		// names one (rule 4).
		enrolment.URI = otpauthURI(hostOf(r), enrolment.Account, enrolment.Secret)
		out := &totpEnrolmentOutput{}
		out.Body = *enrolment
		return out, nil
	}
}

func handleFinishTOTP(factors contracts.Factors) func(context.Context, *finishTOTPInput) (*recoveryCodesOutput, error) {
	return func(ctx context.Context, in *finishTOTPInput) (*recoveryCodesOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		userID, err := callerOf(ctx)
		if err != nil {
			return nil, err
		}
		factor, codes, err := factors.FinishTOTP(ctx, tx, userID, in.Body.Secret, in.Body.Code)
		if err != nil {
			return nil, refusal503(err)
		}
		out := &recoveryCodesOutput{}
		out.Body.Factor, out.Body.Codes = factor, codes
		return out, nil
	}
}

func handleListFactors(factors contracts.Factors) func(context.Context, *struct{}) (*factorsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*factorsOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		userID, err := callerOf(ctx)
		if err != nil {
			return nil, err
		}
		items, err := factors.ListFactors(ctx, tx, userID)
		if err != nil {
			return nil, rest.Fault(err)
		}
		out := &factorsOutput{}
		out.Body.Items, out.Body.Total = items, len(items)
		return out, nil
	}
}

func handleWithdrawFactor(factors contracts.Factors) func(context.Context, *factorIDInput) (*doneOutput, error) {
	return func(ctx context.Context, in *factorIDInput) (*doneOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		userID, err := callerOf(ctx)
		if err != nil {
			return nil, err
		}
		if err := factors.WithdrawFactor(ctx, tx, userID, in.ID); err != nil {
			return nil, rest.Fault(err)
		}
		return done(), nil
	}
}

func handleRotateRecoveryCodes(factors contracts.Factors) func(context.Context, *struct{}) (*recoveryCodesOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*recoveryCodesOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		userID, err := callerOf(ctx)
		if err != nil {
			return nil, err
		}
		codes, err := factors.RotateRecoveryCodes(ctx, tx, userID)
		if err != nil {
			return nil, rest.Fault(err)
		}
		out := &recoveryCodesOutput{}
		out.Body.Codes = codes
		return out, nil
	}
}

func handleVerifyFactor(factors contracts.Factors, cookies Cookies) func(context.Context, *verifyFactorInput) (*sessionOutput, error) {
	return func(ctx context.Context, in *verifyFactorInput) (*sessionOutput, error) {
		r, _ := httpx.RequestFrom(ctx)
		// The same question the login route asks, for the same reason: this is a
		// route that mints a credential rather than spending one, so a form on
		// another site posting here could leave the visitor signed in as the
		// account whose code they somehow held.
		if !httpx.SameSite(r) {
			return nil, problem.New(http.StatusForbidden,
				"answer the second factor from the sign-in page itself")
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		session, identity, err := factors.VerifySecondFactor(ctx, tx, in.Body.Email, in.Body.Code, ClientOf(r))
		if err != nil {
			return nil, refusal(err)
		}
		return &sessionOutput{
			SetCookie: cookies.Session(session.ID, session.ExpiresAt),
			Body:      identity,
		}, nil
	}
}

// The bodies. Each is its own type rather than a shared envelope because the
// catalogue reads these: a response that could carry two shapes is a client that
// has to guess.

type totpEnrolmentOutput struct {
	Body contracts.TOTPEnrolment
}

type finishTOTPInput struct {
	Body struct {
		Secret string `json:"secret" maxLength:"64" doc:"The secret the enrolment showed, as the authenticator app received it"`
		Code   string `json:"code" maxLength:"16" doc:"The six-digit code the app is showing"`
	} `required:"true"`
}

type verifyFactorInput struct {
	Body struct {
		Email string `json:"email" format:"email" maxLength:"320" doc:"The address that was signed in with"`
		Code  string `json:"code" maxLength:"40" doc:"The code from the app, or one of the recovery codes"`
	} `required:"true"`
}

type factorIDInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The factor to withdraw, as the list showed it"`
}

type factorsOutput struct {
	Body struct {
		Items []*contracts.Factor `json:"items"`
		Total int                 `json:"total"`
	}
}

type recoveryCodesOutput struct {
	Body struct {
		Factor *contracts.Factor `json:"factor,omitempty"`
		Codes  []string          `json:"codes" example:"9f2c…-…"`
	}
}
