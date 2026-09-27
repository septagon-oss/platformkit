package sitetest_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
)

// Review 2, finding 1 of review 1, third instance. Review 1 read this one from
// the code and left it in its Unverified list: "saving what is already stored
// says nothing" asserted again.ID != first.ID on the second Save, and the
// generated retry compared stored rows only. The cure claims sitetest's answer
// now carries the id, so a service that stores the settings it was given and
// answers a different row must be refused by the case that kept the sentence.
//
// This pins that claim for sitetest. It passes while the retry compares the two
// answers and the id is one of the fields the answer renders; it fails if either
// is dropped.
func TestTheRetryReadsWhatSaveAnswered(t *testing.T) {
	// Reachability through the fixed behaviour: the fake the package ships
	// passes its own suite, so a failure below belongs to the mutant.
	if ok := runSiteSuiteQuietly(t, nil); !ok {
		t.Fatalf("the conformance suite does not pass the fake it ships with; nothing below means anything")
	}
	if ok := runSiteSuiteQuietly(t, func(s contracts.Service) contracts.Service {
		return &answersAnotherSite{Service: s}
	}); ok {
		t.Errorf("the conformance suite passed a service whose second Save stores what it was given and answers a " +
			"row with an id nothing wrote; \"saving what is already stored says nothing\" asserted again.ID != " +
			"first.ID, and a caller reading the answer is handed settings that are not the tenant's")
	}
}

// runSiteSuiteQuietly runs sitetest.RunService against the fake, optionally
// wrapped, and reports whether it passed. It runs in a testing.T of its own so a
// suite that fails on purpose does not fail the test watching it.
func runSiteSuiteQuietly(t *testing.T, wrap func(contracts.Service) contracts.Service) bool {
	t.Helper()
	return testing.RunTests(
		func(pat, str string) (bool, error) { return true, nil },
		[]testing.InternalTest{{
			Name: "sitetest_RunService",
			F: func(inner *testing.T) {
				sitetest.RunService(inner, func(inner *testing.T, run func(sitetest.Fixture)) {
					fake := sitetest.NewFake()
					var svc contracts.Service = fake
					if wrap != nil {
						svc = wrap(fake)
					}
					run(sitetest.Fixture{Ctx: inner.Context(), Service: svc, Published: fake.Published})
				})
			},
		}})
}

// answersAnotherSite stores what the fake stores and answers a row with a fresh
// id when — and only when — the save changed nothing. That is the retry's own
// shape and no other case's: the generated success saves onto an unconfigured
// site, and "changing one thing says so" saves something different the second
// time, so both read a truthful answer from this service. A suite that fails
// against it fails in the retry, and nowhere else.
type answersAnotherSite struct {
	contracts.Service
}

func (a *answersAnotherSite) Save(ctx context.Context, tx db.Tx[db.Tenant], in *contracts.SiteSettings) (*contracts.SiteSettings, error) {
	before, err := a.Service.Settings(ctx, tx)
	if err != nil {
		return nil, err
	}
	got, err := a.Service.Save(ctx, tx, in)
	if err != nil || got == nil {
		return got, err
	}
	if configuration(before) == configuration(got) {
		lie := *got
		lie.ID = uuid.New()
		return &lie, nil
	}
	return got, nil
}

// configuration is everything a save can change, so that "this save changed
// nothing" is a comparison and not a guess. The id is not in it: the id is what
// the lie moves.
func configuration(s *contracts.SiteSettings) string {
	if s == nil {
		return "nothing"
	}
	return fmt.Sprintf("title=%q tagline=%q home=%q theme=%s colour=%s nav=%v",
		s.Title, s.Tagline, s.HomeSlug, s.Theme, s.PrimaryColor, s.Nav)
}
