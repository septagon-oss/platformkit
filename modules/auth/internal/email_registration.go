package internal

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func RegisterEmailRegistrationRoutes(surfaces httpx.Surfaces, svc *Service, policy contracts.EmailRegistration) {
	httpx.Register(surfaces.Public, huma.Operation{
		OperationID: "auth-register", Method: http.MethodPost, Path: "/register",
		Summary:     "Register with a password and email confirmation",
		Description: "Accepts a password, matching confirmation and terms consent. New accounts await mailbox verification; existing accounts remain unchanged. Check your email after the neutral acknowledgment.",
		Tags:        []string{"auth"}, DefaultStatus: http.StatusAccepted,
		Errors:     []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{user.EventRegistrationUnverified}},
	}, httpx.Public(), func(ctx context.Context, in *passwordRegistrationInput) (*doneOutput, error) {
		if err := emailRequest(ctx, svc); err != nil {
			return nil, err
		}
		if err := in.validate(); err != nil {
			return nil, err
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		_, err = policy.Users.RegisterUnverified(ctx, tx, user.PasswordRegistration{
			Email: in.Body.Email, DisplayName: in.Body.DisplayName,
			Password: in.Body.Password, Roles: policy.Roles,
		})
		if err != nil && !errors.Is(err, user.ErrRegistrationExists) {
			return nil, rest.Fault(err)
		}
		return done(), nil
	})

	httpx.Register(surfaces.Public, huma.Operation{
		OperationID: "auth-resend-verification", Method: http.MethodPost, Path: "/resend-verification",
		Summary:     "Request another email verification link",
		Description: "Queues a tenant-local request without revealing account existence, eligibility or recipient cooldown. A newly delivered link replaces the previous verification link.",
		Tags:        []string{"auth"}, DefaultStatus: http.StatusAccepted,
		Errors:     []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{contracts.EventVerificationRequested}},
	}, httpx.Public(), func(ctx context.Context, in *resendVerificationInput) (*doneOutput, error) {
		if err := emailRequest(ctx, svc); err != nil {
			return nil, err
		}
		email := contracts.EmailKey(in.Body.Email)
		allowed, err := svc.limiter.VerificationMail(ctx, email)
		if err != nil {
			return nil, problem.New(http.StatusServiceUnavailable, "account email requests are temporarily unavailable")
		}
		if !allowed {
			return done(), nil
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		askedAt, _ := httpx.RequestFrom(ctx)
		err = events.Publish(ctx, tx, contracts.EventVerificationRequested, contracts.VerificationRequested{
			Email: email, At: db.Now(), Served: httpx.ServedAuthority(askedAt),
		})
		if err != nil {
			return nil, rest.Fault(err)
		}
		return done(), nil
	})

	httpx.Register(surfaces.Public, huma.Operation{
		OperationID: "auth-verify-email", Method: http.MethodPost, Path: "/verify-email",
		Summary:     "Confirm the registered email address",
		Description: "Consumes the current unexpired verification token and activates its exact unverified account in one transaction. It preserves the registered password and roles and issues no session.",
		Tags:        []string{"auth"}, Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests},
		Extensions: map[string]any{httpx.EventsExtension: []string{user.EventEmailVerified}},
	}, httpx.Public(), func(ctx context.Context, in *verifyEmailInput) (*doneOutput, error) {
		r, _ := httpx.RequestFrom(ctx)
		if !httpx.SameSite(r) {
			return nil, problem.New(http.StatusForbidden, "confirm the email from the verification page itself")
		}
		if !svc.MayRedeem(ctx, ClientOf(r).IP) {
			return nil, problem.New(http.StatusTooManyRequests, "too many account link attempts; wait and try again")
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		if err := svc.verifyEmail(ctx, tx, policy.Users, in.Body.Token); err != nil {
			if errors.Is(err, contracts.ErrCredentials) {
				return nil, problem.New(http.StatusUnauthorized, "that verification link is invalid or has expired; request another link")
			}
			return nil, rest.Fault(err)
		}
		return done(), nil
	})

	// The read of the delivery record, for the one question the acknowledgment
	// cannot answer: "did the mail actually leave?" See contracts.MailLedger.
	//
	// Two words, not four. An id that left no record — because the cap wrote
	// none, because nobody has the address, because it is another tenant's,
	// because the caller typed nonsense — and an id whose mail went out are both
	// `pending`, and only a transport's refusal is `failed`. The reason is
	// contracts.MailReport's: a route that mails only accounts it found answers
	// "a mail left" about exactly the addresses that have an account, so a door
	// that said `sent` would undo the neutral acknowledgment one call later and
	// be a list of who has an account here. What this door still cannot be is
	// oracle-free: an id that answers `failed` necessarily had somebody to mail,
	// so it leaks existence with exactly the probability that a transport
	// refuses — which is why the sentence the shell says on `pending` is the
	// acknowledgment's own, and asks nothing back. The bounds are in these lines:
	// an unguessable handle the caller already holds about its own call, one
	// indexed lookup of the same shape either way, a budget of its own in front
	// of it (contracts.MailDeliveryAsks — not the redemption budget, which the
	// person needs for the link they are waiting for), RLS behind it, and a
	// response that names no address, no kind and no reason.
	httpx.Register(surfaces.Public, huma.Operation{
		OperationID: "auth-mail-delivery", Method: http.MethodPost, Path: "/mail-delivery",
		Summary:     "Ask whether the mail one call asked for was refused",
		Description: "Answers two words about the mail the call bearing this request id caused — failed when a transport refused it, pending otherwise — and names no address, no kind and no reason. A call that left no delivery record, one whose mail went out, and one from another tenant are answered identically, so the door cannot be walked for the addresses that have a record.",
		Tags:        []string{"auth"}, DefaultStatus: http.StatusOK,
		Errors: []int{http.StatusForbidden, http.StatusTooManyRequests},
	}, httpx.Public(), func(ctx context.Context, in *mailDeliveryInput) (*mailDeliveryOutput, error) {
		r, _ := httpx.RequestFrom(ctx)
		if !httpx.SameSite(r) {
			return nil, problem.New(http.StatusForbidden, "ask about the mail from the page that asked for it")
		}
		if !svc.MayAskAboutMail(ctx, ClientOf(r).IP) {
			return nil, problem.New(http.StatusTooManyRequests, "too many mail delivery questions; wait and try again")
		}
		out := &mailDeliveryOutput{}
		out.Body.State = notificationcontracts.MailStatePending
		// No ledger wired is no record to read, and the answer is the same one an
		// unknown id gets: a deployment must not be able to tell the two apart.
		if svc.mail.Mails == nil || in.Body.RequestID == "" {
			return out, nil
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		outcome, known, err := svc.mail.Mails.MailOutcome(ctx, tx, in.Body.RequestID)
		if err != nil {
			return nil, rest.Fault(err)
		}
		out.Body.State = notificationcontracts.MailReport(outcome, known)
		return out, nil
	})
}

// Registration and resend share delivery availability, request-origin and IP
// controls. Recipient cooldown is neutral and belongs only to explicit resend.
func emailRequest(ctx context.Context, svc *Service) error {
	r, _ := httpx.RequestFrom(ctx)
	if !httpx.SameSite(r) {
		return problem.New(http.StatusForbidden, "request an account email from this site itself")
	}
	if svc.mail.Mailer == nil || svc.mail.Hosts == nil {
		return problem.New(http.StatusServiceUnavailable, "account email delivery is unavailable")
	}
	if !svc.MayAsk(ctx, ClientOf(r).IP) {
		return problem.New(http.StatusTooManyRequests, "too many account requests from this address; wait and try again")
	}
	return nil
}

type resendVerificationInput struct {
	Body struct {
		Email string `json:"email" minLength:"1" maxLength:"320" format:"email" doc:"The address awaiting verification"`
	}
}

type verifyEmailInput struct {
	Body struct {
		Token string `json:"token" minLength:"1" maxLength:"128" writeOnly:"true" doc:"The verification link's one-time credential"`
	}
}

// mailDeliveryInput is the id of the caller's own call, and nothing else. The
// handle is the string the register or forgot call was already answered with in
// X-Request-ID (kit/httpx/request_id.go), which kit/events.Publish stores in the
// outbox row and the relay hands back to the worker that mails the link — so the
// page already holds what it needs to ask, and no new token, response field or
// correlation table is involved.
type mailDeliveryInput struct {
	Body struct {
		RequestID string `json:"requestId" maxLength:"64" doc:"The X-Request-ID of the call that asked for the mail"`
	}
}

type mailDeliveryOutput struct {
	// State is the one word the shell is allowed to say: pending or failed. No
	// recipient, no kind, no reason, no timestamp and no count — and no `sent`,
	// which is contracts.MailReport's refusal and the reason the reason there is
	// no 404: every answer has the same shape, so the door cannot be walked for
	// the addresses that have a record.
	Body struct {
		State string `json:"state" enum:"pending,failed" doc:"Whether the mail that call asked for was refused"`
	}
}
