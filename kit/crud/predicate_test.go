package crud_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// dueToday is the declaration modules/task makes about its own entity, spelled the
// way its author spells it: two predicates, two clock words, no date.
var dueToday = []entity.EntryPredicate{
	{Field: "dueAt", Op: "gte", Value: "today"},
	{Field: "dueAt", Op: "lt", Value: "tomorrow"},
}

// predicate_test.go asks the two narrowed reads the questions the card asks with:
// does a window count exactly the rows in the window, does a day break on the UTC
// midnight rather than wherever the server thinks it is, and does a name the
// schema does not know refuse instead of answering with everything.

// quoteLiteral is the zone name as SQL: one literal, with the doubling a name
// carrying a quote would need. The zone is a constant in this file and the
// doubling is written so a name that ever came from elsewhere could not smuggle
// a statement into it.
func quoteLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// open is a task whose only interesting fact is the instant it is due.
func open(title string, due time.Time) *Task {
	at := due
	return &Task{Base: crud.Base{ID: uuid.New()}, Title: title, DueAt: &at}
}

// dayStart is the instant that opens the UTC day the time falls in.
func dayStart(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// TestADayWindowCountsWhatItsBoundsAdmitAndNothingElse counts one tenant's tasks
// whose due instants sit on yesterday, today, tomorrow and next week. The window
// is today: the half-open pair `>= today AND < tomorrow` is what the task module
// declares, and a total that came back as 4 would be the card saying "Due today"
// over the whole list.
func TestADayWindowCountsWhatItsPredicatesAdmitAndNothingElse(t *testing.T) {
	conn := setup(t)
	day := dayStart(time.Now().UTC())
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, e := range []*Task{
			open("yesterday", day.Add(-20*time.Hour)),
			open("today", day.Add(9*time.Hour)),
			open("today-late", day.Add(23*time.Hour)),
			open("tomorrow", day.Add(26*time.Hour)),
			open("next week", day.Add(9*24*time.Hour)),
			{Base: crud.Base{ID: uuid.New()}, Title: "no due date at all"},
		} {
			e.ID = uuid.New()
			if err := crud.Create[*Task](ctx, tx, e); err != nil {
				t.Fatalf("create %s: %v", e.Title, err)
			}
		}
		// One of the six carries no due instant at all: a live row whose window
		// predicate is NULL is neither in nor out of the window, and is out.
		total, err := crud.Count[*Task](tx)
		if err != nil {
			t.Fatalf("count everything: %v", err)
		}
		if total != 6 {
			t.Fatalf("six tasks answer a total of %d", total)
		}
		where := crud.Where{
			{Field: "dueAt", Op: crud.OpGte, Value: day},
			{Field: "dueAt", Op: crud.OpLt, Value: day.Add(24 * time.Hour)},
		}
		got, err := crud.CountWhere[*Task](tx, where)
		if err != nil {
			t.Fatalf("count the window: %v", err)
		}
		if got != 2 {
			t.Errorf("the window holds %d of six tasks, want the two due inside it", got)
		}
	})
}

// TestAWindowedCountSeesNoOtherTenant'sRows is the same question at two hosts: the
// narrowing is a predicate and never a tenant, and row-level security is what
// keeps one customer's card free of another's rows.
func TestAWindowedCountSeesNoOtherTenant(t *testing.T) {
	conn := setup(t)
	day := dayStart(time.Now().UTC())
	where := crud.Where{{Field: "dueAt", Op: crud.OpGte, Value: day}, {Field: "dueAt", Op: crud.OpLt, Value: day.Add(24 * time.Hour)}}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.Create[*Task](ctx, tx, open("acme today", day.Add(8*time.Hour))); err != nil {
			t.Fatalf("seed acme: %v", err)
		}
	})
	as(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for i := range 3 {
			e := open(fmt.Sprintf("globex today %d", i), day.Add(time.Duration(8+i)*time.Hour))
			e.ID = uuid.New()
			if err := crud.Create[*Task](ctx, tx, e); err != nil {
				t.Fatalf("seed globex: %v", err)
			}
		}
		total, err := crud.Count[*Task](tx)
		if err != nil {
			t.Fatalf("count globex: %v", err)
		}
		if total != 3 {
			t.Errorf("globex counts %d rows and sees acme's, want its own 3", total)
		}
		if _, err := crud.CountWhere[*Task](tx, where); err != nil {
			t.Errorf("the windowed count at globex: %v", err)
		}
	})
}

// TestADayBucketIsAUTCDayUnderAnySessionTimeZone is the exact bug in the exact
// sentence: date_trunc over a timestamptz truncates in the session's zone, and
// nothing in kit/db sets one, so a server configured to America/Sao_Paulo would
// move every bucket by three hours and a person's quiet Tuesday would arrive on
// Wednesday. The window is counted twice, once with the session moved, and the
// fourteen numbers must not move.
func TestADayBucketIsAUTCDayUnderAnySessionTimeZone(t *testing.T) {
	conn := setup(t)
	day := dayStart(time.Now().UTC())
	// One row late in the UTC day — 22:00, which is 19:00 in São Paulo and so
	// belongs to the same calendar day there, and one early — 01:00, which is
	// 22:00 of the day before there and would fall into the wrong bucket.
	mornings := []time.Time{day.Add(1 * time.Hour), day.Add(22 * time.Hour)}
	days := func(t *testing.T, zone string) map[string]int64 {
		t.Helper()
		out := map[string]int64{}
		from := day.Add(-13 * 24 * time.Hour)
		as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
			if zone != "" {
				// A literal, because Postgres takes no parameter for a zone name.
				if err := tx.DB().Exec("SET TIME ZONE " + quoteLiteral(zone)).Error; err != nil {
					t.Fatalf("move the session to %s: %v", zone, err)
				}
			}
			counted, err := crud.CountDays[*Task](tx, "dueAt", from, day.Add(24*time.Hour))
			if err != nil {
				t.Fatalf("break the window into days: %v", err)
			}
			for _, d := range counted {
				out[d.Day.Format(time.RFC3339)] = d.Count
			}
		})
		return out
	}
	for _, instant := range mornings {
		e := open("due at "+instant.Format(time.RFC3339), instant)
		e.ID = uuid.New()
		as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
			if err := crud.Create[*Task](ctx, tx, e); err != nil {
				t.Fatalf("seed: %v", err)
			}
		})
	}
	mine := days(t, "")
	moved := days(t, "America/Sao_Paulo")
	if len(mine) != 1 {
		t.Fatalf("two rows due in one UTC day answer %d named days, want the one day they both fall in", len(mine))
	}
	for day, count := range mine {
		if moved[day] != count {
			t.Errorf("a session moved to São Paulo answers %d rows on %s, want %d: the buckets moved with the session", moved[day], day, count)
		}
	}
	if _, named := mine[day.Format(time.RFC3339)]; !named {
		t.Errorf("neither bucket is today's UTC midnight (%s); the buckets answer %v", day.Format(time.RFC3339), mine)
	}
}

// TestADayBreakRefusesAFieldThatIsNoDay and TestAPredicateNamingNoFieldAreRefused
// keep the two ways a declaration can name nothing as refusals rather than as the
// whole table: an unfiltered total under a label is the wrong number a card could
// only ever repeat.
func TestADayBreakRefusesAFieldThatIsNoDay(t *testing.T) {
	conn := setup(t)
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if _, err := crud.CountDays[*Task](tx, "title", time.Time{}, time.Time{}); err == nil {
			t.Fatal("a day breakdown of a title answered a number")
		}
	})
}

func TestAPredicateNamingNoFieldIsRefusedAndNotIgnored(t *testing.T) {
	conn := setup(t)
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, w := range []crud.Where{
			{{Field: "notAColumn", Op: crud.OpEq, Value: "x"}},
			{{Field: "tags", Op: crud.OpEq, Value: "x"}},
			{{Field: "status", Op: crud.Op("contains"), Value: "x"}},
		} {
			if _, err := crud.CountWhere[*Task](tx, w); err == nil {
				t.Errorf("%+v answered a count rather than a refusal", w)
			}
		}
	})
}

// TestAResolvedDeclarationCountsTodaysRows reads the task module's own declaration
// — two predicates naming "today" and "tomorrow" — resolved at one instant and
// asked of the table. A frozen date in the Go source is the bug this shape exists
// to avoid, so the case resolves at two different instants and the counts move.
func TestAResolvedDeclarationCountsTodaysRows(t *testing.T) {
	conn := setup(t)
	if _, ok := crud.FieldNamed(crud.Fields[*Task](), "dueAt"); !ok {
		t.Fatalf("the fixture entity has no dueAt field to declare a window with")
	}
	day := dayStart(time.Now().UTC())
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.Create[*Task](ctx, tx, open("due today", day.Add(6*time.Hour))); err != nil {
			t.Fatalf("seed: %v", err)
		}
	})
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		where, err := crud.Resolve(crud.Fields[*Task](), dueToday, day.Add(12*time.Hour))
		if err != nil {
			t.Fatalf("resolve the declaration at noon: %v", err)
		}
		got, err := crud.CountWhere[*Task](tx, where)
		if err != nil {
			t.Fatalf("ask the resolved window: %v", err)
		}
		if got != 1 {
			t.Errorf("the declared window at noon today counts %d, want the one task due in it", got)
		}
		// The same two predicates, resolved one day later, count nothing: the
		// words mean the responder's day and not the day the binary was built.
		where, err = crud.Resolve(crud.Fields[*Task](), dueToday, day.Add(36*time.Hour))
		if err != nil {
			t.Fatalf("resolve the declaration tomorrow: %v", err)
		}
		got, err = crud.CountWhere[*Task](tx, where)
		if err != nil {
			t.Fatalf("ask tomorrow's window: %v", err)
		}
		if got != 0 {
			t.Errorf("the declared window tomorrow counts %d, want the rows due in *that* day, which is none", got)
		}
	})
}
