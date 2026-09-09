package internal

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// RegisterApprovalRegistrationRoutes composes a trusted user registrar with
// the same public abuse budget as email signup and password recovery. Passwords
// travel directly to the user owner; neither credentials nor hashes enter events.
func RegisterApprovalRegistrationRoutes(api *httpx.API, svc *Service, policy contracts.ApprovalRegistration) {
	httpx.Register(api, huma.Operation{
		OperationID: "auth-register", Method: http.MethodPost, Path: Path + "/register",
		Summary:     "Request an account requiring approval",
		Description: "Accepts a password, matching confirmation and terms consent. New accounts await operator approval before sign-in. Existing accounts retain their identity, credentials, roles and status; every accepted request receives the same acknowledgment.",
		Tags:        []string{"auth"}, DefaultStatus: http.StatusAccepted,
		Errors:     []int{http.StatusForbidden, http.StatusTooManyRequests},
		Extensions: map[string]any{httpx.EventsExtension: []string{user.EventRegistrationPending}},
	}, httpx.Public(), func(ctx context.Context, in *approvalRegistrationInput) (*doneOutput, error) {
		r, _ := httpx.RequestFrom(ctx)
		if !httpx.SameSite(r) {
			return nil, problem.New(http.StatusForbidden, "request an account from the registration page itself")
		}
		if !svc.MayAsk(ctx, ClientOf(r).IP) {
			return nil, problem.New(http.StatusTooManyRequests, "too many account requests from this address; wait and try again")
		}
		switch {
		case !in.Body.TermsAccepted:
			return nil, problem.New(http.StatusUnprocessableEntity, "accept the terms before requesting an account")
		case in.Body.Password != in.Body.Confirmation:
			return nil, problem.New(http.StatusUnprocessableEntity, "the password and confirmation must match")
		case strings.TrimSpace(in.Body.DisplayName) == "":
			return nil, problem.New(http.StatusUnprocessableEntity, "enter your full name")
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		_, err = policy.Users.RegisterPending(ctx, tx, user.PendingRegistration{
			Email: in.Body.Email, DisplayName: in.Body.DisplayName,
			Password: in.Body.Password, Roles: policy.Roles,
		})
		// Only the known email conflict is an acknowledgment. The owner leaves
		// this transaction usable; outages and other conflicts still fail.
		if errors.Is(err, user.ErrRegistrationExists) {
			return done(), nil
		}
		if err != nil {
			return nil, rest.Fault(err)
		}
		return done(), nil
	})
}

type approvalRegistrationInput struct {
	Body struct {
		Email         string `json:"email" minLength:"1" maxLength:"320" format:"email" doc:"Your email address"`
		DisplayName   string `json:"displayName" minLength:"1" maxLength:"200" doc:"Your full name"`
		Password      string `json:"password" minLength:"12" maxLength:"256" writeOnly:"true" doc:"At least twelve characters; spaces are allowed"`
		Confirmation  string `json:"confirmation" minLength:"12" maxLength:"256" writeOnly:"true" doc:"Repeat the password exactly"`
		TermsAccepted bool   `json:"termsAccepted" doc:"You have read and accept this application's terms"`
	}
}
