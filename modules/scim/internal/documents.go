package internal

import (
	"context"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

// The discovery documents. They read no rows and take no transaction, and that
// is the honest answer to what they are: what this service supports is a fact
// about its code, and a document that said otherwise — a configurable
// patch.supported, say — would be a promise the router does not keep.
//
// The attribute descriptions are written out rather than derived from the Go
// types because a client's error messages read them: which attributes are
// readOnly, and why, is the sentence a connector's operator sees when it tries
// to write one.

// providerConfig is the service's own account of itself. Every yes is a route
// that exists, and every no is one that does not: bulk is false and no /Bulk is
// mounted, so a client that reads the document and a client that probes the
// address get the same answer.
func (s *Service) ProviderConfig(_ context.Context) (*contracts.ProviderConfig, error) {
	return &contracts.ProviderConfig{
		Schemas:        []string{contracts.SchemaUser, contracts.SchemaGroup},
		SupportingETag: false,
		Filter: contracts.ConfigFeature{
			Supported: true,
		},
		ChangePassword: contracts.ConfigFeature{Supported: false},
		Sort:           contracts.ConfigFeature{Supported: false},
		Bulk:           contracts.ConfigFeature{Supported: false},
		Patch:          contracts.ConfigFeature{Supported: true},
		AuthenticationSchemes: []contracts.AuthScheme{{
			Type: "bearerToken", Name: "OAuth 2.0 bearer token",
			Description: "A scoped credential the tenant issues; its scopes are its authority.",
			SpecURI:     "https://datatracker.ietf.org/doc/html/rfc6750", Primary: true,
		}},
		Meta: contracts.Meta{ResourceType: "ServiceProviderConfig", Location: "/ServiceProviderConfig"},
	}, nil
}

// ResourceTypes names the two types and the addresses they answer at. The
// endpoints are the protocol's own tails; the kernel composes the prefix, so the
// module that does not know its own prefix cannot state a wrong one.
func (s *Service) ResourceTypes(_ context.Context) (contracts.ResourceTypeList, error) {
	return contracts.ResourceTypeList{
		{
			Schemas: []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
			ID:      contracts.ResourceTypeUser, Name: contracts.ResourceTypeUser,
			Endpoint: "/Users", Schema: contracts.SchemaUser,
			Description: "A person of this tenant, as their identity provider describes them.",
		},
		{
			Schemas: []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
			ID:      contracts.ResourceTypeGroup, Name: contracts.ResourceTypeGroup,
			Endpoint: "/Groups", Schema: contracts.SchemaGroup,
			Description: "A collection the identity provider declares, and the role it brings.",
		},
	}, nil
}

// ResourceType is one of the two, or ErrNotFound for a name this service does
// not serve — which is what a client that asks for a third type deserves.
func (s *Service) ResourceType(_ context.Context, name string) (*contracts.ResourceType, error) {
	all, _ := s.ResourceTypes(context.Background())
	for _, rt := range all {
		if rt.Name == name {
			return rt, nil
		}
	}
	return nil, fmt.Errorf("%w: no resource type %q here", crud.ErrNotFound, name)
}

// Schemas is the attribute documents' index.
func (s *Service) Schemas(_ context.Context) (contracts.SchemaList, error) {
	return contracts.SchemaList{userSchema(), groupSchema()}, nil
}

// Schema is one attribute document.
func (s *Service) Schema(_ context.Context, id string) (*contracts.Schema, error) {
	switch id {
	case contracts.SchemaUser:
		return userSchema(), nil
	case contracts.SchemaGroup:
		return groupSchema(), nil
	}
	return nil, fmt.Errorf("%w: no schema %q here", crud.ErrNotFound, id)
}

func userSchema() *contracts.Schema {
	return &contracts.Schema{
		ID: contracts.SchemaUser, Definition: "User",
		Description: "A person of this tenant. The attributes this platform does not keep are readOnly, with the reason.",
		Attributes: []contracts.SchemaAttribute{
			{Name: "userName", Type: "string", Required: true, CaseExact: false,
				Mutability: "readOnly", Uniqueness: "constant", Returned: "default",
				Description: "The person's address. It is this tenant's login lookup, so no door here moves it after the person exists."},
			{Name: "externalId", Type: "string", Mutability: "readWrite", Returned: "default",
				Description: "The identifier the identity provider calls this person by."},
			{Name: "displayName", Type: "string", Mutability: "readWrite", Returned: "default",
				Description: "What the person is called. This platform keeps one name; the halves below fold into it."},
			{Name: "name", Type: "complex", Mutability: "readWrite", Returned: "default", SubAttributes: []contracts.SchemaAttribute{
				{Name: "formatted", Type: "string", Mutability: "readWrite", Returned: "default"},
				{Name: "givenName", Type: "string", Mutability: "writeOnly", Returned: "never",
					Description: "Read on write and folded into the one name this platform keeps; nothing stores the halves separately."},
				{Name: "familyName", Type: "string", Mutability: "writeOnly", Returned: "never",
					Description: "Read on write and folded into the one name this platform keeps."},
			}},
			{Name: "emails", Type: "complex", MultiValued: true, Mutability: "readOnly", Returned: "default",
				SubAttributes: []contracts.SchemaAttribute{
					{Name: "value", Type: "string", Mutability: "readOnly", Returned: "default"},
					{Name: "type", Type: "string", Mutability: "readOnly", Returned: "default", CanonicalValues: []string{"work"}},
					{Name: "primary", Type: "boolean", Mutability: "readOnly", Returned: "default"},
				},
				Description: "Derived from the one address this platform keeps. On create it is read only when userName is absent and exactly one address is named."},
			{Name: "active", Type: "boolean", Mutability: "readWrite", Returned: "default",
				Description: "Whether the person can sign in. Deleting a person deactivates them; it does not remove them."},
			{Name: "groups", Type: "complex", MultiValued: true, Mutability: "readOnly", Returned: "default",
				SubAttributes: []contracts.SchemaAttribute{
					{Name: "value", Type: "string", Mutability: "readOnly", Returned: "default"},
					{Name: "display", Type: "string", Mutability: "readOnly", Returned: "default"},
					{Name: "type", Type: "string", Mutability: "readOnly", Returned: "default", CanonicalValues: []string{"DIRECT"}},
				},
				Description: "Membership moves through /Groups. Two doors writing one membership is the drift this service refuses."},
			{Name: "password", Type: "string", Mutability: "readOnly", Returned: "never",
				Description: "A password is chosen by the person it belongs to. Invite them and let them set one."},
			{Name: "meta", Type: "complex", Mutability: "readOnly", Returned: "default"},
		},
		Meta: contracts.Meta{ResourceType: "Schema", Location: "/Schemas/" + contracts.SchemaUser},
	}
}

func groupSchema() *contracts.Schema {
	return &contracts.Schema{
		ID: contracts.SchemaGroup, Definition: "Group",
		Description: "A collection the identity provider declares.",
		Attributes: []contracts.SchemaAttribute{
			{Name: "displayName", Type: "string", Required: true, Mutability: "readOnly", Returned: "default",
				Description: "The collection's name, as its provider spells it. It keys the role mapping, so it does not change."},
			{Name: "externalId", Type: "string", Mutability: "readWrite", Returned: "default"},
			{Name: "members", Type: "complex", MultiValued: true, Mutability: "readWrite", Returned: "default", SubAttributes: []contracts.SchemaAttribute{
				{Name: "value", Type: "string", Mutability: "readWrite", Returned: "default"},
				{Name: "display", Type: "string", Mutability: "readOnly", Returned: "default",
					Description: "What the person is called, read from their record rather than stored here."},
			}},
			{Name: "meta", Type: "complex", Mutability: "readOnly", Returned: "default"},
		},
		Meta: contracts.Meta{ResourceType: "Schema", Location: "/Schemas/" + contracts.SchemaGroup},
	}
}
