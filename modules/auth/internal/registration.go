package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// RegisterRegistrationRoutes is called only by compositions that supply a
// registrar. Registration and password recovery share the public mail-request
// budget, so switching endpoints cannot multiply the allowed traffic.
func RegisterRegistrationRoutes(api *httpx.API, svc *Service) {
	httpx.Register(api, huma.Operation{
		OperationID: "auth-register", Method: http.MethodPost,
		Path: Path + "/register", Summary: "Request a member account",
		Description: "Queues registration in this tenant. Check your email to choose a password; existing accounts keep their roles and status. The acknowledgment does not reveal whether the account exists.",
		Tags:        []string{"auth"}, DefaultStatus: http.StatusAccepted,
		Errors:     []int{http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{contracts.EventRegistrationRequested}},
	}, httpx.Public(), func(ctx context.Context, in *registrationInput) (*doneOutput, error) {
		if svc.mail.Mailer == nil || svc.mail.Hosts == nil {
			return nil, problem.New(http.StatusServiceUnavailable, "account email delivery is unavailable")
		}
		r, _ := httpx.RequestFrom(ctx)
		if !svc.MayAsk(ctx, ClientOf(r).IP) {
			return nil, problem.New(http.StatusTooManyRequests, "too many account requests from this address; wait and try again")
		}
		candidate := user.User{Email: in.Body.Email, DisplayName: in.Body.DisplayName, Status: user.StatusInvited}
		if err := candidate.Validate(ctx); err != nil {
			return nil, rest.Fault(fmt.Errorf("%w: %s", crud.ErrInvalid, err))
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		err = events.Publish(ctx, tx, contracts.EventRegistrationRequested, contracts.RegistrationRequested{
			Email: candidate.Email, DisplayName: candidate.DisplayName, At: db.Now(),
		})
		if err != nil {
			return nil, rest.Fault(err)
		}
		return done(), nil
	})
}

// RegistrationSubscription resolves the address outside the public request.
// The transaction lock serializes separate requests for one tenant/address;
// delivery claims alone cannot deduplicate independently submitted requests.
func RegistrationSubscription(svc *Service, users contracts.RegistrationUsers) events.Subscription {
	return events.Subscription{
		Module: "auth", Name: contracts.EventRegistrationRequested,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], event events.Event) error {
			var asked contracts.RegistrationRequested
			if err := json.Unmarshal(event.Payload, &asked); err != nil {
				return fmt.Errorf("auth: read registration request: %w", err)
			}
			candidate := user.User{Email: asked.Email, DisplayName: asked.DisplayName, Status: user.StatusInvited}
			if err := candidate.Validate(ctx); err != nil {
				return fmt.Errorf("auth: invalid registration request: %w", err)
			}
			key := fmt.Sprintf("auth/registration/%s/%x", db.TenantOf(tx).ID, contracts.Hash(contracts.EmailKey(candidate.Email)))
			if err := tx.DB().Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
				return fmt.Errorf("auth: lock registration: %w", err)
			}
			existing, err := users.ByEmail(ctx, tx, candidate.Email)
			if err == nil {
				if existing.Status == user.StatusInvited {
					return svc.Offer(ctx, tx, existing.ID)
				}
				return nil
			}
			if !errors.Is(err, crud.ErrNotFound) {
				return err
			}
			created, err := users.Invite(ctx, tx, candidate.Email, candidate.DisplayName)
			if err != nil {
				return err
			}
			_, err = users.SetRoles(ctx, tx, created.ID, []string{contracts.RoleMember})
			return err
		},
	}
}

type registrationInput struct {
	Body struct {
		Email       string `json:"email" minLength:"1" maxLength:"320" format:"email" doc:"Your email address"`
		DisplayName string `json:"displayName,omitempty" maxLength:"200" doc:"Your display name"`
	}
}
