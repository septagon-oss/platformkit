package pkit_test

// 0074 rule 3 gives one contract one provider, or the app chooses, and a
// product contributes the one landing its page is: "Homepage … take the
// contributions, and refuse two where they accept one". The composition checks
// that a module which declares Provides[T] put exactly one T
// ("declares it provides … and did not put it once"); nothing checks a module
// that declares Contributes[T]. These two cases are what that gap accepts.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/homepage"
	homepagecontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/homepage/contracts"
)

// TestAContributionDeclaredTwiceIsAnswered pins that one module may not say
// Contributes[E] twice and have every value it puts reach the taker twice. The
// delivery answers either by refusing the composition or by counting the
// module once among the contributors; what it may not do is wire the same two
// contributions into the taker as four.
func TestAContributionDeclaredTwiceIsAnswered(t *testing.T) {
	dup := pkit.NewModule("dup", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put(w, cartcontracts.Extension{Module: "one"})
		pkit.Put(w, cartcontracts.Extension{Module: "two"})
		return module.Module{Name: "dup"}, nil
	}, pkit.Contributes[cartcontracts.Extension](), pkit.Contributes[cartcontracts.Extension]())

	got := &probe{}
	err := pkit.NewApp("collect").Use(dup, cart.Module, probeModule(got)).Validate(dev)
	if err == nil {
		if len(got.extensions) != 2 {
			t.Errorf("the taker was wired %d extensions %v from two contributions, and the composition said nothing",
				len(got.extensions), got.extensions)
		}
	}
}

// TestAContributionPutTwiceIsAnswered pins the case of a module that declares
// one contribution and puts two: a module that takes exactly one (homepage
// takes one Landing) currently reads the first and drops the second without a
// sentence.
func TestAContributionPutTwiceIsAnswered(t *testing.T) {
	two := pkit.NewModule("two", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put(w, homepagecontracts.Landing{Path: "/first"})
		pkit.Put(w, homepagecontracts.Landing{Path: "/second"})
		return module.Module{Name: "two"}, nil
	}, pkit.Contributes[homepagecontracts.Landing]())

	if err := pkit.NewApp("collect").Use(two, homepage.Module).Validate(dev); err == nil {
		t.Error("a module that contributes two landings to a module that takes one was accepted, and the taker reads only the first")
	}
}
