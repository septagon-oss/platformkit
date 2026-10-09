package translationtest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// StubSource is the entity the suite pretends exists: one mounted Spec's two
// names, the translatable fields it declares, which of them are richtext, and
// the live rows it holds.
//
// It is a test double of rest.TranslationSourceOf, and it has to be one: the
// question "which records have no Portuguese yet" is answerable only from the
// entity's own row set, so a suite that gave the fake no row set could not ask
// it — and C18, the case that says so, would be checking an empty map instead
// of the port.
type StubSource struct {
	Module_   string
	Entity_   string
	FieldList []string
	Rich      map[string]bool
	// Rules_ is each field's own ceiling and closed set — the same two facts a
	// mounted Spec derives from its schema tags, and the reason a machine draft
	// can be refused over them here exactly as it is refused over them there.
	Rules_ map[string]rest.FieldRule
	// Rows_ is the entity's live rows, keyed by id. A row absent from it is a
	// row that does not exist, which is how a deleted source says so.
	Rows_ map[uuid.UUID]rest.SourceRow
}

// The title length the stub entity declares, spelled once: long enough that
// every case but the one about the ceiling fits under it, short enough that a
// provider's over-long answer does not.
const stubTitleChars = 40

// NewStubSource is the entity the suite runs against unless a case says
// otherwise: a page with a plain title and a richtext body.
func NewStubSource() *StubSource {
	return &StubSource{
		Module_: "pages", Entity_: "page",
		FieldList: []string{"body", "title"},
		Rich:      map[string]bool{"body": true, "title": false},
		Rules_: map[string]rest.FieldRule{
			"body":  {Name: "body"},
			"title": {Name: "title", MaxChars: stubTitleChars},
		},
		Rows_: map[uuid.UUID]rest.SourceRow{},
	}
}

func (s *StubSource) Module() string            { return s.Module_ }
func (s *StubSource) Entity() string            { return s.Entity_ }
func (s *StubSource) Fields() []string          { return slices.Clone(s.FieldList) }
func (s *StubSource) RichText() map[string]bool { return s.Rich }
func (s *StubSource) Rules() map[string]rest.FieldRule {
	return maps.Clone(s.Rules_)
}

// Put records one live row of the entity with the source text it now holds.
func (s *StubSource) Put(id uuid.UUID, updatedAt time.Time, values map[string]string) {
	if s.Rows_ == nil {
		s.Rows_ = map[uuid.UUID]rest.SourceRow{}
	}
	s.Rows_[id] = rest.SourceRow{ID: id, UpdatedAt: updatedAt, Values: values}
}

func (s *StubSource) Rows(_ context.Context, _ db.Tx[db.Tenant], ids []uuid.UUID) ([]rest.SourceRow, error) {
	var out []rest.SourceRow
	for _, id := range ids {
		if row, ok := s.Rows_[id]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *StubSource) Page(_ context.Context, _ db.Tx[db.Tenant], limit, offset int) ([]rest.SourceRow, int64, error) {
	ids := make([]string, 0, len(s.Rows_))
	for id := range s.Rows_ {
		ids = append(ids, id.String())
	}
	sort.Strings(ids)
	if offset > len(ids) {
		offset = len(ids)
	}
	page := ids[offset:]
	if limit > 0 && limit < len(page) {
		page = page[:limit]
	}
	out := make([]rest.SourceRow, 0, len(page))
	for _, raw := range page {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, 0, fmt.Errorf("stub: %w", err)
		}
		out = append(out, s.Rows_[id])
	}
	return out, int64(len(ids)), nil
}

var _ rest.TranslationSource = (*StubSource)(nil)

// Machine is the case's machine translator, and it is a switch rather than a
// value because the suite has to ask the same service three different things:
// what happens with no provider, with one that answers, and with one that hands
// back the text it was given.
//
// It is one implementation shared by the fake and the real service, which is the
// only way the two can be run against the same case and mean the same thing.
type Machine struct {
	// Answer is what the provider says. nil is no provider configured, which
	// answers locale.ErrNoProvider and gets the 422 rather than the 503.
	Answer func(ctx context.Context, text, from, to string) (string, error)
}

func (m *Machine) Translate(ctx context.Context, text, from, to string) (string, error) {
	if m == nil || m.Answer == nil {
		return "", locale.ErrNoProvider
	}
	return m.Answer(ctx, text, from, to)
}

var _ locale.Translator = (*Machine)(nil)

// The entity every case runs against, spelled once.
const (
	TestModule = "pages"
	TestEntity = "page"
	FieldBody  = "body"
	FieldTitle = "title"
	// PT is the second language, spelled the way a tenant spells it: the
	// declared tag, not a folded language.
	PT = "pt-PT"
	// EN is what every source row is written in.
	EN = "en"
)

// Source is one record's source text, as the stub entity's two fields: a plain
// title and a richtext body. The body takes two paragraphs so a case that asks
// what changed has something to point at, and an empty second argument leaves
// one paragraph.
func Source(title, first, second string) map[string]string {
	body := first
	if second != "" {
		body += "\n\n" + second
	}
	return mapOf(title, body)
}

func mapOf(title, body string) map[string]string {
	return map[string]string{"title": title, "body": body}
}

// expected is an Expected map spelled from the revisions just read.
func expected(body, title int64) map[string]int64 {
	return map[string]int64{"body": body, "title": title}
}

// rich is which fields of the stub entity are richtext.
func rich() map[string]bool { return map[string]bool{"body": true, "title": false} }
