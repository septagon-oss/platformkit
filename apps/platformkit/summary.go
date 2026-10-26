package main

// summary.go is the composition's answer to "what does this hold?" — one number
// per resource the caller may read, at one instant, at /api/v1/app/summary. The
// address is the kernel's and the body is ui's, which makes the mount the
// composition's: no module may compose the module-less /api/v1/app prefix, and only
// the composition holds the whole registered list, read through the closure each
// resource carries — the one the generated screens call, behind the same guard.
//
// Three rules hold here and nowhere else. A cached total is never handed to a caller
// the guard would refuse, because Readable is asked before the key is formed; an
// authorizer that decided per row would make a per-tenant count wrong, and that key
// would name the grant set or go uncached. Absence is never stored — a refused count
// leaves no number, since a stored absence reads back as a decision — except a
// statement the database refused, which stops the loop and fails the request. And
// nothing here is a word, so no Vary and no key names a language.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/screens"
)

const (
	// summaryTTL is how long one tenant's count of one resource is believed. It
	// bounds a number a person acts on, so it is shorter than the half minute a
	// host resolution gets: a card that refreshed twice in one screen reading must
	// not be able to show the same stale fifteen seconds twice. Nothing is wired
	// to invalidate it earlier — an invalidation would put one module's writes in
	// another module's namespace, for a card that refreshes on every foreground.
	summaryTTL = 15 * time.Second

	// summaryBudget bounds the parts of one request that take this context: the
	// store reads, which on a remote store are a round trip per entry, and the
	// read-guard decision asked of every resource. It bounds no statement — the
	// transaction opened on the request's own context before this deadline existed
	// and crud's reads take no context of their own, so a query is cut short by the
	// installation's statement_timeout and by nothing here. Past it the rest of the
	// list is not asked and its entries carry no number, which is the absence the
	// body already uses for a count nobody gave.
	summaryBudget = 5 * time.Second

	// summaryNamespace is this document's scope inside the shared store. A
	// namespace names no slash — kit/cache refuses one — because the tenant
	// segment is what isolates it.
	summaryNamespace = "summary"
)

// summaryFace is the composition's count of its own resources. api arrives at the
// mount, and now is late-bound the way connectionFace.describe is, so a case can
// ask what "Due today" means at one named instant rather than whenever the test
// happened to run.
type summaryFace struct {
	api *httpx.API
	now func() time.Time
}

func (f *summaryFace) clock() time.Time {
	if f.now == nil {
		return db.Now()
	}
	return f.now()
}

// summaryDocument is the envelope, for the reason connectionDocument is: a body
// handed to huma as `any` leaves its fields out of the published document, and a
// sparkline is written against the names in that document. It carries no Vary
// because no header changes these bytes.
type summaryDocument struct {
	Body *screens.Summary
}

// summaryAnswers is one resource's counts as the store holds them: the answers,
// never the body. A body would key one caller's resource list — a fact about
// grants — and would re-answer every resource to refresh one.
type summaryAnswers struct {
	Total     *int64 `json:"total,omitempty"`
	Highlight *int64 `json:"highlight,omitempty"`
	// Days carries no omitempty: an empty slice is the answer a quiet window
	// deserves and has to survive the round trip, while a window nobody counted
	// stays null and reads back as the absence it is.
	Days []crud.DayCount `json:"days"`
}

func (f *summaryFace) mount(api *httpx.API) {
	f.api = api
	httpx.Register(api.Surfaces("").App, huma.Operation{
		OperationID: "app-summary",
		Method:      http.MethodGet,
		Path:        "/summary",
		Summary:     "What each resource this caller may read holds, at one instant",
		Description: "One total per readable resource, plus the one sub-count and the daily series each resource declares. " +
			"A number the installation could not answer is absent, which is not zero: an empty workspace and a workspace " +
			"that would not answer are two different things to say. Nothing here is a word — the labels are in /resources — " +
			"so the body is the same in every language.",
		Tags:   []string{"kernel"},
		Errors: []int{http.StatusServiceUnavailable},
	}, httpx.AnyCredential(), func(ctx context.Context, _ *struct{}) (*summaryDocument, error) {
		body, err := f.answer(ctx)
		if err != nil {
			return nil, err
		}
		return &summaryDocument{Body: body}, nil
	})
}

// answer is the read: one transaction — the request's own, so row-level security
// scopes every statement with no help from here, and there is no parameter with
// which to name another tenant — one clock, and one statement per question each
// entry declares: a card that declares a total, a sub-count and a daily series
// costs three, because a total and a sub-count of the same rows narrow differently
// and no one read answers both.
func (f *summaryFace) answer(ctx context.Context) (*screens.Summary, error) {
	// The transaction is only checked for being there. Every read goes through the
	// resource's own closures, which resolve it from this same context, so a
	// tenant scope cannot be named here even by omission.
	if _, ok := httpx.TxFrom(ctx); !ok {
		return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	tenant, ok := tenancy.FromContext(ctx)
	if !ok {
		return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	resources := f.api.Resources()
	asOf := f.clock()
	answers := map[string]screens.Answers{}
	asked, cancel := context.WithTimeout(ctx, summaryBudget)
	defer cancel()
	for _, r := range resources {
		// The same three questions the catalogue asks, in the same order, and for
		// the same reason: an entry of this document and an entry of that one are
		// the same card, and a number for a card no shell was ever given would be a
		// number nobody can name. Readable runs before any cache key is formed.
		if r.CommandsOnly || !r.Offers(httpx.CRUDList) || !r.Readable(asked) {
			continue
		}
		got, ok, spent := f.countOne(asked, tenant.ID, r, asOf)
		if spent {
			// A statement Postgres refused leaves this transaction aborted, so the
			// rest of the questions would each fail for the first one's reason and
			// the request would fail anyway. Stop, and let the database answer once.
			break
		}
		if ok {
			answers[r.Module+"/"+r.Entity] = got
		}
	}
	return &screens.Summary{AsOf: asOf.UTC(), Entries: screens.Summarise(asked, resources, answers, asOf).Entries}, nil
}

// countOne is one resource's counts: from the store if the store still believes
// them, and from the resource's own guarded closures otherwise. Its second answer
// is "nothing was answered", which writes nothing; its third is "the database
// refused a statement", which means this request's transaction is spent and no
// further question should be asked of it. It is handed no transaction: the
// closures read through the request's own, which is what keeps the tenant scope
// out of this file.
func (f *summaryFace) countOne(ctx context.Context, tenant uuid.UUID,
	r httpx.Resource, asOf time.Time) (screens.Answers, bool, bool) {
	// A declared sub-count or trend is resolved against a UTC day, so the day
	// belongs in the name: at midnight the key moves rather than one day's window
	// being served as the next one's.
	day := ""
	if r.Present.Highlight != nil || r.Present.TrendField != "" {
		day = "/" + crud.DayStart(asOf).Format("2006-01-02")
	}
	key := cache.Of(tenant, summaryNamespace).Entry(r.Module + "/" + r.Entity + day)
	store := f.api.Cache()
	buf, found, under := fromStore(ctx, store, key)
	if found {
		var stored summaryAnswers
		if err := json.Unmarshal(buf, &stored); err == nil {
			return screens.Answers{Total: stored.Total, Highlight: stored.Highlight, Days: stored.Days}, true, false
		}
		// A value this package cannot read is a miss and not a refusal: the
		// database is the truth for a count, and asking it costs one query.
	}
	loaded, ok, spent := f.load(ctx, r, asOf)
	if spent {
		return screens.Answers{}, false, true
	}
	if !ok {
		return screens.Answers{}, false, false
	}
	if buf, err := json.Marshal(summaryAnswers{
		Total: loaded.Total, Highlight: loaded.Highlight, Days: loaded.Days,
	}); err == nil {
		// The write carries the generation the miss above read open, so an
		// invalidation that landed during the queries closes this entry too.
		if err := store.Set(ctx, key, buf, summaryTTL, under); err != nil {
			f.log(ctx, r, "could not store its count; this process will ask again", err)
		}
	}
	return loaded, true, false
}

// fromStore is the store read, with every failure of the store itself read as a miss
// alongside the generation the read did answer. kit/httpx resolves a host the same
// way: a cache that has stopped answering costs one query, never a refusal, and a
// caller that decides to load has to be handed the generation its own read saw in
// order to say what its write is worth.
func fromStore(ctx context.Context, store cache.Cache, key cache.Key) ([]byte, bool, cache.Generation) {
	buf, found, under, err := store.Get(ctx, key)
	if err != nil || !found {
		return nil, false, under
	}
	return buf, true, under
}

// load asks the entry's declared questions, in the request's transaction. Each
// question is asked only where the entry declares it: an entry that hints nothing
// costs one statement, and a card that could not be answered is absent rather
// than zero. Its second answer is "nothing at all was answered", the one case
// that must never reach the store; its third is "a statement was refused", after
// which there is nothing further to ask in this transaction.
func (f *summaryFace) load(ctx context.Context, r httpx.Resource, asOf time.Time) (screens.Answers, bool, bool) {
	var answers screens.Answers
	answered, spent := false, false
	// Every closure is asked behind a nil check: a resource built by hand may
	// carry no count at all, and a card with no number is the answer, where a
	// panic in a document a shell is waiting on is not.
	if r.Count == nil {
		return answers, false, false
	}
	if total, err := r.Count(ctx); err != nil {
		f.log(ctx, r, "answers no total", err)
		spent = true
	} else {
		answers.Total = &total
		answered = true
	}
	if h := r.Present.Highlight; h != nil {
		where, err := crud.Resolve(r.Schema.Fields, h.Filter, asOf)
		switch {
		case err != nil:
			// No statement ran: this is a declaration the mount gate should have
			// refused, and the transaction is still good.
			f.log(ctx, r, "declares a highlight this installation cannot ask", err)
		case r.CountFiltered == nil:
			f.log(ctx, r, "declares a highlight and carries no filtered count", nil)
		default:
			n, err := r.CountFiltered(ctx, where)
			if err != nil {
				f.log(ctx, r, "answers no highlight", err)
				spent = true
			} else {
				answers.Highlight = &n
			}
		}
	}
	if field := r.Present.TrendField; field != "" {
		from, to := screens.TrendWindow(asOf)
		if r.CountDays == nil {
			f.log(ctx, r, "declares a daily series and carries no daily count", nil)
		} else if counted, err := r.CountDays(ctx, field, from, to); err != nil {
			f.log(ctx, r, "answers no daily series", err)
			spent = true
		} else {
			answers.Days = counted
		}
	}
	return answers, answered, spent
}

// log names the resource whose card will carry no number, and why. The caller is
// told nothing about it — an absent number is the answer this document gives — and
// the operator is told which card would not answer.
func (f *summaryFace) log(ctx context.Context, r httpx.Resource, what string, cause error) {
	slog.WarnContext(ctx, "summary: "+r.Module+"/"+r.Entity+" "+what, "module", r.Module, "entity", r.Entity, "error", cause)
}
