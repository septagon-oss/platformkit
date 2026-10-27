package rest_test

// singleton_that_moves_a_door_to_the_operator_addresses_it_where_it_answers_test.go asks
// the projection, not the document. A served /openapi.json is one answer a client reads;
// the generated screens and the phone's catalogue read two fields of the registered
// resource instead — Schema.Path, and WritePath, which kit/httpx documents as naming the
// write's address *only when the two doors answer at different prefixes*, the empty value
// meaning "writes answer where reads do". rest.Singleton composes both from wherever its
// two doors landed once the operator grants were read (kit/rest/singleton.go), so the two
// views of one row agree only as long as that composition runs.
//
// The four grants are asked together because each pair answers a different question: the
// tenant-only arm is the projection a settings row has always had, the write-only arm is
// the split kit/httpx documents for a price list, the read-only arm is its mirror, and the
// both arm is the row the operator holds outright — the one whose two doors leave the
// tenant's host together and so name no second address at all.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// splitDoorBoard is a tenant's one drawing row, the shape a module keeps for a drawing the
// tenant configures once and somebody else may hold the door to.
type splitDoorBoard struct {
	crud.Base
	Drawing  entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64         `json:"revision" readOnly:"true"`
}

func (splitDoorBoard) TableName() string { return "rest_split_door_boards" }

// servedDoor is one door of the served document: the address it answers at and the surface
// the kernel wrote it on.
type servedDoor struct{ path, surface string }

// mountedDoors mounts a tenant's one drawing row with the given operator grants and reads
// back what two different clients would be told: the resource the kernel registered, and
// each verb the served document answers, keyed by method. A verb answered at two addresses
// is reported rather than chosen between, because the projection names one address only.
func mountedDoors(t *testing.T, operatorRead, operatorWrite bool) (httpx.Resource, map[string]servedDoor) {
	t.Helper()
	board := rest.Singleton[*splitDoorBoard]{
		Module: "drawing", Entity: "board", Path: "/board",
		Read: "drawing:read", Write: "drawing:write",
		OperatorRead: operatorRead, OperatorWrite: operatorWrite,
		Load: func(context.Context, db.Tx[db.Tenant]) (*splitDoorBoard, error) {
			return &splitDoorBoard{}, nil
		},
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *splitDoorBoard) (*splitDoorBoard, error) {
			return in, nil
		},
	}
	api, router := patchDocumentAPI()
	board.Mount(api.Surfaces(board.Module))
	resources := api.Resources()
	if len(resources) != 1 {
		t.Fatalf("the mount registered %d resources, want the one row it serves: %v", len(resources), resources)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("the served document = %d %s", w.Code, w.Body.String())
	}
	var document struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatalf("the served document is not the JSON a client reads: %v (%s)", err, w.Body)
	}
	doorOf := map[string]servedDoor{}
	for path, byMethod := range document.Paths {
		for method, operation := range byMethod {
			named, held := operation[httpx.SurfaceExtension]
			if !held {
				t.Errorf("%s %s is served with no surface named, so no client can tell whose door it is",
					strings.ToUpper(method), path)
				continue
			}
			verb := strings.ToUpper(method)
			if twice, seen := doorOf[verb]; seen {
				t.Errorf("%s answers at %s and at %s, which one projection of one row cannot name", verb, twice.path, path)
				continue
			}
			doorOf[verb] = servedDoor{path: path, surface: fmt.Sprint(named)}
		}
	}
	return resources[0], doorOf
}

// TestASingletonThatMovesADoorToTheOperatorAddressesItWhereItAnswers holds the two views of
// one row to each other: the address the registered resource names for a read, and the one
// it names for a write, are the addresses the served document answers those verbs at —
// whichever surface each door was moved to. Where the two doors share an address the
// resource names no write at all, which is what kit/httpx reads as "write it where you read
// it"; where they part, the write address has to be the door that takes the write, or a
// generated form posts to a prefix on which nothing listens.
func TestASingletonThatMovesADoorToTheOperatorAddressesItWhereItAnswers(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		operatorRead, operatorWrite bool
		readSurface, writeSurface   httpx.Surface
	}{
		{"both doors are the tenant's", false, false, httpx.SurfaceApp, httpx.SurfaceApp},
		{"the read is the operator's", true, false, httpx.SurfaceOps, httpx.SurfaceApp},
		{"the write is the operator's", false, true, httpx.SurfaceApp, httpx.SurfaceOps},
		{"both doors are the operator's", true, true, httpx.SurfaceOps, httpx.SurfaceOps},
	} {
		resource, doors := mountedDoors(t, tc.operatorRead, tc.operatorWrite)
		read, reads := doors[http.MethodGet]
		write, writes := doors[http.MethodPut]
		if !reads || !writes {
			t.Errorf("%s: the served document answers GET=%v PUT=%v, so the projection cannot be compared to it; the other grants are still asked", tc.name, read, write)
			continue
		}
		if read.surface != string(tc.readSurface) {
			t.Errorf("%s: the read door answers at %s on surface %q, want the %s the mount declares",
				tc.name, read.path, read.surface, tc.readSurface)
		}
		if write.surface != string(tc.writeSurface) {
			t.Errorf("%s: the write door answers at %s on surface %q, want the %s the mount declares",
				tc.name, write.path, write.surface, tc.writeSurface)
		}
		if resource.Schema.Path != read.path {
			t.Errorf("%s: the resource projects its reads at %q, which is where nothing answers them: the served GET is at %q",
				tc.name, resource.Schema.Path, read.path)
		}
		addressed := resource.WritePath
		if addressed == "" {
			addressed = resource.Schema.Path
		}
		if addressed != write.path {
			t.Errorf("%s: a caller pointed by the projection at %s for the write reaches no door: the served PUT answers at %s",
				tc.name, addressed, write.path)
		}
		if namedElsewhere := resource.WritePath != ""; namedElsewhere == (read.path == write.path) {
			t.Errorf("%s: the resource names a separate write address of %q while its doors answer at %q and %q, so the projection is wrong about whether they part",
				tc.name, resource.WritePath, read.path, write.path)
		}
	}
}
