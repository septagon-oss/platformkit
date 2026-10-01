package main

// This file is the whole of what this application adds to change control: the one
// subject it applies proposals for, the one flag it reads, and the one door that
// flag stands in front of.
//
// modules/change owns the object — the proposal, its digest, its state machine,
// the four-eyes rule — and owns no opinion about which writes need one, because
// that opinion is a fact about an installation. So the three things that are this
// product's own facts are written here, where a reader can see all of them at
// once: site settings are the first subject, `change.control.site-settings` is the
// switch, and the settings form's own door is where the switch bites. A module
// that knew any of the three would be a module with a customer in it.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/flags"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
)

// proposalAddress is where a write the gate refuses has to go instead. It is the
// change module's own collection as the surface composes it — /api/v1/<module> for
// the app surface, kit/httpx/surfaces.go — written out here because the refusal is
// this product's sentence about its own addresses, and modules/change, which owns
// the door, is the one module that must not name it.
const proposalAddress = "/api/v1/change/proposals"

// siteSettingsFlag is the switch. Off, the settings form writes, which is what it
// did before change control existed. On, that door answers 409 and names
// proposalAddress, and the write goes in as a proposal that an account other than
// the proposer applies. The key is this application's: another product that gates
// a different write picks its own key and its own subject.
const siteSettingsFlag = "change.control.site-settings"

// changeModule is wired in compose, beside the module whose settings it gates:
// change.Module over changeSubjects(sites) and site.Module over settingsGate. The
// proposals answer whether or not the flag is on: the switch decides which door a
// settings write comes through, not whether the object exists, and a person who
// wants a second pair of eyes on a change can propose one today in an installation
// with the switch off. All three parts are typed by the two contracts they
// satisfy, so the compiler checks the graph and no reader has to run a generator.

// configFlags is kit/flags' Evaluator over what kit/config read out of the flags
// block: one installation, one process, one boolean per key, no targeting beyond
// the tenant the request already resolved.
//
// It is here rather than in kit/flags/providers because it is not a provider worth
// installing: there is no SDK, no wire format and no service behind it, and a
// package for eight lines of map lookup is a package that invites somebody to put
// real targeting in it later. The contract it satisfies is kit/flags', including
// the half that matters — a key that is not configured answers the caller's
// fallback with Defaulted set, which is how "this installation has never heard of
// that flag" stays distinguishable from "it is off".
type configFlags map[string]bool

var _ flags.Evaluator = configFlags(nil)

func (f configFlags) Boolean(_ context.Context, key string, _ flags.Subject, fallback bool) (flags.Decision, error) {
	value, configured := f[key]
	if !configured {
		return flags.Decision{Value: fallback, Defaulted: true}, nil
	}
	return flags.Decision{Value: value}, nil
}

// settingsGate answers modules/site's question — may this tenant's settings be
// written directly? — by asking the flag. It is the only place in this application
// where a flag changes what a request gets, and it refuses rather than silently
// dropping the write: the answer has to say where the write goes.
type settingsGate struct{ eval flags.Evaluator }

var _ sitecontracts.WriteGate = settingsGate{}

func (g settingsGate) Check(ctx context.Context, tx db.Tx[db.Tenant]) error {
	tenant := db.TenantOf(tx)
	actor, _ := tenancy.ActorFrom(ctx)
	decision, err := g.eval.Boolean(ctx, siteSettingsFlag,
		flags.Subject{TenantID: tenant.ID, TargetingKey: actor.String()}, false)
	if err != nil {
		// The fallback this caller passed is false, and kit/flags' rule is that a
		// failed evaluation returns it with Defaulted set: a flag service that is
		// unreachable leaves every tenant's settings door open rather than locking
		// them out of their own site. Refusing a write because a question could not
		// be answered would be the worse failure, and it would be a way to take the
		// last one away.
		return nil
	}
	if !decision.Value {
		return nil
	}
	// crud.ErrConflict is the status — the row is not in a state this door writes —
	// and the wrapped Refusal is the sentence: a refusal that does not name the way
	// through it is not an answer (changecontracts.Refusal).
	return fmt.Errorf("%w: %w", crud.ErrConflict, &changecontracts.Refusal{
		SubjectModule: "site", SubjectEntity: "settings", SubjectID: uuid.Nil,
		Path: proposalAddress, Permission: changecontracts.PermissionChangePropose,
	})
}

// siteSubject is the tenant's site settings as a change subject: the two methods
// modules/change needs to know what a row looks like and to write it, in the hands
// of the module that owns the row. Nothing here opens site_settings — the site
// service reads and writes its own table, and this is the adapter that says which
// of its doors a proposal comes through.
type siteSubject struct{ sites sitecontracts.Service }

var _ changecontracts.Subject = siteSubject{}

// Lock is the read a diff is made against, taken with the row locked, so the
// revision recorded beside the diff is the revision nobody else can move while
// this transaction is open. A tenant that has never saved its site is at revision
// 0 with the defaults, which is a real base revision: the first write to this
// tenant's settings is as reviewable as the tenth.
func (s siteSubject) Lock(ctx context.Context, tx db.Tx[db.Tenant]) (json.RawMessage, int64, error) {
	current, err := s.sites.SettingsForUpdate(ctx, tx)
	if err != nil {
		return nil, 0, err
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return nil, 0, fmt.Errorf("site: the settings a diff was made against cannot be written down: %w", err)
	}
	return encoded, current.Revision, nil
}

// Save writes the merged document through the site service, which validates it as
// a site and publishes site.settings_updated beside the proposal's own event. It
// returns the revision the row is on now, which is what the proposal records as
// applied.
//
// This is Service.Save and not the settings route: the gate stands on the route,
// so an approved proposal is never refused by the flag that sent it through the
// proposal in the first place.
func (s siteSubject) Save(ctx context.Context, tx db.Tx[db.Tenant], merged json.RawMessage) (int64, error) {
	var next sitecontracts.SiteSettings
	if err := json.Unmarshal(merged, &next); err != nil {
		return 0, fmt.Errorf("%w: the merged document is not a site settings document: %v", crud.ErrInvalid, err)
	}
	saved, err := s.sites.Save(ctx, tx, &next)
	if err != nil {
		return 0, err
	}
	return saved.Revision, nil
}

// changeSubjects is the literal list of subjects this installation applies
// proposals for. One element, named by its module and entity the way the
// manifest's Nav entries name their screens; a subject missing from this list does
// not exist, which is contracts.ErrUnsupportedSubject's whole meaning.
func changeSubjects(sites sitecontracts.Service) []changecontracts.SubjectBinding {
	return []changecontracts.SubjectBinding{
		{
			Module: "site", Entity: "settings",
			Resolve: func(_ context.Context, _ db.Tx[db.Tenant], subjectID uuid.UUID) (changecontracts.Subject, error) {
				// Site settings are one row per tenant, which is what the nil uuid
				// means in contracts.Proposal. A proposal that named a settings row
				// by id was written by somebody who guessed, and the guess is
				// refused rather than resolved to the one row there is.
				if subjectID != uuid.Nil {
					return nil, fmt.Errorf("%w: a tenant has one site, and %s names no second one",
						crud.ErrInvalid, subjectID)
				}
				return siteSubject{sites: sites}, nil
			},
		},
	}
}
