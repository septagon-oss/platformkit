package translationtest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/translation/contracts/translationtest"
)

// TestTheFakeIsAService runs the whole conformance suite against the fake. A
// fake that did not pass it would be a second, quieter opinion about what the
// port does — the failure AGENTS.md rule 8 names, and the reason the real
// service runs the same map in modules/translation/internal/service_test.go.
func TestTheFakeIsAService(t *testing.T) {
	translationtest.RunService(t, func(t *testing.T, run func(translationtest.Fixture)) {
		ctx := t.Context()
		tx := db.Tx[db.Tenant]{}
		source := translationtest.NewStubSource()
		machine := &translationtest.Machine{}
		fake := translationtest.NewFake(machine, source)
		run(translationtest.Fixture{
			Ctx: ctx, Tx: tx, Service: fake, Source: source, Machine: machine,
			Published: fake.Published,
			Payloads:  fake.Payloads,
			Rows:      func() []translationtest.Row { return fake.Rows(ctx, tx) },
		})
	})
}
