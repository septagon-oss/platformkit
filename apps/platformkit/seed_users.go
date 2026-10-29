package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/seed"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// userSeeder seeds people: an invitation through the user module, the roles the
// file names, and — when the deployment names a demo password — the password the
// demo walkthrough signs in with. Roles and password are the module's own
// commands, SetRoles and SetPassword, so a seeded person holds what a person in
// the admin screens would hold and the trail says which command gave it.
//
// A person is created and never renamed: an invitation is the write the user
// module offers a seed, and a name a person chose for themselves is theirs from
// then on. Passwords are never in a seed file — the loader refuses a field named
// for one — and never in a plan line, which prints keys and addresses only.
type userSeeder struct {
	users usercontracts.SeedWriter
	// demoPassword is config's Demo.Password: the one password a demonstration's
	// people sign in with. It is the deployment's answer to a record that asks for
	// a credential, and an empty one asks the run to mint one per person and hand
	// it to the caller that ran it — see commands and mintedCredential. The loader
	// refuses any seed field named for a password, so this value has exactly one
	// door into a seed run.
	demoPassword string
}

// signInDeclared is the one word a demo record and the person it names agree on.
// In the file it is the ask — this person is one the demonstration signs in as.
// In the snapshot it is the answered fact — that person holds a sign-in
// credential. The same word on both sides is what makes a rerun of a person who
// has one read as unchanged, and it names neither a secret nor a hash.
const signInDeclared = "demo"

func (userSeeder) Resource() seed.Resource {
	return seed.Resource{
		Alias: "users", Module: "user", Entity: "user",
		NaturalKey: "email", WriteGrant: usercontracts.PermissionUserManage,
		// An address is the record's key and the module owns its spelling, so a
		// file that writes `Person@Example.test` declares the person the module
		// stores as `person@example.test` — one record, one mapping, one run.
		CanonicalKey: usercontracts.CanonicalEmail,
		// No delete path, deliberately: deactivating a person is not undoing an
		// invitation, and a file that stops declaring an address must not be able
		// to switch a person off.
		Prunable: false,
	}
}

func (w userSeeder) Target(_ context.Context, r seed.Record, _ map[string]uuid.UUID, _ time.Time) (seed.Target, error) {
	text, err := seedTexts(r, "displayName", "signIn")
	if err != nil {
		return seed.Target{}, err
	}
	fields := map[string]any{"email": usercontracts.CanonicalEmail(r.Key), "displayName": text["displayName"]}
	roles, err := seedRoles_(r.Fields["roles"])
	if err != nil {
		return seed.Target{}, err
	}
	fields["roles"] = roles
	// The ask comes from the file, and not from whether this deployment happens to
	// name a demo password: a record that asks for a sign-in asks whether or not
	// the answer has arrived yet, which is what makes a deployment that sets
	// PLATFORMKIT_DEMO_PASSWORD after provisioning a tenant still able to give its
	// demo people a credential. See signInHeld for the other side.
	if text["signIn"] == signInDeclared {
		fields["signIn"] = signInDeclared
	}
	return seed.Target{Fields: fields}, nil
}

// signInHeld is the snapshot's whole answer about sign-in: does this person hold
// a credential at all. It reads only whether the module has a hash, and never
// the hash, a comparison against one, or anything that leaves this function
// other than one word — the same shape the plan prints and no more.
//
// "Any credential", and not "the demo credential", is the point. A person who
// chose their own password holds a credential, so the record reads as settled
// and the run that comes round again writes no password at all: docs/seed.md
// promises that a rerun never resets a person's password, and the only way to
// keep that promise without a second record of who set what is to ask the row
// whether anybody has.
func signInHeld(row *usercontracts.User) string {
	if row.PasswordHash != "" {
		return signInDeclared
	}
	return ""
}

func (w userSeeder) Read(ctx context.Context, tx db.Tx[db.Tenant], key seed.Key, _ bool) (seed.Snapshot, error) {
	var (
		row *usercontracts.User
		err error
	)
	if key.RecordID != uuid.Nil {
		row, err = w.users.Get(ctx, tx, key.RecordID)
	} else {
		row, err = w.users.ByEmail(ctx, tx, key.Value)
	}
	if errors.Is(err, crud.ErrNotFound) {
		return seed.Snapshot{}, nil
	}
	if err != nil {
		return seed.Snapshot{}, err
	}
	fields := map[string]any{
		"email": row.Email, "displayName": row.DisplayName,
		// The stored answer, sorted the same way the declared one is, so a file
		// that lists roles in another order is the same declaration.
		"roles":  seedRolesSorted(row.Roles),
		"signIn": signInHeld(row),
	}
	return seed.Snapshot{Present: true, ID: row.ID, Fields: fields}, nil
}

func (w userSeeder) Create(ctx context.Context, tx db.Tx[db.Tenant], t seed.Target) (seed.Snapshot, error) {
	row, err := w.users.Invite(ctx, tx, t.Fields["email"].(string), t.Fields["displayName"].(string))
	if err != nil {
		return seed.Snapshot{}, err
	}
	return w.commands(ctx, tx, row, t)
}

// Update touches the two facts a seed owns about a person — the roles the file
// names, and the demo sign-in the deployment offers — and refuses the rename it
// was asked not to do. A person's name is theirs; a demo file that changed one
// fails here, loudly, rather than overwriting whoever that address belongs to.
func (w userSeeder) Update(ctx context.Context, tx db.Tx[db.Tenant], cur seed.Snapshot, t seed.Target) (seed.Snapshot, error) {
	if want, have := t.Fields["displayName"].(string), cur.Fields["displayName"].(string); want != have {
		return seed.Snapshot{}, fmt.Errorf(
			"user owns a person after the invitation: this seed changes no name (had %q, the file asks %q)", have, want)
	}
	row, err := w.users.Get(ctx, tx, cur.ID)
	if err != nil {
		return seed.Snapshot{}, err
	}
	return w.commands(ctx, tx, row, t)
}

// commands runs the module's own role and password commands for one record.
// Both are idempotent in the way the seed needs: SetRoles with the same set
// writes and publishes nothing, and a SetPassword is only reached when the
// record's sign-in fact differs, so a second run of the same files writes no
// password at all.
func (w userSeeder) commands(ctx context.Context, tx db.Tx[db.Tenant], row *usercontracts.User, t seed.Target) (seed.Snapshot, error) {
	roles, err := seedRoles_(t.Fields["roles"])
	if err != nil {
		return seed.Snapshot{}, err
	}
	// The set the file declares, whenever it differs from the set the person
	// holds — including when what it declares is nothing. A file that takes a
	// role back says `roles: []`, and a writer that skipped an empty list would
	// keep the grant the declaration removed, forever, and call it unchanged.
	// Taking the last administrator away is the user module's own refusal, and
	// this run commits nothing when it says so.
	if !slices.Equal([]string(row.Roles), roles) {
		if row, err = w.users.SetRoles(ctx, tx, row.ID, roles); err != nil {
			return seed.Snapshot{}, err
		}
	}
	// The credential a seed gives is the one a person does not have yet. Once a
	// row holds a hash the answer is settled — see signInHeld — so the deployment
	// password reaches an invited person and never overwrites a chosen one.
	if signIn, asked := t.Fields["signIn"].(string); asked && signIn == signInDeclared && row.PasswordHash == "" {
		password := w.demoPassword
		if password == "" {
			// No deployment answer means this demonstration names its people but
			// not their credential, and an invited person who cannot be signed in
			// as is not a walkthrough. So the run mints one for this person and
			// prints it once, the way bootstrap does for the first administrator.
			if password, err = generatePassword(); err != nil {
				return seed.Snapshot{}, err
			}
			// The minted secret goes to whoever asked for this run and not to the
			// process's output — see mintedCredential. A run nothing is collecting
			// for keeps only the hash, which is the honest answer for a create
			// transaction with no caller on the other side to receive it.
			mintInto(ctx, row.Email, password)
		}
		if err := w.users.SetPassword(ctx, tx, row.ID, password); err != nil {
			return seed.Snapshot{}, err
		}
		if row, err = w.users.Get(ctx, tx, row.ID); err != nil {
			return seed.Snapshot{}, err
		}
	}
	fields := map[string]any{
		"email": row.Email, "displayName": row.DisplayName,
		"roles":  seedRolesSorted(row.Roles),
		"signIn": signInHeld(row),
	}
	return seed.Snapshot{Present: true, ID: row.ID, Fields: fields}, nil
}

func (w userSeeder) Delete(context.Context, db.Tx[db.Tenant], seed.Snapshot) error {
	return errors.New("user: deactivating a person is not undoing an invitation")
}
