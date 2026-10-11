package change

import (
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/ui/page"
)

// ReviewChrome is what a composition hands this module so its two screens can be
// drawn: the shell they are drawn in, whose answer to "may this caller decide" the
// decision controls reuse, and how many rows one page holds.
//
// Read and Decide are deliberately not among them: they are the one service the
// resolver builds below, and a composition that could name them could name a queue
// reading a different service than the routes beside it. What the application owns
// is the chrome and the authorizer, which is what this edge carries and nothing more.
type ReviewChrome struct {
	Shell     page.Shell
	Authorize httpx.Authorizer
	PerPage   int
}

// Module is change control as the resolver sees it, for an application that
// names it in Use: `Use(change.Module)`.
//
// It owns the object — the proposal, its digest, its state machine, the
// four-eyes rule — and no opinion about which writes need one, because that
// opinion is a fact about an installation. So the subjects arrive as
// contributions, and a composition that contributes none mounts the six
// proposal routes over nothing: an object with no subject is a review queue that
// is always empty, which is why the absence is Explain's note rather than a
// silence.
//
// The three Optional edges are the module's own screens and notices. A proposal
// with no way to read it and nobody to tell leaves every product to write its own
// queue, which is the one-client module this repository exists to prevent; and a
// module that guessed either — which chrome its queue sits in, whether a mail
// server exists, what its page is addressed at — would be a module with a product
// in it. So all three arrive from the composition, or do not arrive and the
// manifest says so in the same shape it always did: no subscription, no queue,
// and the surface's own 404 for anybody who asks for a screen nobody composed.
var Module = pkit.NewModule("change", wire,
	pkit.Needs[[]changecontracts.SubjectBinding](),
	pkit.Optional[changecontracts.Notifier](),
	pkit.Optional[changecontracts.ProposalPage](),
	pkit.Optional[*ReviewChrome](),
	pkit.Provides[changecontracts.Service](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	subjects := pkit.All[changecontracts.SubjectBinding](w)
	svc := NewService(subjects)
	deps := Deps{
		Service:      svc,
		Notify:       pkit.Get[changecontracts.Notifier](w),
		ProposalPage: pkit.Get[changecontracts.ProposalPage](w),
	}
	// The two screens are drawn by this module and composed by the application: the
	// pages arrive with their shell, their authorizer and their page size, and the
	// two things only this module can supply — the queue's read and the four
	// commands the controls post to — are filled here, from the one service the
	// resolver built. No pages at all leaves Read nil, which is the absence
	// MountReviews already answers with nothing.
	if chrome := pkit.Get[*ReviewChrome](w); chrome != nil {
		deps.Reviews = ReviewPages{
			Shell: chrome.Shell, Authorize: chrome.Authorize, PerPage: chrome.PerPage,
			Read: svc, Decide: svc,
		}
	}
	manifest := New(deps)
	pkit.Put[changecontracts.Service](w, svc)
	return manifest, nil
}
