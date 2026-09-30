package internal

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// secretRef is the shape a secret reference has to be: an environment
// variable's name. It is not a general "reference" grammar, because this
// repository has exactly one secret store — the environment, which is what
// kit/config reads — and a rule wider than the store would accept a value
// nothing can resolve. A deployment with a vault resolves its own names and
// declares the shape it uses there.
var secretRef = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)

// oidcRow is the six columns migrations/000030 added to tenants, read and
// written by name. It is its own struct rather than six fields on Tenant
// because the tenant entity travels — hosts, locales, every route's response
// body — and a provider nobody configured would then appear in all of it as
// five empty strings rather than as the absence it is.
type oidcRow struct {
	Issuer       *string        `gorm:"column:oidc_issuer"`
	ClientID     *string        `gorm:"column:oidc_client_id"`
	SecretRef    *string        `gorm:"column:oidc_secret_ref"`
	RedirectPath *string        `gorm:"column:oidc_redirect_path"`
	Registration string         `gorm:"column:oidc_registration"`
	Roles        pq.StringArray `gorm:"column:oidc_roles;type:text[]"`
}

// TableName pins the read onto tenants, which this row type covers six columns
// of and no more.
func (oidcRow) TableName() string { return "tenants" }

func (r oidcRow) settings() (contracts.OIDCSettings, bool) {
	if r.Issuer == nil || *r.Issuer == "" {
		return contracts.OIDCSettings{}, false
	}
	return contracts.OIDCSettings{
		Issuer: *r.Issuer, ClientID: deref(r.ClientID), SecretRef: deref(r.SecretRef),
		RedirectPath: deref(r.RedirectPath), Registration: r.Registration, Roles: []string(r.Roles),
	}, true
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// rolesArray is `oidc_roles` spelled as the column is: a NOT NULL text[] whose
// empty value is `{}`. A nil slice is not an empty array — pq turns it into a
// NULL, which the column refuses — and "no roles to hand out" is the ordinary
// answer for a tenant registered as `existing`, so the empty case is the one
// that has to be right.
func rolesArray(in contracts.OIDCSettings) pq.StringArray {
	if in.Roles == nil {
		return pq.StringArray{}
	}
	return pq.StringArray(in.Roles)
}

// SetOIDC says which provider one tenant's people sign in against.
//
// The refusals are the shape rule of migrations/000030 spelled in Go for the
// parts SQL cannot see — that an issuer is a URL rather than a sentence, that a
// secret reference is a name — each naming the field an operator has to fix.
// Writing the same values again changes nothing and publishes nothing: a retry
// must not read as two changes in an audit trail.
func (s *Service) SetOIDC(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in contracts.OIDCSettings) (*contracts.Tenant, error) {
	if err := validOIDC(in); err != nil {
		return nil, err
	}
	t, err := s.Get(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	before, _ := s.oidcOfSystem(tx, id)
	if sameOIDC(before, in) {
		return t, nil
	}
	err = tx.DB().Table("tenants").Where("id = ?", id).Updates(map[string]any{
		"oidc_issuer": in.Issuer, "oidc_client_id": in.ClientID, "oidc_secret_ref": in.SecretRef,
		"oidc_redirect_path": in.RedirectPath, "oidc_registration": registration(in),
		"oidc_roles": rolesArray(in), "updated_at": db.Now(),
	}).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	if err := events.PublishFor(ctx, tx, id, contracts.EventOIDCSet, contracts.OIDCSet{
		TenantID: id, Issuer: in.Issuer, ClientID: in.ClientID, SecretRef: in.SecretRef,
		RedirectPath: in.RedirectPath, Registration: registration(in), Roles: in.Roles,
		WasIssuer: before.Issuer, WasClient: before.ClientID, Replaced: before.Issuer != "",
		At: db.Now(),
	}); err != nil {
		return nil, err
	}
	return s.Get(ctx, tx, id)
}

// ClearOIDC takes a tenant's provider away. Every column goes back to the state
// the CHECK reads as "no provider here", in one write, because the pair of
// CHECKs only holds for the whole row and a half-cleared tenant is the
// half-provider the file exists to make unstatable.
func (s *Service) ClearOIDC(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	t, err := s.Get(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	before, ok := s.oidcOfSystem(tx, id)
	if !ok {
		return t, nil
	}
	err = tx.DB().Table("tenants").Where("id = ?", id).Updates(map[string]any{
		"oidc_issuer": nil, "oidc_client_id": nil, "oidc_secret_ref": nil,
		"oidc_redirect_path": nil, "oidc_registration": contracts.RegistrationExisting,
		"oidc_roles": pq.StringArray{}, "updated_at": db.Now(),
	}).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	return t, events.PublishFor(ctx, tx, id, contracts.EventOIDCCleared, contracts.OIDCCleared{
		TenantID: id, Issuer: before.Issuer, ClientID: before.ClientID, At: db.Now(),
	})
}

// OIDCOf is the sign-in's read: this tenant's provider, in the transaction the
// Host header resolved, which is the whole of why two tenants authenticate
// against two issuers in one process.
func (s *Service) OIDCOf(_ context.Context, tx db.Tx[db.Tenant]) (*contracts.OIDCSettings, bool, error) {
	var row oidcRow
	err := tx.DB().Model(oidcRow{}).Where("id = ?", db.TenantOf(tx).ID).
		Select("oidc_issuer", "oidc_client_id", "oidc_secret_ref", "oidc_redirect_path",
			"oidc_registration", "oidc_roles").Take(&row).Error
	if err != nil {
		return nil, false, crud.Classify(err)
	}
	settings, ok := row.settings()
	if !ok {
		return nil, false, nil
	}
	return &settings, true, nil
}

// oidcOfSystem is the same read from the control plane, for the "did anything
// change" comparison and for the previous issuer the event carries.
func (s *Service) oidcOfSystem(tx db.Tx[db.System], id uuid.UUID) (contracts.OIDCSettings, bool) {
	var row oidcRow
	err := tx.DB().Model(oidcRow{}).Where("id = ?", id).
		Select("oidc_issuer", "oidc_client_id", "oidc_secret_ref", "oidc_redirect_path",
			"oidc_registration", "oidc_roles").Take(&row).Error
	if err != nil {
		return contracts.OIDCSettings{}, false
	}
	return row.settings()
}

// sameOIDC is "nothing changed", written once: the two lists compared element
// by element and the mode through its default, because a row written with no
// mode and a row written with `existing` are one tenant.
func sameOIDC(a, b contracts.OIDCSettings) bool {
	return a.Issuer == b.Issuer && a.ClientID == b.ClientID && a.SecretRef == b.SecretRef &&
		a.RedirectPath == b.RedirectPath && registration(a) == registration(b) &&
		slices.Equal(a.Roles, b.Roles)
}

// registration is the mode with its default spelled out, so that a row written
// without one says `existing` rather than "" and the CHECK has one thing to
// check.
func registration(in contracts.OIDCSettings) string {
	if in.Registration == "" {
		return contracts.RegistrationExisting
	}
	return in.Registration
}

// validOIDC is the write's rule. An issuer is an https URL with a host and no
// query — http is allowed only for a local host, the same exception kit/config
// makes for the process's own issuer, because a test issuer and a developer's
// Keycloak are both local and neither is https. A path is allowed: an issuer
// that names a realm (https://id.example/realms/people) is the common case and
// refusing it would refuse the product rather than the mistake.
func validOIDC(in contracts.OIDCSettings) error {
	u, err := url.Parse(in.Issuer)
	switch {
	case err != nil || u.Host == "":
		return fmt.Errorf("%w: oidc.issuer %q is not a URL", crud.ErrInvalid, in.Issuer)
	case u.Scheme != "https" && !config.Local(u.Host):
		return fmt.Errorf("%w: oidc.issuer %q is not https", crud.ErrInvalid, in.Issuer)
	case u.RawQuery != "" || u.Fragment != "":
		return fmt.Errorf("%w: oidc.issuer %q carries a query; an issuer is a base URL", crud.ErrInvalid, in.Issuer)
	case in.ClientID == "":
		return fmt.Errorf("%w: oidc.clientId is empty", crud.ErrInvalid)
	case !secretRef.MatchString(in.SecretRef):
		return fmt.Errorf("%w: oidc.secretRef %q is not an environment variable's name", crud.ErrInvalid, in.SecretRef)
	}
	switch registration(in) {
	case contracts.RegistrationDisabled, contracts.RegistrationExisting, contracts.RegistrationProvision:
	default:
		return fmt.Errorf("%w: oidc.registration %q is not disabled, existing or provision", crud.ErrInvalid, in.Registration)
	}
	if registration(in) == contracts.RegistrationProvision && len(in.Roles) == 0 {
		return fmt.Errorf("%w: provision with no roles would make people who can do nothing", crud.ErrInvalid)
	}
	for _, role := range in.Roles {
		if strings.TrimSpace(role) == "" {
			return fmt.Errorf("%w: oidc.roles carries an empty name", crud.ErrInvalid)
		}
	}
	return nil
}
