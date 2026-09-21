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
		err = events.Publish(ctx, tx, contracts.EventVerificationRequested, contracts.VerificationRequested{Email: email, At: db.Now()})
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
