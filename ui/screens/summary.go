package screens

import (
	"context"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// summary.go is what each resource the caller may read holds, at one instant.
//
// It is numbers and nothing else. Every word a person reads about a resource —
// its singular, its group, a highlight's label — is in the catalogue, resolved
// once through ui/resource, and an entry of this document joins it on the same
// (module, entity) pair the catalogue publishes. That is what lets this body be
// the same bytes in every language, and so be cached per tenant rather than per
// tenant and person: a key may name no language because no answer under it is
// written in one.
//
// Every number is a pointer. A total of zero is an answer the installation gave
// and prints `"total":0`; a total nobody could answer prints no key at all, and
// a shell draws no card. An int64 cannot say both, and the difference is the
// whole difference between an empty workspace and a workspace that could not be
// counted.
//
// The entry set is the catalogue's, decided by the same method: a resource that
// is not readable, that mounts no list, or that a shell is only given commands
// for is in neither document. No number can exist for an entry the shell was
// never handed.

// Summary is the counts, taken at one instant.
type Summary struct {
	// AsOf is the instant this body was answered at. It is not a snapshot: the
	// counts are read one question at a time, and an installation may answer one
	// of them from a value a few seconds older than another. What a consumer may
	// say is that no number here is older than AsOf by more than the lifetime the
	// installation gives a count.
	AsOf time.Time `json:"asOf"`
	// Entries is one entry per resource the caller may read, in registration
	// order. An empty list is the truth about a caller who may read nothing, and
	// is not a refusal.
	Entries []SummaryEntry `json:"entries"`
}

// SummaryEntry is one resource's counts. Module and Entity are spelled exactly
// as the catalogue's entry spells them, which is the join: nothing here carries
// an address, a screen or a picture a shell could take from that entry instead.
type SummaryEntry struct {
	Module string `json:"module"`
	Entity string `json:"entity"`
	// Total is every live row this caller's list would page through.
	Total *int64 `json:"total,omitempty"`
	// Highlight is the entry's declared sub-count — the "Due today" beside the
	// total — and is absent for an entry that declares none, for the same reason
	// the words of it are absent there.
	Highlight *int64 `json:"highlight,omitempty"`
	Trend     *Trend `json:"trend,omitempty"`
}

// Trend is one timestamp column broken into days.
type Trend struct {
	// Field echoes the declared name — "createdAt" — so a shell drawing two
	// documents never has to guess which field a series was read from.
	Field string `json:"field"`
	// Days is always TrendDays long, oldest first. A day the query named no rows
	// for is a zero here rather than a hole: a shell that had to tell a quiet day
	// apart from an unanswered one would draw a gap where the answer was "none".
	Days []TrendDay `json:"days"`
}

// TrendDay is one UTC calendar day and how many rows fall inside it.
type TrendDay struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

// Answers is one entry's counts as the installation read them. A nil field is
// "not answered" — a query that refused, a budget that ran out — and is never a
// zero, and never cached.
type Answers struct {
	Total     *int64
	Highlight *int64
	// Days carries only the days the query found. Summarise is what turns a day
	// it did not name into a zero, so the query reads a window and answers it.
	Days []crud.DayCount
}

// TrendDays is how many days a declared trend covers: two weeks of a sparkline,
// which is as far back as a person reads a daily shape and short enough that the
// query is one bounded range and not a scan of a tenant's history.
const TrendDays = 14

// TrendWindow is the half-open range of the TrendDays ending at the UTC day
// asOf falls in: [start of that day − (TrendDays−1), start of the next).
func TrendWindow(asOf time.Time) (from, to time.Time) {
	day := crud.DayStart(asOf)
	return day.Add(-time.Duration(TrendDays-1) * 24 * time.Hour), day.Add(24 * time.Hour)
}

// Summarise lays the counts read for each resource into one document at one
// instant. It reads no database, forms no key and asks no permission: it selects
// the entries the catalogue selects, keeps the answers the installation gave, and
// fills the fourteen buckets of the trend window, because which days exist is a
// fact about the document and not about the rows.
//
// answers is keyed `<module>/<entity>` — the same spelling a copy key uses in the
// middle — and an entry with no answer in it carries no number.
func Summarise(ctx context.Context, resources []httpx.Resource, answers map[string]Answers, asOf time.Time) Summary {
	out := Summary{AsOf: asOf.UTC(), Entries: []SummaryEntry{}}
	from, _ := TrendWindow(asOf)
	for _, r := range resources {
		if r.CommandsOnly || !r.Offers(httpx.CRUDList) || !r.Readable(ctx) {
			continue
		}
		entry := SummaryEntry{Module: r.Module, Entity: r.Entity}
		got := answers[r.Module+"/"+r.Entity]
		entry.Total, entry.Highlight = got.Total, got.Highlight
		if field := r.Present.TrendField; field != "" && got.Days != nil {
			entry.Trend = &Trend{Field: field, Days: days(got.Days, from)}
		}
		out.Entries = append(out.Entries, entry)
	}
	return out
}

// days is the window as fourteen buckets, oldest first, the counted rows dropped
// into the one their instant falls in. A row outside the window is nobody's
// business here: the query bounded it, and a count in a day the document does not
// have would mean the two disagree about where the window is.
func days(counted []crud.DayCount, from time.Time) []TrendDay {
	out := make([]TrendDay, TrendDays)
	for i := range out {
		day := from.Add(time.Duration(i) * 24 * time.Hour)
		out[i] = TrendDay{Day: day.Format("2006-01-02")}
	}
	for _, c := range counted {
		i := int(c.Day.UTC().Sub(from) / (24 * time.Hour))
		if i >= 0 && i < TrendDays {
			out[i].Count += c.Count
		}
	}
	return out
}
