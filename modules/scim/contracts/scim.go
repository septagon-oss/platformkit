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
	GivenName  string `json:"givenName,omitempty" required:"false"`
	FamilyName string `json:"familyName,omitempty" required:"false"`
	Formatted  string `json:"formatted,omitempty" required:"false"`
}

// Email is one of a person's addresses. SCIM allows many; this platform has one
// per person, so the list a client sends is read for at most one value.
type Email struct {
	Value   string `json:"value,omitempty" required:"false"`
	Type    string `json:"type,omitempty" required:"false"`
	Primary bool   `json:"primary,omitempty" required:"false"`
}

// MemberRef is one entry of a group's membership: who is in it, by id.
type MemberRef struct {
	Value   string `json:"value,omitempty" required:"false"`
	Display string `json:"display,omitempty" required:"false"`
	Type    string `json:"type,omitempty" required:"false"`
	Ref     string `json:"$ref,omitempty" required:"false"`
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
//
// Every field is optional to the schema and judged by the service, because the
// protocol's own requirement is contextual — a create is named by userName or by
// one email address — and a schema that demanded the union of the spellings would
// refuse the request that is legal. It is also why the tags name the protocol's
// keys: without them a marshaller would derive UserName and ExternalID from the
// Go field names, and every connector on earth would be told its own document was
// malformed.
type UserDraft struct {
	Schemas     []string `json:"schemas,omitempty" required:"false"`
	ExternalID  string   `json:"externalId,omitempty" required:"false"`
	UserName    string   `json:"userName,omitempty" required:"false"`
	DisplayName string   `json:"displayName,omitempty" required:"false"`
	Name        *Name    `json:"name,omitempty" required:"false"`
	Emails      []Email  `json:"emails,omitempty" required:"false"`
	Active      *bool    `json:"active,omitempty" required:"false"`
	// Password is present in the protocol and refused here. It is a field rather
	// than an omission so that "you sent a password" is a refusal the caller can
	// read instead of an attribute silently dropped.
	Password string `json:"password,omitempty" required:"false"`
	// Groups is read off the wire and ignored: membership moves through Groups.
	Groups []GroupRef `json:"groups,omitempty" required:"false"`
}

// GroupDraft is what a POST or PUT says about a group.
type GroupDraft struct {
	Schemas     []string    `json:"schemas,omitempty" required:"false"`
	ExternalID  string      `json:"externalId,omitempty" required:"false"`
	DisplayName string      `json:"displayName,omitempty" required:"false"`
	Members     []MemberRef `json:"members,omitempty" required:"false"`
	// Role is the one attribute of this document that this tenant owns and its
	// identity provider does not, so it is a field refused rather than a field
	// read — the same standing as UserDraft.Password, for the same reason. The
	// core Group schema has no place to hold a role, and a credential that could
	// write one could hand itself authority: which is exactly why a machine may be
	// given the door at all. MapGroup is the only writer of the binding, at the
	// tenant's own address, under user:manage.
	Role string `json:"role,omitempty" required:"false"`
}

// Operation is one PATCH operation: add, remove or replace, at a path, with a
// value whose meaning is the path's. It stays JSON here because the protocol's
// PATCH body is JSON in a shape no Go type describes honestly.
type Operation struct {
	Op    string          `json:"op,omitempty" required:"false"`
	Path  string          `json:"path,omitempty" required:"false"`
	Value json.RawMessage `json:"value,omitempty" required:"false"`
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
// Concurrency: this service issues no resource version it can compare a write
// against, so it accepts no conditional write: the protocol door refuses a request
// that carries If-Match before it reaches any of the commands below (see
// Service.ProviderConfig, which says `supportETag: false`, and the note at
// Meta.Version, which is an answer and not a demand). What a command does check is
// the state it is about to change. Every write it performs is a
// command on modules/user that rechecks its own state inside its own transaction
// (SetRoles locks the row, ConfirmAddress refuses a status it cannot activate),
// and every write of a group takes that group's row lock before it reads who is
// in it: checking that a collection exists and then writing its members is two
// statements, and between them a DELETE can commit, so without the lock a second
// connector's membership would arrive behind a group that no longer exists — an
// orphan no read of either side can explain. A writer that arrives behind a
// deletion therefore waits for it, then finds no group to write to.
//
// The group lock is not enough for what a membership write leaves behind. Two
// collections, two writers, one person: each holds a different group's lock, so
// neither waits for the other, and the roles each recomputes are read off the
// person's own row. So every person a membership write touches is locked,
// ascending by id, and what their roles become is computed *after* the lock is
// held and not before it — otherwise the second writer restores a role the first
// one had just taken away, which is a revoked grant that outlives the revocation.
//
// Authority: no group command writes the tenant's role mapping. The protocol
// carries no such field (see GroupDraft.Role), and the binding is written by
// MapGroup alone, which rechecks the caller's authority inside its transaction
// before it writes: the guard on the door is not the whole rule, because MapGroup
// is exported and any module may call it. The recheck asks Authority, which the
// composition answers from the two tables that decide it, so no caller is refused
// by a list this module keeps and none is admitted by the absence of one.
type Service interface {
	// ProviderConfig, ResourceTypes, ResourceType, Schemas and Schema are the
	// discovery documents. They take no transaction because they read no rows:
	// what this service supports is a fact about its code. The two that name many
	// documents answer inside the ListResponse envelope RFC 7644 §4 gives every
	// collection of this protocol, because a client that decodes them as one — the
	// only shape a SCIM list has — has to be able to read them at all.
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
	// CreateGroup declares a collection. A draft that names a role is refused:
	// the mapping is this tenant's to choose, and MapGroup is the only command
	// that writes it.
	CreateGroup(ctx context.Context, tx db.Tx[db.Tenant], in GroupDraft) (*GroupResource, error)
	ReplaceGroup(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in GroupDraft) (*GroupResource, error)
	PatchGroup(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, ops []Operation) (*GroupResource, error)
	DeleteGroup(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error

	// GroupMappings is the tenant's mapping table, and MapGroup writes one row
	// of it. Mapping is the workspace's door (user:read, user:manage) rather than
	// the protocol's: it is a role assignment decided in advance by an
	// administrator at a screen, not an act of provisioning. MapGroup rechecks
	// the caller's authority itself, through Authority, so no caller reaches this
	// table by naming the command instead of the door — not a credential carrying
	// its own narrower scope, and not a session whose roles do not grant it.
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

// Authority answers the one authority question this module cannot answer from its
// own rows: whether the caller of this transaction holds a permission.
//
// It is a port for the same reason RoleDirectory is, and the split is the same
// one. Which roles a person holds is modules/user's column and what those roles
// grant is the auth module's table; a provisioning module that joined them would
// own a rule about authority that two other modules spent their rows deciding —
// and the rule would be wrong the day either of them changed it. So the module
// asks the question, the composition answers it over both tables, and the answer
// is never weaker than the one the same composition's HTTP guard gives the same
// caller: the route a request already passed is not a second, weaker rule about
// who may bind a mapping, and the command reached by naming it instead of the
// route must not be either.
//
// The caller is read from ctx, in the tenant of tx, and out of the person's row
// rather than out of what the context claims: a caller the door built carries the
// roles its session was recognised with, and one built by naming the command
// carries whatever its author wrote. Only one of those is a fact about a grant.
type Authority interface {
	// Holds reports whether the caller on ctx holds permission in this tenant.
	// An error is a decision that could not be made, and the write is refused:
	// "I cannot tell" must not read as "yes", which is the same rule HasRole
	// states from the other side of the same table.
	Holds(ctx context.Context, tx db.Tx[db.Tenant], permission string) (bool, error)
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

// MappingList is the tenant's own bindings, read at the workspace's door. It is
// an array body rather than a ListResponse because it is this platform's list and
// not the protocol's: no SCIM client reads it, and the door that does is guarded
// by the user module's permissions. An array body has to name its own media type
// like any other.
type MappingList []*GroupMapping

// ResourceTypeList and SchemaList are the protocol's two discovery collections,
// and they are the ListResponse envelope rather than a bare array because
// RFC 7644 §4 answers a GET that returns many documents with one document — the
// same envelope a page of people and a page of groups use, for the same reason: a
// client decodes a list of this protocol as a ListResponse, and the two addresses
// a client uses to find out what the service serves are the last two it should
// fail to decode.
//
// The two are separate types rather than one generic envelope because huma builds
// an OpenAPI schema out of each, and a client reads that document before it reads
// either body.
type (
	ResourceTypeList struct {
		Schemas      []string        `json:"schemas"`
		TotalResults int             `json:"totalResults"`
		StartIndex   int             `json:"startIndex"`
		ItemsPerPage int             `json:"itemsPerPage"`
		Resources    []*ResourceType `json:"Resources,omitempty"`
	}
	SchemaList struct {
		Schemas      []string  `json:"schemas"`
		TotalResults int       `json:"totalResults"`
		StartIndex   int       `json:"startIndex"`
		ItemsPerPage int       `json:"itemsPerPage"`
		Resources    []*Schema `json:"Resources,omitempty"`
	}
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
