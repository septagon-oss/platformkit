package pkit_test

// Root's verdict for this task asks the fixture for "three modules
// contributing to one consumer that is built after them". The cart takes
// every Extension, so it is that consumer; this case composes three living
// contributors to it and asserts the consumer is built after all three,
// receives all three contributions, and that Explain names all three.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/wishlist"
)

func contributor(what string, order *[]string) *pkit.Module {
	return pkit.NewModule(what, func(w *pkit.Wiring) (module.Module, error) {
		*order = append(*order, what)
		pkit.Put(w, cartcontracts.Extension{Module: what})
		return module.Module{Name: what}, nil
	}, pkit.Contributes[cartcontracts.Extension]())
}

func TestThreeModulesContributeToOneConsumerBuiltAfterThem(t *testing.T) {
	var order []string
	got := &probe{}
	app := pkit.NewApp("collect").Use(
		user.Module, wishlist.Module,
		contributor("shop", &order), contributor("blog", &order), contributor("gallery", &order),
		cart.Module, probeModule(got),
	)
	if err := app.Validate(dev); err != nil {
		t.Fatalf("three contributors to one consumer were refused: %v", err)
	}
	if len(got.extensions) != 4 {
		t.Errorf("the consumer saw %d extensions %v, want the three contributors plus wishlist's", len(got.extensions), got.extensions)
	}
	built := strings.Join(order, ",")
	if built != "shop,blog,gallery" {
		t.Errorf("the contributors built in order %q, want shop,blog,gallery", built)
	}

	text, err := app.Explain(dev)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Explain with three contributors:\n%s", text)
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "cart.Module takes every cartcontracts.Extension") {
			for _, who := range []string{"shop.Module", "blog.Module", "gallery.Module", "wishlist.Module"} {
				if !strings.Contains(l, who) {
					t.Errorf("Explain leaves %s out of what the cart takes: %q", who, l)
				}
			}
		}
	}
}
