package events_test

// Two compositions of one app cannot both stand in one process spelling one name two
// ways, and the second only becomes possible when the first is gone: the grip a Catalog
// holds is what makes "this app answers billing.invoice_issued like this" a fact another
// boot has to agree with. Disposal is therefore part of the claim, not a courtesy — a
// composition that ended without giving its names back would leave a test process, or a
// server cycling through compositions of one app, refusing a boot that means exactly what
// it says.
//
// These cases need no database: the claim, the refusal and the release are answers about
// a manifest, and the outbox row they govern is written or not written by the same two
// sentences the cases read.

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/events"
)

type disposedAmount struct {
	Amount int64 `json:"amount"`
}

// disposedText is the same event name spelled as a document that is not disposedAmount.
type disposedText struct {
	Amount string `json:"amount"`
}

var disposedApp = appname.MustParse("acme")

func disposedFirst() []events.Declared {
	return []events.Declared{events.Declare[disposedAmount]("billing.invoice_issued")}
}

func disposedSecond() []events.Declared {
	return []events.Declared{events.Declare[disposedText]("billing.invoice_issued")}
}

// TestADisposedCatalogFreesItsNamesForTheNextComposition is disposal and conflict at one
// door: while a composition holds them its app's names answer one way, and a second that
// spells one of them another way is refused beside it — until the first is disposed.
func TestADisposedCatalogFreesItsNamesForTheNextComposition(t *testing.T) {
	t.Cleanup(func() { events.DeclareAll(nil) })

	first, err := events.NewCatalog(disposedApp, disposedFirst())
	if err != nil {
		t.Fatalf("the first composition was refused: %v", err)
	}
	_, err = events.NewCatalog(disposedApp, disposedSecond())
	if err == nil {
		first.Close()
		t.Fatal("one app answered billing.invoice_issued under two payloads at once")
	}
	if !strings.Contains(err.Error(), "one app holds one shape per event name") {
		t.Errorf("the disagreement was refused with another sentence: %v", err)
	}
	first.Close()

	second, err := events.NewCatalog(disposedApp, disposedSecond())
	if err != nil {
		t.Fatalf("the next composition was refused after the first gave its names back: %v", err)
	}
	second.Close()
	freed, err := events.NewCatalog(disposedApp, disposedFirst())
	if err != nil {
		t.Fatalf("a composition was refused after every earlier one was disposed: %v", err)
	}
	freed.Close()
}

// TestADisposedCatalogEndsOnlyItsOwnClaim is the refcount: two live compositions of one
// app declared one name together, and the disposal of one leaves the other's publish
// measured rather than unanswered.
func TestADisposedCatalogEndsOnlyItsOwnClaim(t *testing.T) {
	t.Cleanup(func() { events.DeclareAll(nil) })
	shared := []events.Declared{events.Declare[disposedAmount]("billing.invoice_issued")}

	first, err := events.NewCatalog(disposedApp, shared)
	if err != nil {
		t.Fatalf("the first composition was refused: %v", err)
	}
	defer first.Close()
	second, err := events.NewCatalog(disposedApp, shared)
	if err != nil {
		t.Fatalf("a second composition of the same names was refused: %v", err)
	}
	defer second.Close()

	first.Close()
	if err := events.CheckAppDeclared(disposedApp, shared); err != nil {
		t.Errorf("disposing one composition gave the other's declaration away: %v", err)
	}
	second.Close()
	// Nothing stands for the app now, so a shape it never held is no disagreement.
	if err := events.CheckAppDeclared(disposedApp, disposedSecond()); err != nil {
		t.Errorf("the app still answered its names after both compositions were disposed: %v", err)
	}
}

// TestCompositionsOfOneAppClaimAndGiveBackTheirNamesConcurrently runs claims, disposes and
// the two-composition refusal against one another. Every claim of the same shape succeeds
// whatever interleaving it meets, a claim of a disagreeing shape is refused or accepted
// only by the claims standing, and once every goroutine has finished the names are free
// again — which is the promise the refcount exists to keep under -race.
func TestCompositionsOfOneAppClaimAndGiveBackTheirNamesConcurrently(t *testing.T) {
	t.Cleanup(func() { events.DeclareAll(nil) })
	const goroutines, rounds = 6, 30

	var refused, refusedOtherSentence atomic.Int64
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for range rounds {
				catalog, err := events.NewCatalog(disposedApp, disposedFirst())
				if err != nil {
					refused.Add(1)
					if !strings.Contains(err.Error(), "one app holds one shape per event name") &&
						!strings.Contains(err.Error(), "declared") {
						refusedOtherSentence.Add(1)
					}
					return
				}
				// Every other goroutine declares this same shape, so a claim of it can
				// only ever be refused by a claim of the other shape — and this one is
				// never taken concurrently here.
				if i == 0 {
					if _, err := events.NewCatalog(disposedApp, disposedSecond()); err == nil {
						t.Errorf("a composition disagreeing with a live one was accepted")
					}
				}
				catalog.Close()
			}
		}(i)
	}
	wg.Wait()

	if n := refusedOtherSentence.Load(); n != 0 {
		t.Errorf("%d claims of a shape nothing disagreed with were refused for another reason", n)
	}
	if err := events.CheckAppDeclared(disposedApp, disposedSecond()); err != nil {
		t.Errorf("names from a finished round are still held: %v", err)
	}
	last, err := events.NewCatalog(disposedApp, disposedSecond())
	if err != nil {
		t.Fatalf("the app refused a fresh shape after every claim of the old one ended: %v", err)
	}
	last.Close()
}

// TestDeclareForAndDeclareMoreRefuseOneDisagreementInOneSentence is the delegation, not
// the claim: the unnamed door is the app-named door, and an author reading either error
// reads the same reason.
func TestDeclareForAndDeclareMoreRefuseOneDisagreementInOneSentence(t *testing.T) {
	t.Cleanup(func() { events.DeclareAll(nil) })

	release, err := events.DeclareFor(appname.Name(""), disposedFirst())
	if err != nil {
		t.Fatalf("the unnamed composition's claim was refused: %v", err)
	}
	defer release()

	for name, claim := range map[string]func([]events.Declared) (func(), error){
		"DeclareFor":  func(list []events.Declared) (func(), error) { return events.DeclareFor("", list) },
		"DeclareMore": events.DeclareMore,
	} {
		_, err := claim(disposedSecond())
		if err == nil {
			t.Fatalf("%s accepted a second shape for a name the deployment already answers", name)
		}
		if !strings.Contains(err.Error(), "one app holds one shape per event name") {
			t.Errorf("%s refused with another sentence: %v", name, err)
		}
	}
}

// TestANewCatalogRefusesANameItSpellsTwoWays is the within-one-list refusal, which a
// constructor must give rather than pick a winner: the composition is wrong, not the list.
func TestANewCatalogRefusesANameItSpellsTwoWays(t *testing.T) {
	list := append(disposedFirst(), disposedSecond()...)
	_, err := events.NewCatalog(disposedApp, list)
	if err == nil {
		t.Fatal("a catalog accepted one event name under two payloads in one list")
	}
	if !strings.Contains(err.Error(), "one event has one payload shape") {
		t.Errorf("the disagreement inside one list was refused with another sentence: %v", err)
	}
	if _, err := events.NewCatalog(appname.Name("not a slug!"), disposedFirst()); err == nil {
		t.Error("a name appname refuses was accepted as an app")
	}
}
