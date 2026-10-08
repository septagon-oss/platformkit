-- SCIM provisioning: the two things this module owns and nothing else.
--
-- There is no table of people here. A person is modules/user's row, and a
-- second copy of it under this module's name would be a second answer to
-- "who is in this tenant" — the question that has exactly one owner in this
-- composition. What the protocol needs beyond that row is the two facts the
-- identity provider brings and no other door in this platform asks: which
-- collection it says a person belongs to, and which of its own identifiers it
-- calls them by.
--
-- The entity is modules/scim/contracts/scim.go.

-- A group is the provider's collection, named as the provider names it. The
-- name is the key a connector sends, so it is compared without case and stored
-- once, and it never moves (see the module's note on immutability).
CREATE TABLE scim_groups (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id     uuid NOT NULL,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now(),
	name          text NOT NULL,
	external_id   text NOT NULL DEFAULT '',
	-- The entity's own columns, not this module's ideas: crud.Entity is the
	-- contract every table this module writes through kit/crud is held to, and a
	-- column the adapter writes is a column the table has to have.
	deleted_at    timestamptz
);

CREATE UNIQUE INDEX scim_groups_tenant_name ON scim_groups (tenant_id, lower(name));

-- Who is in which group. Both halves are this tenant's rows: the group is
-- above, and the user is modules/user's row, reached by id and never copied.
CREATE TABLE scim_group_members (
	tenant_id  uuid NOT NULL,
	group_id   uuid NOT NULL,
	user_id    uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (group_id, user_id)
);

-- A person's groups is the read every user document does, and it is the join
-- the group's own membership list is not.
CREATE INDEX scim_group_members_tenant_user ON scim_group_members (tenant_id, user_id);

-- The tenant's mapping from a group to a role. It is a row rather than a
-- column of scim_groups because a group whose name nobody has mapped yet is a
-- group a connector can mirror today: the mapping arrives later, at the
-- workspace door, and deleting it must not touch the collection.
--
-- The role is a name. What a name grants is the auth module's table, and role
-- assignments are modules/user's column — this row decides what a membership
-- will mean, and the write that spends it is SetRoles.
CREATE TABLE scim_group_mappings (
	id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id  uuid NOT NULL,
	group_id   uuid NOT NULL,
	role       text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz
);

-- The identifier the provider calls a person by. Keyed by the person, because
-- a connector that sends a different externalId for the same person is
-- describing the same person twice, and the answer is the row, not a new one.
CREATE TABLE scim_user_external (
	tenant_id   uuid NOT NULL,
	user_id     uuid NOT NULL PRIMARY KEY,
	external_id text NOT NULL DEFAULT ''
);

-- One provider identifier per tenant, when it has one: two people with the
-- same id from the provider's side is a connector's bug this table refuses
-- rather than two identities nobody can tell apart afterwards.
CREATE UNIQUE INDEX scim_user_external_tenant_id ON scim_user_external (tenant_id, external_id)
	WHERE external_id <> '';

-- One binding per group: the role a collection brings is one answer, and two rows
-- would be two answers to which one a membership granted.
CREATE UNIQUE INDEX scim_group_mappings_group ON scim_group_mappings (group_id);

ALTER TABLE scim_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE scim_groups FORCE ROW LEVEL SECURITY;
CREATE POLICY scim_groups_tenant ON scim_groups
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

ALTER TABLE scim_group_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE scim_group_members FORCE ROW LEVEL SECURITY;
CREATE POLICY scim_group_members_tenant ON scim_group_members
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

ALTER TABLE scim_group_mappings ENABLE ROW LEVEL SECURITY;
ALTER TABLE scim_group_mappings FORCE ROW LEVEL SECURITY;
CREATE POLICY scim_group_mappings_tenant ON scim_group_mappings
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

ALTER TABLE scim_user_external ENABLE ROW LEVEL SECURITY;
ALTER TABLE scim_user_external FORCE ROW LEVEL SECURITY;
CREATE POLICY scim_user_external_tenant ON scim_user_external
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
