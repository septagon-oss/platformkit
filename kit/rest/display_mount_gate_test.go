package rest_test

// The row-name mark is refused where widgetFault and presentationFault are
// refused: at Mount, on a Spec and on a Singleton. The sentence itself is
// pinned in display_test.go; what is pinned here is that both mount sites ask
// it, in the shape presentation_gate_test.go already uses — the panic must name
// the field, so a router-less mount panicking for its own reasons cannot pass.

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// Twice marks two fields as the one a row is called by.
type Twice struct {
	crud.Base
	Name  string `json:"name" ui:"display"`
	Alias string `json:"alias" ui:"display"`
}

func (Twice) TableName() string { return "rest_twice" }

func refusedForTheSecondMark(t *testing.T, site string, mount func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Errorf("%s mounted an entity that names two display fields", site)
			return
		}
		message, _ := recovered.(string)
		if !strings.Contains(message, `"alias"`) || !strings.Contains(message, "second display field") {
			t.Errorf("%s refused for another reason: %v", site, recovered)
		}
	}()
	mount()
}

func TestASecondDisplayMarkIsRefusedAtEitherMount(t *testing.T) {
	refusedForTheSecondMark(t, "Spec", func() {
		rest.Spec[*Twice]{Module: "twice", Entity: "twice", Path: "/doubles",
			Read: "twice:read", Write: "twice:write"}.Mount(httpx.Surfaces{})
	})
	refusedForTheSecondMark(t, "Singleton", func() {
		rest.Singleton[*Twice]{Module: "twice", Entity: "twice", Path: "/doubles", Read: "twice:read",
			Load: func(context.Context, db.Tx[db.Tenant]) (*Twice, error) { return &Twice{}, nil },
		}.Mount(httpx.Surfaces{})
	})
}
