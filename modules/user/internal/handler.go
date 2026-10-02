package internal

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// RegisterRoutes mounts the lifecycle commands on the same resource the
// Spec mounts the five CRUD routes on.
//
// They are routes rather than fields of a PATCH because each is a rule about
// the state the user is in and each publishes an event. Setting a password
// through a generic update would put a password in a request body beside a
// display name, and in the update event beside it too; granting a role through
// one would make "who was made an administrator, and when" a question with no
// answer. spec.Immutable is the other half of that argument, and rest.Command
// is the part all three share.
func RegisterRoutes(surfaces httpx.Surfaces, spec rest.Spec[*contracts.User], svc contracts.Service) {
	// The invitation, which is a route of its own and not the create route with
	// a flag.
	//
	// POST to the collection writes a row and publishes user.user.created, and
	// nothing subscribes to that: an administrator who used it made somebody
	// who exists, cannot sign in, and was never told. Inviting is a different
	// intention — somebody should be able to get in — and user.invited is the
	// event the auth module listens for in order to mail them a link. It is a
	// collection of its own rather than a command on a user, because there is
	// no user yet to command.
	httpx.Register(surfaces.App, huma.Operation{
		OperationID:   "user-invitation-create",
		Method:        http.MethodPost,
		Path:          invitations,
		Summary:       "Invite somebody into this tenant",
		Description:   "Creates a person who cannot sign in yet and publishes user.invited, which is what mails them a link to choose a password. Inviting an address that is already here is a conflict. Roles are granted in the same transaction, so an invitation that names them is one event about who was made what.",
		Tags:          []string{"user"},
		DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity,
			http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventInvited, contracts.EventRolesSet,
		}},
	}, httpx.Permission(contracts.PermissionUserManage),
		func(ctx context.Context, in *inviteInput) (*rest.Item[*contracts.User], error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
			}
			u, err := svc.Invite(ctx, tx, in.Body.Email, in.Body.DisplayName)
			if err != nil {
				return nil, rest.Fault(err)
			}
			if len(in.Body.Roles) > 0 {
				// In the same transaction as the invitation, so that a person
				// who is invited as an administrator was never, for a moment, a
				// person with no roles who had already been mailed a link.
				if u, err = svc.SetRoles(ctx, tx, u.ID, in.Body.Roles); err != nil {
					return nil, rest.Fault(err)
				}
			}
			return &rest.Item[*contracts.User]{Body: u}, nil
		})

	rest.Command(surfaces, spec, "approve-registration",
		"Approve a pending registration",
		"Activates a pending account without changing its password or roles. Active accounts are unchanged; invitations and deactivated accounts cannot be approved.",
		[]string{contracts.EventRegistrationApproved},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.User, error) {
			principal, _ := tenancy.PrincipalFrom(ctx)
			return svc.ApproveRegistration(ctx, tx, id, principal.UserID)
		}, rest.CommandOptions{Auth: httpx.Permission(contracts.PermissionRegistrationApprove)})

	rest.Command(surfaces, spec, "set-password",
		"Set a user's password",
		"Stores an argon2id hash and makes an invited user active. It is not idempotent: setting a password to what it already was is still a password change, and somebody who did it deliberately has to see it in their own audit trail.",
		[]string{contracts.EventPasswordSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in passwordBody) (*contracts.User, error) {
			if err := svc.SetPassword(ctx, tx, id, in.Password); err != nil {
				return nil, err
			}
			return svc.Get(ctx, tx, id)
		}, rest.CommandOptions{})

	rest.Command(surfaces, spec, "roles",
		"Set a user's roles",
		"Replaces the roles this person holds. The same set again, in any order, changes nothing and publishes nothing.",
		[]string{contracts.EventRolesSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in rolesBody) (*contracts.User, error) {
			return svc.SetRoles(ctx, tx, id, in.Roles)
		}, rest.CommandOptions{})

	rest.Command(surfaces, spec, "handle",
		"Claim or change a user's handle",
		"The lower-case name this person answers to in this tenant, used in URLs and mentions. Renameable, because every key and audit entry names the uuid and nothing has to follow the name: the previous holder stays in user.handle_set. A handle somebody else holds is a conflict that names the rule and not the holder, and a handle cannot be released.",
		[]string{contracts.EventHandleSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in handleBody) (*contracts.User, error) {
			return svc.SetHandle(ctx, tx, id, in.Handle)
		}, rest.CommandOptions{Auth: httpx.Permission(contracts.PermissionUserManage)})

	rest.Command(surfaces, spec, "deactivate",
		"Deactivate a user",
		"Stops the person signing in. Their sessions stop working with them, because a session is only honoured for an active user, so there is no list of sessions to walk. Deactivating them again changes nothing.",
		[]string{contracts.EventDeactivated},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.User, error) {
			return svc.Deactivate(ctx, tx, id)
		}, rest.CommandOptions{})

	// The person's own language, on the collection and not on a row: the caller is
	// the subject, so an {id} in the path would be asking somebody to name
	// themselves, and a door that takes an id is a door that can be pointed at a
	// colleague. It is guarded by "signed in" rather than by a permission because
	// there is no tenant role that should have to be granted for a person to read
	// their own page in their own language — the same reason a basket command is
	// httpx.SignedIn(). See internal.Service.SetPreferredLocale for what the
	// command itself rechecks inside the authoritative transaction.
	rest.Command(surfaces, spec, "language",
		"Choose which language this person is answered in",
		"Writes the signed-in person's own language, which beats browser negotiation on every page they are shown and survives signing out and back in. An empty locale takes the choice back and leaves it to the browser and the tenant's default. A tag this tenant is not served in is refused: the set is the tenant's declaration and a choice outside it would be a preference that silently means nothing.",
		[]string{contracts.EventLanguageSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in languageBody) (*contracts.User, error) {
			var (
				u   *contracts.User
				err error
			)
			if in.ExpectedRevision != nil {
				u, err = svc.SetPreferredLocaleAt(ctx, tx, *in.ExpectedRevision, in.Locale)
			} else {
				u, err = svc.SetPreferredLocale(ctx, tx, in.Locale)
			}
			if errors.Is(err, contracts.ErrNoSignIn) {
				return nil, problem.New(http.StatusUnauthorized, "sign in to choose a language")
			}
			// A stale revision comes back as crud.ErrConflict, which rest.Fault turns
			// into a 409 whose detail names both revisions — and, because the command
			// returns no row when it refuses, the body holds no user at all. A body
			// that looked like the write that was refused would be a lie about it.
			return u, err
		}, rest.CommandOptions{
			Auth:       httpx.SignedIn(),
			Collection: true,
			// The caller is the subject, so the generated Users list offers no
			// control for it: a button labelled with the language question, sitting
			// beside a list of other people, would write the reader's own row.
			Self: true,
		})

	// The same choice, read and replaced by the person it is about.
	//
	// The command above takes an intent; these two take a read. A page that shows
	// the choice has seen it, and a page that has seen it can say so — which is the
	// only way its save can be refused when the row moved underneath it. That is
	// why the pair is GET/PUT on an address of its own rather than a second verb on
	// the collection: a POST says "do this", and these two say "this is what I read,
	// and this is what I now want". The module's own account control posts through
	// the same two commands, so the form and the JSON door decide one fact the same
	// way (modules/admin/internal/account.go).
	httpx.Register(surfaces.App, huma.Operation{
		OperationID: "user-own-language",
		Method:      http.MethodGet,
		Path:        ownLanguage,
		Summary:     "Which language this person is answered in",
		Description: "The language the person signed in as chose, empty while the browser and the tenant decide, with the revision it is held at. The revision is what to send back as expectedRevision: it moves once per accepted change, and a change that changes nothing leaves it where it was.",
		Tags:        []string{"user"},
		Errors:      []int{http.StatusUnauthorized, http.StatusServiceUnavailable},
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*languageOutput, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
		}
		language, err := svc.OwnLanguage(ctx, tx)
		if err != nil {
			if errors.Is(err, contracts.ErrNoSignIn) {
				return nil, problem.New(http.StatusUnauthorized, "sign in to choose a language")
			}
			return nil, rest.Fault(err)
		}
		return &languageOutput{Body: language}, nil
	})

	httpx.Register(surfaces.App, huma.Operation{
		OperationID: "user-own-language-set",
		Method:      http.MethodPut,
		Path:        ownLanguage,
		Summary:     "Choose which language this person is answered in",
		Description: "Replaces the signed-in person's own language at the revision it was read at. An expected revision that is not the one the row holds answers 409 and writes nothing: the page that brought it saw a choice somebody else has since replaced. A tag this tenant is not served in is refused the same way the command's is, and the answer is the choice and its new revision — never a user row, because a body that looks like the write that was refused is a lie about it.",
		Tags:        []string{"user"},
		Errors:      []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusConflict, http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventLanguageSet}},
	}, httpx.SignedIn(), func(ctx context.Context, in *languageInput) (*languageOutput, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
		}
		u, err := svc.SetPreferredLocaleAt(ctx, tx, in.Body.ExpectedRevision, in.Body.Locale)
		if err != nil {
			if errors.Is(err, contracts.ErrNoSignIn) {
				return nil, problem.New(http.StatusUnauthorized, "sign in to choose a language")
			}
			return nil, rest.Fault(err)
		}
		return &languageOutput{Body: contracts.Language{
			Locale: u.PreferredLocale, Revision: u.LocaleRevision,
		}}, nil
	})
}

// passwordBody and rolesBody are the arguments of the two commands that take
// one. deactivate takes none, so its body is the empty struct and a caller
// sends no body at all.
type passwordBody struct {
	Password string `json:"password" minLength:"12" maxLength:"256" doc:"The new password; at least twelve characters"`
}

type handleBody struct {
	Handle string `json:"handle" minLength:"3" maxLength:"32" doc:"Lower-case letters, digits and interior . _ -, three to thirty-two characters, and not a name that stands for the platform" example:"ada"`
}

// languageBody is the account language command's argument. Locale is one field and
// it is not a pointer: "" is the caller saying "stop asking me", which a nil and an
// empty string would have to be distinguished for.
//
// ExpectedRevision is the other half, and it is a pointer because the two answers
// it carries are different questions. Bringing the revision the choice was read at
// says "this is what I saw, and I mean to be the next write after it": a row that
// moved since is a conflict and not a second write. Bringing none says the caller
// stated an intent rather than a read — a person pressing a control labelled
// Portuguese need not have read anything first — and the row that comes back says
// which language they now hold either way.
type languageBody struct {
	Locale           string `json:"locale" maxLength:"35" doc:"One of the languages this tenant is served in, or empty to leave the choice to the browser" example:"pt-PT"`
	ExpectedRevision *int64 `json:"expectedRevision,omitempty" doc:"The revision the current choice was read at; a stale one is a conflict that writes nothing" example:"2"`
}

// ownLanguage is the address of the versioned pair. It names no person because
// neither route takes one: the transaction answers who is asking, which is the
// half of these commands that no caller may be trusted with.
const ownLanguage = "/me/language"

// languageInput is the PUT body: the choice and the revision it was read at.
// Unlike the command's argument, the revision is not optional here — a caller of
// this door read the choice through its GET, so it holds a number and the only
// honest question left is whether the row still agrees with it.
type languageInput struct {
	Body languageWrite `required:"true"`
}

type languageWrite struct {
	Locale           string `json:"language" maxLength:"35" doc:"One of the languages this tenant is served in, or empty to leave the choice to the browser" example:"pt-PT"`
	ExpectedRevision int64  `json:"expectedRevision" doc:"The revision GET /api/v1/user/me/language answered" example:"2"`
}

// languageOutput is what both routes answer with: the choice and its revision.
type languageOutput struct {
	Body contracts.Language
}

type rolesBody struct {
	Roles []string `json:"roles" doc:"The roles this person holds from now on" example:"admin"`
}

// invitations is where somebody is invited. It is beside the users collection
// rather than under it because an invitation is not a sub-resource of the user
// it creates.
const invitations = "/invitations"

// Invitation is what a caller sends to invite somebody. It is a named type and
// not the anonymous struct it was, because huma names a schema after the Go
// type behind it and an anonymous one came out of the generator as
// "InviteInputBody1" — a name with this package's plumbing and a deduplication
// counter in it, published in the OpenAPI document and compiled into every
// generated client.
type Invitation struct {
	Email       string   `json:"email" format:"email" maxLength:"320" doc:"The address to invite" example:"ada@acme.example.com"`
	DisplayName string   `json:"displayName,omitempty" maxLength:"200" doc:"Name to show" example:"Ada Lovelace"`
	Roles       []string `json:"roles,omitempty" doc:"Roles to grant in the same transaction" example:"admin"`
}

type inviteInput struct {
	Body Invitation `required:"true"`
}
