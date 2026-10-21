// Package decide declares the reusable group decision capability.
package decide

import (
	"fmt"
	"time"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"github.com/septagon-oss/platformkit/modules/decide/internal"
	"github.com/septagon-oss/platformkit/pkit"
)

// Deps is the complete composition boundary. Access retains current authority
// locks; Clock and Key are required. SweepEvery zero selects one minute.
type Deps struct {
	Access     contracts.Access
	Clock      contracts.Clock
	Key        contracts.InviteKey
	Notifier   contracts.Notifier
	Tenants    jobs.TenantLister
	Uses       rest.FileUses
	Shown      rest.FileShown
	Images     richtext.Files
	SweepEvery time.Duration
}

// Module provides Decide through the typed plan, with a one-minute deadline job.
var Module = WithSweepEvery(0)

// WithSweepEvery declares the same module with a custom deadline-job interval.
// Zero selects one minute; a negative interval refuses composition before I/O.
func WithSweepEvery(every time.Duration) *pkit.Module {
	return pkit.NewModule("decide", func(w *pkit.Wiring) (module.Module, error) {
		return wire(w, every)
	}, pkit.Needs[contracts.Access](), pkit.Needs[contracts.Clock](), pkit.Needs[contracts.InviteKey](),
		pkit.Needs[contracts.Notifier](), pkit.Needs[jobs.TenantLister](), pkit.Needs[rest.FileUses](),
		pkit.Needs[rest.FileShown](), pkit.Needs[richtext.Files](), pkit.Provides[contracts.Service](),
		pkit.Contributes[personaldata.Part]())
}

func wire(w *pkit.Wiring, every time.Duration) (module.Module, error) {
	d := Deps{Access: pkit.Get[contracts.Access](w), Clock: pkit.Get[contracts.Clock](w), Key: pkit.Get[contracts.InviteKey](w),
		Notifier: pkit.Get[contracts.Notifier](w), Tenants: pkit.Get[jobs.TenantLister](w), Uses: pkit.Get[rest.FileUses](w),
		Shown: pkit.Get[rest.FileShown](w), Images: pkit.Get[richtext.Files](w), SweepEvery: every}
	if d.SweepEvery < 0 || d.Notifier == nil || d.Tenants == nil {
		return module.Module{}, fmt.Errorf("decide: notifier, tenant lister and nonnegative sweep interval are required")
	}
	if d.SweepEvery == 0 {
		d.SweepEvery = time.Minute
	}
	svc, err := internal.New(d.Access, d.Clock, d.Key, d.Uses, d.Shown, d.Images)
	if err != nil {
		return module.Module{}, err
	}
	pkit.Put[contracts.Service](w, svc)
	pkit.Put[personaldata.Part](w, internal.PersonalData())
	return module.Module{Name: "decide", Migrations: Migrations.Files, Declared: contracts.Events,
		PersonalData: personaldata.Implements(
			personaldata.Table{Name: "decide_questions", Person: "creator_id", Text: []string{"title", "description"}},
			personaldata.Table{Name: "decide_options", NotPerson: []string{"question_id", "image_id"}, Text: []string{"title", "note", "link"}},
			personaldata.Table{Name: "decide_participants", Person: "person_id", NotPerson: []string{"question_id"}},
			personaldata.Table{Name: "decide_votes", Person: "person_id", NotPerson: []string{"question_id", "option_id"}},
		),
		Permissions: []module.Permission{{Key: contracts.PermissionRead, Label: "Read group decisions"},
			{Key: contracts.PermissionManage, Label: "Create and manage group decisions"}, {Key: contracts.PermissionVote, Label: "Join and vote on group decisions"}},
		Routes: svc.Routes, Jobs: []jobs.Job{svc.Deadlines(d.Tenants, d.SweepEvery)},
		Subscriptions: []events.Subscription{internal.Tell(d.Notifier)}}, nil
}
