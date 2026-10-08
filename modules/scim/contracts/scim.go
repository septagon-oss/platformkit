// Package contracts is the SCIM 2.0 boundary of the platform: the resources an
// identity provider provisions, and the commands that move them.
//
// SCIM is not a shape over this platform's own directory — it is somebody
// else's protocol, sent by a machine that has been configured with one address,
// one media type and one set of field names. The contracts therefore name the
// protocol's vocabulary (a User has a userName, a Group has members, every
// resource has a meta) and stop there: what a person *is* here belongs to
// modules/user, which owns the row, the status, the roles and the events. This
// package translates; it holds no second opinion about anybody.
//
// It names no SDK, no driver, no HTTP framework and no HTML: the wire types are
// JSON, the commands take the caller's transaction, and the transport lives in
// internal/, which is where a media type and a status code belong.
package contracts

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// PermissionProvision is the one grant this module asks for. It is held by a
// credential the tenant's identity provider presents, and it covers both doors
// the module opens — provisioning a person and mapping a group — because both
// are one act of administration seen from two addresses, and a caller with the
// reach to create a person has the reach to say which role a group brings.
//
// There is deliberately no `scim:read` beside it. The only reads here are of the
// directory the writes act on, and a second key that can list the people a first
// key can write would be a privilege nobody asked for.
const PermissionProvision = "scim:provision"

// ProtocolMediaType is the media type SCIM names its JSON with. The kernel owns
// the registration of a media type — a module cannot reach the API's format
// table — so the composition lists this one in its own options and the string
// stays out of the module, which serves whatever it is mounted with.
const ProtocolMediaType = "application/scim+json"

// The protocol's fixed identifiers. They are in the specification, which is why
// they are constants and not configuration: a tenant that could rename them
// would be running a dialect no client ships support for.
const (
	// SchemaUser is the urn a User resource names in its schemas array.
	SchemaUser = "urn:ietf:params:scim:schemas:core:2.0:User"
	// SchemaGroup is the urn a Group resource names in its schemas array.
	SchemaGroup = "urn:ietf:params:scim:schemas:core:2.0:Group"
	// SchemaListResponse is the urn a list result names.
	SchemaListResponse = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	// SchemaPatchOp is the urn a PATCH body names.
	SchemaPatchOp = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	// ResourceTypeUser and ResourceTypeGroup name the two types this module
	// serves. Endpoint is the address the discovery document points a client at,
	// composed by the kernel's mount table rather than written out here.
	ResourceTypeUser  = "User"
	ResourceTypeGroup = "Group"
)

// A refusal in this module is one of kit/fault's three values, wrapped: a
// resource that is not here is ErrNotFound, an attribute or a filter the service
// cannot honour is ErrInvalid, and a write that contradicts state that already
// exists — reprovisioning a person whose group still names a role nobody declared
// — is ErrConflict. The kernel answers those three; the module invents no fourth,
// and the SCIM error document is not a place to start.
//
// One refusal has no fault of its own and needs none:
//
//   - A group with no mapping is a group with no role. It is a group's ordinary
//     state, not a failure, so MapGroup answers "" and GetGroup answers "" and
//     neither is an error.

// Meta is every resource's own account of itself. ResourceVersion is the
// revision a client could send back as an ETag; this service reads it rather
// than issuing one — see Service's note on concurrency.
type Meta struct {
	ResourceType string `json:"resourceType,omitempty"`
	Created      string `json:"created,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	Location     string `json:"location,omitempty"`
	Version      string `json:"version,omitempty"`
}

// Name is the part of a person's name SCIM splits. This platform keeps one
// display name, so the round trip is lossy in one direction and says so: see
// FoldName for how a write folds the halves together.
type Name struct {
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	Formatted  string `json:"formatted,omitempty"`
}

// Email is one of a person's addresses. SCIM allows many; this platform has one
// per person, so the list a client sends is read for at most one value.
type Email struct {
	Value   string `json:"value,omitempty"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// MemberRef is one entry of a group's membership: who is in it, by id.
type MemberRef struct {
	Value   string `json:"value,omitempty"`
	Display string `json:"display,omitempty"`
	Type    string `json:"type,omitempty"`
	Ref     string `json:"$ref,omitempty"`
}

// UserResource is a person as the protocol sees them.
//
// Every field comes from one place: id, userName, displayName and active read
// the user row; groups is derived from this module's own membership table; meta
// reads the row's own timestamps. Nothing here is a copy the module keeps.
type UserResource struct {
	Schemas     []string   `json:"schemas"`
	ID          uuid.UUID  `json:"id"`
	ExternalID  string     `json:"externalId,omitempty"`
	UserName    string     `json:"userName"`
	DisplayName string     `json:"displayName,omitempty"`
	Name        *Name      `json:"name,omitempty"`
	Emails      []Email    `json:"emails,omitempty"`
	Active      bool       `json:"active"`
	Groups      []GroupRef `json:"groups,omitempty"`
	Meta        Meta       `json:"meta"`
}

// GroupRef is a group as seen from the person who is in it — the read-only half
// of membership, which moves only through the Groups door.
type GroupRef struct {
	Value   string `json:"value,omitempty"`
	Type    string `json:"type,omitempty"`
	Display string `json:"display,omitempty"`
}

// GroupResource is a collection the identity provider declares, with the role it
// brings when its name is mapped.
type GroupResource struct {
	Schemas     []string    `json:"schemas"`
	ID          uuid.UUID   `json:"id"`
	ExternalID  string      `json:"externalId,omitempty"`
	DisplayName string      `json:"displayName"`
	Members     []MemberRef `json:"members,omitempty"`
	// Role is the extension this platform adds, and the reason a group exists
	// here at all: without a mapping a mirrored organisational unit is a label.
	Role string `json:"role,omitempty"`
	Meta Meta   `json:"meta"`
}

// GroupMapping is the tenant's own statement of which role a group brings, read
// and written at the workspace door rather than at the protocol's.
type GroupMapping struct {
	// Group is the name as the provider spells it, folded to the form the table
	// keys on, so what a screen shows is what a connector will match.
	Group string `json:"group"`
	// Role is "" when the group brings none, which is a state and not a failure.
	Role string `json:"role"`
}

// UserDraft is what a POST or PUT says about a person. It is the request's
// shape, not the stored one: the service decides which of these fields reach
// modules/user and which are refused.
type UserDraft struct {
	Schemas     []string
	ExternalID  string
	UserName    string
	DisplayName string
	Name        *Name
	Emails      []Email
	Active      *bool
	// Password is present in the protocol and refused here. It is a field rather
	// than an omission so that "you sent a password" is a refusal the caller can
	// read instead of an attribute silently dropped.
	Password string
	// Groups is read off the wire and ignored: membership moves through Groups.
	Groups []GroupRef
}

// GroupDraft is what a POST or PUT says about a group.
type GroupDraft struct {
	Schemas     []string
	ExternalID  string
	DisplayName string
	Members     []MemberRef
	Role        string
}

// Operation is one PATCH operation: add, remove or replace, at a path, with a
// value whose meaning is the path's. It stays JSON here because the protocol's
// PATCH body is JSON in a shape no Go type describes honestly.
type Operation struct {
	Op    string          `json:"op,omitempty"`
	Path  string          `json:"path,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

// ListQuery is a list request: the filter, and the two bounds.
//
// Only one filter is honoured — userName eq "<address>" — and anything else
// refuses the request. The reason is the cost of getting it wrong: a connector
// that asked for a subset and was handed the whole directory would go on to
// act on everybody it meant to exclude.
type ListQuery struct {
	Filter     string
	StartIndex int
	Count      int
}

// UserList and GroupList are the protocol's list envelope. TotalResults counts
// what the filter matched in the tenant, not what Items holds.
type UserList struct {
	Schemas      []string        `json:"schemas"`
	TotalResults int             `json:"totalResults"`
	StartIndex   int             `json:"startIndex"`
	ItemsPerPage int             `json:"itemsPerPage"`
	Resources    []*UserResource `json:"Resources,omitempty"`
}

// GroupList is the same envelope for groups.
type GroupList struct {
	Schemas      []string         `json:"schemas"`
	TotalResults int              `json:"totalResults"`
	StartIndex   int              `json:"startIndex"`
	ItemsPerPage int              `json:"itemsPerPage"`
	Resources    []*GroupResource `json:"Resources,omitempty"`
}

// ProviderConfig is the service's own description: what it supports, and what a
// client must not try. It is a document rather than configuration — every field
// answers a question about this module's code, which is the only honest source.
type ProviderConfig struct {
	Schemas               []string      `json:"schemas"`
	SupportingETag        bool          `json:"supportETag"`
	Filter                ConfigFeature `json:"filter"`
	ChangePassword        ConfigFeature `json:"changePassword"`
	Sort                  ConfigFeature `json:"sort"`
	Bulk                  ConfigFeature `json:"bulk"`
	AuthenticationSchemes []AuthScheme  `json:"authenticationSchemes,omitempty"`
	Patch                 ConfigFeature `json:"patch"`
	Meta                  Meta          `json:"meta"`
}

// ConfigFeature is one yes-or-no capability, with the number behind a yes that
// has one.
type ConfigFeature struct {
	Supported      bool  `json:"supported"`
	MaxOperations  int   `json:"maxOperations,omitempty"`
	MaxPayloadSize int64 `json:"maxPayloadSize,omitempty"`
}

// AuthScheme is how the service says a client authenticates. The protocol's
// bearerToken scheme is the one the kernel's own scoped key is.
type AuthScheme struct {
	Type        string `json:"type,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	SpecURI     string `json:"specURI,omitempty"`
	Primary     bool   `json:"primary,omitempty"`
}

// ResourceType is one of the two documents a client reads to learn the schema.
type ResourceType struct {
	Schemas      []string `json:"schemas"`
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name,omitempty"`
	Endpoint     string   `json:"endpoint,omitempty"`
	Description  string   `json:"description,omitempty"`
	Schema       string   `json:"schema,omitempty"`
	SchemaSchema string   `json:"schemaExtensions,omitempty"`
}

// Schema is the attribute document. It is the protocol's own data structure —
// a list of attribute descriptions — and it is written out rather than derived
// because a client's error messages read it, so its wording is the module's.
type Schema struct {
	ID          string            `json:"id"`
	Definition  string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Attributes  []SchemaAttribute `json:"attributes,omitempty"`
	Meta        Meta              `json:"meta"`
}

// SchemaAttribute is one line of that document.
type SchemaAttribute struct {
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	SubAttributes   []SchemaAttribute `json:"subAttributes,omitempty"`
	MultiValued     bool              `json:"multiValued,omitempty"`
	CanonicalValues []string          `json:"canonicalValues,omitempty"`
	Required        bool              `json:"required,omitempty"`
	CaseExact       bool              `json:"caseExact,omitempty"`
	Mutability      string            `json:"mutability,omitempty"`
	Returned        string            `json:"returned,omitempty"`
	Uniqueness      string            `json:"uniqueness,omitempty"`
	Description     string            `json:"description,omitempty"`
}

// Service is the module's command surface. Every command takes the caller's
// transaction, so a write and the events its underlying commands publish commit
// together, and every read is scoped by the same transaction's tenant — there
// is no method here that can ask about another tenant's groups, because there is
// no transaction that would answer it.
//
// Concurrency: a SCIM connector sends no If-Match, so there is no revision to
// check against. The service does not invent one. Every write it performs is a
// command on modules/user that rechecks its own state inside its own transaction
// (SetRoles locks the row, ConfirmAddress refuses a status it cannot activate),
// and the two membership statements are guarded by the row lock their own table
// gives them; a lost update therefore needs two connectors writing the same
// group at the same instant, which the lock's owner will answer before this
// module grows an ETag nobody asked for.
type Service interface {
	// ProviderConfig, ResourceTypes, ResourceType, Schemas and Schema are the
	// discovery documents. They take no transaction because they read no rows:
	// what this service supports is a fact about its code.
	ProviderConfig(ctx context.Context) (*ProviderConfig, error)
	ResourceTypes(ctx context.Context) (ResourceTypeList, error)
	ResourceType(ctx context.Context, name string) (*ResourceType, error)
	Schemas(ctx context.Context) (SchemaList, error)
	Schema(ctx context.Context, id string) (*Schema, error)

	// ListUsers pages the tenant's people. The filter is the one described on
	// ListQuery; an unsupported one refuses the request.
	ListUsers(ctx context.Context, tx db.Tx[db.Tenant], q ListQuery) (*UserList, error)
	GetUser(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*UserResource, error)
	// CreateUser brings a person into being. active false (or absent) invites
	// them; active true says the identity provider vouches for the mailbox and
	// activates them in the same transaction. Neither half is this module's to
	// invent: Invite and ConfirmAddress are modules/user's commands, and a
	// person who arrives with no password signs in through the tenant's own
	// door.
	CreateUser(ctx context.Context, tx db.Tx[db.Tenant], in UserDraft) (*UserResource, error)
	ReplaceUser(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in UserDraft) (*UserResource, error)
	PatchUser(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, ops []Operation) (*UserResource, error)
	// DeactivateUser is what DELETE answers here: the person stops being able to
	// sign in and the row, its address and its history stay. A connector that
	// reprovisions the same person later finds the same person back rather than a
	// second one — which is the whole reason this is not a delete.
	DeactivateUser(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error

	ListGroups(ctx context.Context, tx db.Tx[db.Tenant], q ListQuery) (*GroupList, error)
	GetGroup(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*GroupResource, error)
	CreateGroup(ctx context.Context, tx db.Tx[db.Tenant], in GroupDraft) (*GroupResource, error)
	ReplaceGroup(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in GroupDraft) (*GroupResource, error)
	PatchGroup(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, ops []Operation) (*GroupResource, error)
	DeleteGroup(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error

	// GroupMappings is the tenant's mapping table, and MapGroup writes one row
	// of it. Mapping is the workspace's door (user:read, user:manage) rather
	// than the protocol's: it is a role assignment decided in advance by an
	// administrator at a screen, not an act of provisioning.
	GroupMappings(ctx context.Context, tx db.Tx[db.Tenant]) (MappingList, error)
	MapGroup(ctx context.Context, tx db.Tx[db.Tenant], group string, role string) (*GroupMapping, error)
}

// RoleDirectory answers whether this tenant knows a role name at all.
//
// Role definitions are the auth module's table, and role assignments are
// modules/user's column; this module owns neither, so it declares the question
// and the composition answers it. Without it, mapping a group to a role nobody
// declared would be a binding that grants nothing, discovered the day somebody
// expects it to have granted something.
type RoleDirectory interface {
	// HasRole reports whether this tenant has a role of that name. An error is
	// an outage and refuses the write: "I cannot tell" must not read as "no".
	HasRole(ctx context.Context, tx db.Tx[db.Tenant], name string) (bool, error)
}

// The wire's own name, on every document this service answers with.
//
// SCIM names its media type — application/scim+json — and a client that sent it
// expects it back. Registering a media type with the API is the composition's
// job (a module cannot reach the API's format table, and the kernel's own
// responses are JSON by a rule no module may bend); naming it on a response body
// is this module's, and the kernel honours the answer. It is a plain method on
// the body type rather than a header written at the route, so that a route cannot
// forget it and a refusal — which is the kernel's own document — cannot be made
// to look like one of these by the route that raised it.
const mediaType = ProtocolMediaType + "; charset=utf-8"

func (UserResource) ContentType(string) string     { return mediaType }
func (UserList) ContentType(string) string         { return mediaType }
func (GroupResource) ContentType(string) string    { return mediaType }
func (GroupList) ContentType(string) string        { return mediaType }
func (GroupMapping) ContentType(string) string     { return mediaType }
func (MappingList) ContentType(string) string      { return mediaType }
func (ProviderConfig) ContentType(string) string   { return mediaType }
func (ResourceType) ContentType(string) string     { return mediaType }
func (ResourceTypeList) ContentType(string) string { return mediaType }
func (Schema) ContentType(string) string           { return mediaType }
func (SchemaList) ContentType(string) string       { return mediaType }

// MappingList, ResourceTypeList and SchemaList are the protocol's list answers.
// They are named types rather than slices because the document a client reads is
// an array body, and an array body has to name its own media type like any other.
type (
	MappingList      []*GroupMapping
	ResourceTypeList []*ResourceType
	SchemaList       []*Schema
)

// FoldName is how the protocol's split name becomes this platform's one name.
// The order is the document's: a formatted name, else a display name, else the
// two halves joined. It is a function rather than a few lines in two places so
// that a create and a replace cannot disagree about what somebody is called.
func FoldName(displayName string, name *Name) string {
	if name != nil {
		if s := strings.TrimSpace(name.Formatted); s != "" {
			return s
		}
		if s := strings.TrimSpace(name.GivenName + " " + name.FamilyName); s != "" {
			return s
		}
	}
	return strings.TrimSpace(displayName)
}
