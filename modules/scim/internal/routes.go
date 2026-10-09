package internal

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// The protocol's door. Every handler here is a translation: the document in,
// the arguments of a contracts.Service call out, and the answer back into the
// protocol's envelope. No rule lives at this layer — a rule decided here could
// not be shared with the conformance fake, and the service is where the module's
// decisions are made, against the transaction.
//
// The declared address is the module's own line to write (s.App.At), and the
// permission is the one the tenant grants to the credential its identity
// provider holds. Nothing is mounted without one: the workspace surface denies
// by default, and a machine door that inherited that default would answer nobody.

// RegisterRoutes mounts the nineteen operations of SCIM 2.0 that this service
// serves, plus the tenant's own mapping door.
func RegisterRoutes(s httpx.Surfaces, svc contracts.Service) {
	// The protocol's address, declared once, and every operation mounted
	// relative to it.
	protocol := s.App.At("/scim/v2")

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-service-provider-config", Method: http.MethodGet, Path: "/ServiceProviderConfig",
		Summary: "What this service supports", Tags: []string{"scim"},
		Description: "The service's own account of what it implements. Every yes is a route that exists and every no is one that does not.",
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, _ *struct{}) (*document[contracts.ProviderConfig], error) {
		cfg, err := svc.ProviderConfig(ctx)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*cfg), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-resource-types-list", Method: http.MethodGet, Path: "/ResourceTypes",
		Summary: "The resource types this service serves", Tags: []string{"scim"},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, _ *struct{}) (*document[contracts.ResourceTypeList], error) {
		types, err := svc.ResourceTypes(ctx)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(types), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-resource-type-get", Method: http.MethodGet, Path: "/ResourceTypes/{name}",
		Summary: "One resource type", Tags: []string{"scim"},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *nameInput) (*document[contracts.ResourceType], error) {
		rt, err := svc.ResourceType(ctx, in.Name)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*rt), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-schemas-list", Method: http.MethodGet, Path: "/Schemas",
		Summary: "The attribute documents", Tags: []string{"scim"},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, _ *struct{}) (*document[contracts.SchemaList], error) {
		list, err := svc.Schemas(ctx)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(list), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-schema-get", Method: http.MethodGet, Path: "/Schemas/{id}",
		Summary: "One attribute document", Tags: []string{"scim"},
		Description: "The identifier is the urn the document answers with, sent as one path segment: a client reads it out of /Schemas and comes straight back with it.",
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *schemaInput) (*document[contracts.Schema], error) {
		schema, err := svc.Schema(ctx, in.ID)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*schema), nil
	})

	// The people.
	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-users-list", Method: http.MethodGet, Path: "/Users",
		Summary: "List the tenant's people", Tags: []string{"scim"},
		Description: "Paginated. The one filter this service honours is userName eq \"<address>\"; any other filter is refused rather than dropped, because a connector handed the whole directory when it asked for a subset would act on everybody it meant to leave alone.",
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *listInput) (*document[contracts.UserList], error) {
		tx, done, err := working(ctx)
		if err != nil {
			return nil, err
		}
		list, err := svc.ListUsers(ctx, tx, in.query())
		if err != nil {
			return nil, rest.Fault(err)
		}
		_ = done
		return answered(*list), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-user-create", Method: http.MethodPost, Path: "/Users",
		Summary: "Provision a person", Tags: []string{"scim"}, DefaultStatus: http.StatusCreated,
		Description: "active false (or absent) invites the person; active true says the identity provider vouches for the address and activates them in the same transaction. Neither sets a password: the person signs in through the tenant's own door.",
		Errors:      []int{http.StatusConflict, http.StatusPreconditionFailed, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *userDraftInput) (*document[contracts.UserResource], error) {
		if err := unconditional(in.IfMatch); err != nil {
			return nil, err
		}
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		u, err := svc.CreateUser(ctx, tx, in.Body)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*u), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-user-get", Method: http.MethodGet, Path: "/Users/{id}",
		Summary: "One person", Tags: []string{"scim"},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *idInput) (*document[contracts.UserResource], error) {
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		u, err := svc.GetUser(ctx, tx, in.ID)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*u), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-user-replace", Method: http.MethodPut, Path: "/Users/{id}",
		Summary: "Replace a person's record", Tags: []string{"scim"},
		Description: "A field the document leaves empty is a field the caller is not naming, not a field being cleared — and it is never a reason to deactivate somebody.",
		Errors:      []int{http.StatusNotFound, http.StatusPreconditionFailed, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *userReplaceInput) (*document[contracts.UserResource], error) {
		if err := unconditional(in.IfMatch); err != nil {
			return nil, err
		}
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		u, err := svc.ReplaceUser(ctx, tx, in.ID, in.Body)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*u), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-user-patch", Method: http.MethodPatch, Path: "/Users/{id}",
		Summary: "Change one part of a person's record", Tags: []string{"scim"},
		Description: "active, externalId and the name. An operation this service cannot apply fails the whole request: a partial application would leave the connector's idea of the person and this tenant's two different things.",
		Errors:      []int{http.StatusNotFound, http.StatusPreconditionFailed, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *patchInput) (*document[contracts.UserResource], error) {
		if err := unconditional(in.IfMatch); err != nil {
			return nil, err
		}
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		u, err := svc.PatchUser(ctx, tx, in.ID, in.Body.Operations)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*u), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-user-delete", Method: http.MethodDelete, Path: "/Users/{id}",
		Summary: "Stop a person signing in", Tags: []string{"scim"}, DefaultStatus: http.StatusNoContent,
		Description: "Deactivation, not deletion: the person's row keeps its address reserved so the same person is never provisioned twice, and their history stays where it is.",
		Errors:      []int{http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed, http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{usercontracts.EventDeactivated}},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *deleteInput) (*empty, error) {
		if err := unconditional(in.IfMatch); err != nil {
			return nil, err
		}
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		if err := svc.DeactivateUser(ctx, tx, in.ID); err != nil {
			return nil, rest.Fault(err)
		}
		return &empty{}, nil
	})

	// The collections, and the role each brings.
	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-groups-list", Method: http.MethodGet, Path: "/Groups",
		Summary: "List the tenant's groups", Tags: []string{"scim"},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *listInput) (*document[contracts.GroupList], error) {
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		list, err := svc.ListGroups(ctx, tx, in.query())
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*list), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-group-create", Method: http.MethodPost, Path: "/Groups",
		Summary: "Declare a group", Tags: []string{"scim"}, DefaultStatus: http.StatusCreated,
		Description: "The collection and who is in it. A document that names a role is refused: the role a group brings is this tenant's to bind, at its own door, and a credential that could write it could grant itself anything.",
		Errors:      []int{http.StatusConflict, http.StatusPreconditionFailed, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *groupDraftInput) (*document[contracts.GroupResource], error) {
		if err := unconditional(in.IfMatch); err != nil {
			return nil, err
		}
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		g, err := svc.CreateGroup(ctx, tx, in.Body)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*g), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-group-get", Method: http.MethodGet, Path: "/Groups/{id}",
		Summary: "One group", Tags: []string{"scim"},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *idInput) (*document[contracts.GroupResource], error) {
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		g, err := svc.GetGroup(ctx, tx, in.ID)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*g), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-group-replace", Method: http.MethodPut, Path: "/Groups/{id}",
		Summary: "Replace a group", Tags: []string{"scim"},
		Description: "The name has to stay this group's: it keys the role mapping somebody chose for it, and a document that says nothing about the role leaves that mapping alone.",
		Errors:      []int{http.StatusNotFound, http.StatusPreconditionFailed, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *groupReplaceInput) (*document[contracts.GroupResource], error) {
		if err := unconditional(in.IfMatch); err != nil {
			return nil, err
		}
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		g, err := svc.ReplaceGroup(ctx, tx, in.ID, in.Body)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*g), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-group-patch", Method: http.MethodPatch, Path: "/Groups/{id}",
		Summary: "Move membership", Tags: []string{"scim"},
		Description: "add, remove or replace on members — whole, or one person selected by members[value eq \"<id>\"]. This is the only door that moves a membership, and it writes no role: the tenant binds one at its own group-mappings door.",
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *patchInput) (*document[contracts.GroupResource], error) {
		if err := unconditional(in.IfMatch); err != nil {
			return nil, err
		}
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		g, err := svc.PatchGroup(ctx, tx, in.ID, in.Body.Operations)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*g), nil
	})

	httpx.Register(protocol, huma.Operation{
		OperationID: "scim-group-delete", Method: http.MethodDelete, Path: "/Groups/{id}",
		Summary: "Remove a group", Tags: []string{"scim"}, DefaultStatus: http.StatusNoContent,
		Description: "Refused while the group has members: a connector that deleted a group out from under people would take their role with it and say nothing.",
		Errors:      []int{http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed, http.StatusServiceUnavailable},
	}, httpx.Permission(contracts.PermissionProvision), func(ctx context.Context, in *deleteInput) (*empty, error) {
		if err := unconditional(in.IfMatch); err != nil {
			return nil, err
		}
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		if err := svc.DeleteGroup(ctx, tx, in.ID); err != nil {
			return nil, rest.Fault(err)
		}
		return &empty{}, nil
	})

	// The tenant's own door: which role a group brings. Guarded by the user
	// module's existing permissions, because binding a group name to a role is a
	// role assignment written in advance — which is exactly what user:manage
	// already means — and minting scim:map would put two names on one act.
	httpx.Register(s.App, huma.Operation{
		OperationID: "scim-group-mappings-list", Method: http.MethodGet, Path: "/group-mappings",
		Summary: "Which role each group brings", Tags: []string{"scim"},
	}, httpx.Permission(usercontracts.PermissionUserRead), func(ctx context.Context, _ *struct{}) (*document[contracts.MappingList], error) {
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		list, err := svc.GroupMappings(ctx, tx)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(list), nil
	})

	httpx.Register(s.App, huma.Operation{
		OperationID: "scim-group-mapping-write", Method: http.MethodPut, Path: "/group-mappings",
		Summary: "Bind a group to a role, or clear the binding", Tags: []string{"scim"},
		Description: "An empty role clears the binding; the group stays, because a connector still mirrors it. The role has to be one this tenant knows.",
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, httpx.Permission(usercontracts.PermissionUserManage), func(ctx context.Context, in *mappingInput) (*document[contracts.GroupMapping], error) {
		tx, _, err := working(ctx)
		if err != nil {
			return nil, err
		}
		m, err := svc.MapGroup(ctx, tx, in.Body.Group, in.Body.Role)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return answered(*m), nil
	})
}

// document is the protocol's envelope for a body that names its own media type.
// The response's Content-Type is whatever the body answers to ContentType, which
// is huma's mechanism and not a header written by hand — so a route cannot
// forget it, and a refusal keeps the kernel's own shape.
type document[T any] struct {
	Body T
}

func answered[T any](body T) *document[T] { return &document[T]{Body: body} }

// empty is a 204 with no body, which is what the protocol answers a DELETE with
// and what DefaultStatus above says the operation returns.
type empty struct{}

// unconditional refuses a conditional write, before it becomes an unconditional
// one.
//
// This service issues no ETag and holds no revision to compare one to — the
// discovery document says `supportETag: false`, and `meta.version` is a timestamp a
// client may read, not a token this service would honour back. RFC 7644 §3.14 makes
// versioning optional, and RFC 9110 §13.1.1 is not: an origin server that cannot
// evaluate a precondition has to say so rather than perform the write as though the
// client had sent none.
//
// Ignoring it is the failure this refuses, and the failure is not cosmetic. A
// connector that sent the version it had read was asking "do not overwrite anything
// newer than this"; a door that answered 200 anyway was overwriting exactly the
// update the request was written to avoid, reporting success, publishing an event
// for the write that should never have happened, and — for the same header on a
// DELETE — deactivating a person the connector's own copy of the record no longer
// speaks for. Refusing is the answer that costs a retry; performing is the answer
// that costs the row.
//
// Every write of this door is refused for the header, the two creates included. One
// rule is what makes the refusal dependable: a connector that sent a condition
// anywhere is holding a version it believes the door compares, and an answer of 201
// at the address that quietly dropped the header would keep that belief alive. A
// create is where such a connector sends one — nothing exists yet to match — so it is
// refused with the rest rather than treated as the exception a reader has to remember.
//
// What is *not* given the header is the reads, and the tenant's own `group-mappings`
// route. A read that carries a condition changes nothing, and this refusal exists
// because a condition must never become a mutation: there is no lost update to prevent
// at a GET, and refusing a directory read for a header the caller could not have
// obtained from this service would cost a client its page for nothing. The workspace
// route is not the protocol's door at all — no SCIM document reaches it and no
// connector sends a condition to an address this platform named — so a header no
// caller of that route can be holding would be a field nothing reads.
//
// The refusal is 412 and not 501 because the caller's precondition genuinely was not
// met — this service holds no matching revision for any If-Match, which is what
// RFC 7644 answers an unmatched ETag with — and because 501 would say the door is
// missing when the door is here and answered.
func unconditional(ifMatch string) error {
	if strings.TrimSpace(ifMatch) == "" {
		return nil
	}
	return problem.New(http.StatusPreconditionFailed,
		"this service issues no resource version, so it cannot evaluate an If-Match and will not write as though it had; "+
			"send the write with no condition, or compare meta.version yourself before sending it")
}

// working takes the request's own transaction. Its absence is not a caller's
// error: it means the request reached a handler with no database, which is an
// outage and is answered as one.
func working(ctx context.Context) (db.Tx[db.Tenant], func(), error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return noTx, nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	return tx, func() {}, nil
}

// noTx is the zero transaction, which is what an outage answer carries: there is
// no transaction to hand a handler that will not be called.
var noTx db.Tx[db.Tenant]

// The inputs. A SCIM body is the protocol's shape and this module's vocabulary:
// the drafts in contracts/ are what the service reads, and huma builds them from
// the request.
type (
	nameInput struct {
		Name string `path:"name" doc:"The resource type this service serves"`
	}

	// schemaInput is the attribute document's own address: the urn a client read
	// out of /Schemas arrives as one path segment, so the parameter is an id and
	// not a name. A route that declares {id} while its input reads {name} is
	// refused by the router before the service is ever asked — which is how every
	// schema this service published came to answer 422 at the address advertising
	// it, while /Schemas itself listed the urns.
	schemaInput struct {
		ID string `path:"id" doc:"The urn of the attribute document, as /Schemas names it"`
	}

	idInput struct {
		ID uuid.UUID `path:"id" format:"uuid"`
	}

	listInput struct {
		Filter     string `query:"filter"`
		StartIndex int    `query:"startIndex"`
		Count      int    `query:"count"`
	}

	userDraftInput struct {
		Body    contracts.UserDraft
		IfMatch string `header:"If-Match" required:"false" doc:"Refused: this service issues no resource version to match, and will not perform a conditional write as an unconditional one"`
	}

	userReplaceInput struct {
		ID      uuid.UUID `path:"id" format:"uuid"`
		Body    contracts.UserDraft
		IfMatch string `header:"If-Match" required:"false" doc:"Refused: this service issues no resource version to match, and will not perform a conditional write as an unconditional one"`
	}

	groupDraftInput struct {
		Body    contracts.GroupDraft
		IfMatch string `header:"If-Match" required:"false" doc:"Refused: this service issues no resource version to match, and will not perform a conditional write as an unconditional one"`
	}

	groupReplaceInput struct {
		ID      uuid.UUID `path:"id" format:"uuid"`
		Body    contracts.GroupDraft
		IfMatch string `header:"If-Match" required:"false" doc:"Refused: this service issues no resource version to match, and will not perform a conditional write as an unconditional one"`
	}

	patchInput struct {
		ID      uuid.UUID `path:"id" format:"uuid"`
		IfMatch string    `header:"If-Match" required:"false" doc:"Refused: this service issues no resource version to match, and will not perform a conditional write as an unconditional one"`
		Body    struct {
			Schemas    []string              `json:"schemas"`
			Operations []contracts.Operation `json:"Operations"`
		}
	}

	// deleteInput is DELETE at either address. It is its own input rather than
	// idInput because the condition belongs to the write and not to the address: a
	// GET that carried an If-Match is a client caching, and it is answered normally
	// out of the same table that refuses the DELETE beside it.
	deleteInput struct {
		ID      uuid.UUID `path:"id" format:"uuid"`
		IfMatch string    `header:"If-Match" required:"false" doc:"Refused: this service issues no resource version to match, and will not perform a conditional write as an unconditional one"`
	}

	mappingInput struct {
		Body struct {
			Group string `json:"group"`
			Role  string `json:"role"`
		}
	}
)

func (in *listInput) query() contracts.ListQuery {
	return contracts.ListQuery{Filter: in.Filter, StartIndex: in.StartIndex, Count: in.Count}
}
