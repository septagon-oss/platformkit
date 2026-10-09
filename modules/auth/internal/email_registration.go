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
	// Two words, not four, and neither of them about the address. Only a refusal is
	// `failed`, and only about the mail this call caused: the door reads the one
	// record whose request_id the caller holds and nothing else. `sent` is not
	// sayable, because a route that mails only accounts it found would otherwise
	// answer "a mail left" about exactly the addresses that have an account
	// (contracts.MailReport). And the tenant's newest record is not sayable either,
	// which is what this door got wrong twice: a caller whose own call left no
	// record was answered with whatever refusal the tenant happened to hold last,
	// and since sign-up is public, an attacker can register a mailbox its relay
	// rejects, refill that battery before each probe, and classify every address
	// that has no account as one that does. The door therefore answers about its
	// caller's record or says nothing — which is only a fair rule because every
	// branch of a call that names an address now hands one message to the transport
	// and records it (internal/no_link.go), so a caller is never left without a
	// record of its own for the answer to come from.
	//
	// The residue, stated because it cannot be designed away: a transport that
	// refuses one mailbox and takes another tells a fact about the address it
	// refused, and any word about it names it. That is the bound — an outright
	// transport failure, which is the failure this door exists to speak of, is the
	// same answer to every caller at that moment. It is why the sentence the shell
	// says is about the mail server rather than the person's account, why the
	// bounds are in these lines (an unguessable handle the caller already holds
	// about its own call, one indexed primary-key-shaped lookup, a budget of its
	// own in front of it — contracts.MailDeliveryAsks, not the redemption budget,
	// which the person needs for the link they are waiting for), RLS behind it, and
	// a response that names no address, no kind and no reason.
	httpx.Register(surfaces.Public, huma.Operation{
		OperationID: "auth-mail-delivery", Method: http.MethodPost, Path: "/mail-delivery",
		Summary:     "Ask whether the mail this call asked for was refused",
		Description: "Answers two words about the mail the call bearing this request id caused — failed when a transport refused that mail, pending otherwise — and names no address, no kind and no reason. Only this call's own delivery record is read: an id whose mail went out, an id that left no record and an id from another tenant are answered identically, so the door cannot be walked for the addresses that have a record or an account.",
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
		// The caller's own record, and nothing else: including when it says `sent`,
		// which no aggregate gets to overrule, and including the record of the
		// message that says no link was sent (internal/no_link.go), which is what a
		// call naming an address with nobody behind it leaves behind. known=false
		// therefore means a call this door has nothing to say about — an id nobody
		// mailed for, an id from another tenant, an id from before the record — and
		// it is answered with the one word every such caller gets.
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
	// the addresses that have a record. A `failed` says a transport refused a
	// mail — this call's, or, when this call left no record of its own, the newest
	// one this tenant holds, which is the same answer for every caller at that
	// own.
	Body struct {
		State string `json:"state" enum:"pending,failed" doc:"Whether the mail that call asked for was refused"`
	}
}
