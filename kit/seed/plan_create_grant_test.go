package seed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
)

type createDenied struct{}

func (createDenied) Check(_ context.Context, _ db.Tx[db.Tenant], _ Resource, action Action) error {
	if action == Create {
		return errors.New("create grant is not held")
	}
	return nil
}

func TestPlanRefusesACreateTheActorCannotApply(t *testing.T) {
	writer := newFakeWriter("contents", "content", "content", true)
	service, err := New(Deps{
		Files: fstest.MapFS{"seed/starter/contents.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: home\n    fields: {title: Home}\n")}},
		Root:  "seed", Clock: seedAt, Writers: []Writer{writer}, Authorize: createDenied{},
	})
	if err != nil {
		t.Fatal(err)
	}
	seedTenant(t, false, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		plan, planErr := service.Plan(ctx, tx, Selection{})
		if planErr == nil || !strings.Contains(planErr.Error(), "create grant is not held") {
			t.Errorf("Plan = %+v, %v; want the create grant refusal", plan, planErr)
		}
		applied, applyErr := service.Apply(ctx, tx, Selection{})
		if applyErr == nil || !strings.Contains(applyErr.Error(), "create grant is not held") {
			t.Errorf("Apply = %+v, %v; want the same create grant refusal", applied, applyErr)
		}
		if len(writer.writes) != 0 {
			t.Errorf("refused create reached the owner: %v", writer.writes)
		}
		return nil
	})
}
