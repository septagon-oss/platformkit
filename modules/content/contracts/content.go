// Package contracts is everything another module, an app or a test may know
// about content: the entity, the events, the permissions and the Service
// interface. The implementation is in ../internal.
//
// Content is a page or a post: a slug, a title, a body in Markdown, and a
// status that decides whether the public site serves it. There are no
// versions, no categories and no translations. Versions in particular are a
// module of their own — a history table, a diff, a restore and a retention
// rule — and the private catalogue has one; what a reference architecture owes
// is the lifecycle, which is draft, published, archived.
package contracts

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The two kinds. A page is addressed by its slug and stands on its own; a post
// is dated and belongs in a list. Nothing else distinguishes them, which is why
// this is a field and not two entities.
const (
	KindPage = "page"
	KindPost = "post"
)

// The lifecycle: a draft is being written, a published one is served to
// anybody, an archived one is kept and served to nobody.
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusArchived  = "archived"
)

// The two closed sets, spelled here as well as in the enum tags below: a tag is
// what the schema reads and this is what Validate reads.
var (
	kinds    = []string{KindPage, KindPost}
	statuses = []string{StatusDraft, StatusPublished, StatusArchived}
)

// The bounds. MaxSlug and MaxTitle are the columns' widths; MaxBody is a quarter
// of a megabyte, which is roughly forty thousand words — longer than anything
// anybody writes in one page and short enough that the public route can render
// it on every anonymous request without becoming the way to take the site down.
// The review measured 2.25 seconds for one 8.5 MB body.
const (
	MaxSlug  = 200
	MaxTitle = 200
	MaxBody  = 262144
)

// fold is the transliteration Slugify does before it drops what is left. It is
// the letters that carry no combining mark to strip — norm.NFD gets the accents,
// and these are the ones decomposition does not touch — so that a German, Danish,
// Polish or Turkish title becomes a name a URL can carry instead of becoming
// nothing at all. "Über uns" used to slugify to "ber-uns"; "Ærø" to nothing.
var fold = strings.NewReplacer(
	"ß", "ss", "æ", "ae", "ø", "o", "å", "a", "œ", "oe",
	"đ", "d", "ð", "d", "þ", "th", "ł", "l", "ı", "i", "ħ", "h", "ŋ", "n", "ĸ", "k",
)

// Slugify is the one definition of a slug: lower case, words joined by single
// dashes, nothing outside a-z, 0-9 and the dash. It exists as a function
// because the write path and the public read path have to agree — a slug
// normalised on the way in and looked up verbatim is a page nobody can reach.
func Slugify(s string) string {
	// Decompose first, so an accented letter becomes its base letter and a
	// combining mark, and then drop the marks: é is e, ñ is n, ā is a. The fold
	// above catches the letters decomposition leaves alone. Without both, every
	// language but English produced a slug with holes in it — and a title of
	// nothing but non-ASCII letters produced no slug at all, which is a 422 an
	// author cannot act on.
	var b strings.Builder
	last := byte('-')
	for _, r := range norm.NFD.String(fold.Replace(strings.ToLower(strings.TrimSpace(s)))) {
		var c byte
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			c = byte(r)
		case r == '-' || r == ' ' || r == '_' || r == '/' || r == '.':
			c = '-'
		case unicode.Is(unicode.Mn, r):
			continue // a combining mark, whose base letter has already been written
		default:
			continue // anything else is not part of a name a URL can carry
		}
		if c == '-' && last == '-' {
			continue // one dash, however many separators were written
		}
		b.WriteByte(c)
		last = c
		if b.Len() == MaxSlug {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// Content is one page or post in one tenant.
//
// The struct is the whole surface: the json tags are the API, the gorm tags are
// the table, the enum and validate tags are the schema a generated screen
// reads, and crud.Base contributes the id, the timestamps, the soft delete and
// the tenant column row-level security matches on.
type Content struct {
	crud.Base

	// Slug is the name this content is reached by, unique within the tenant. It
	// is normalised on every write, so two callers cannot disagree about
	// whether "About Us" and "about-us" are the same page.
	Slug string `json:"slug" gorm:"type:varchar(200);not null" validate:"required" minLength:"1" maxLength:"200" doc:"URL name, unique within the tenant" example:"about-us"`
	// Title is what a list and a heading show.
	Title string `json:"title" gorm:"type:varchar(200);not null" validate:"required" minLength:"1" maxLength:"200" doc:"Headline" example:"About us"`
	// Body is Markdown. It is stored as it was written and rendered on read, so
	// a change to what the renderer allows applies to everything ever written
	// rather than to whatever happens to be saved next.
	Body string `json:"body,omitempty" gorm:"type:text;not null;default:''" maxLength:"262144" ui:"widget:textarea;hide:list" doc:"The content itself, in Markdown"`

	// Kind and Status are closed sets; the enum tag is what a form renders as a
	// select and what Validate refuses a value outside.
	Kind   string `json:"kind" gorm:"type:varchar(10);not null;default:'post'" enum:"page,post" ui:"widget:select" doc:"page or post" default:"post" required:"false"`
	Status string `json:"status" gorm:"type:varchar(10);not null;default:'draft'" enum:"draft,published,archived" ui:"widget:select" doc:"Lifecycle state" default:"draft" required:"false"`

	// PublishedAt is when it was published, and nil whenever it is not
	// published: the two are one fact, and Validate keeps them together.
	PublishedAt *time.Time `json:"publishedAt,omitempty" gorm:"type:timestamptz" ui:"widget:datetime" doc:"When this was published" readOnly:"true"`

	// AuthorID is whoever created it. Validate stamps it from the caller on the
	// context, so it is the actor of the request that wrote the row and not
	// something a body can claim; the Spec names it Immutable, so no patch
	// rewrites a byline.
	AuthorID uuid.UUID `json:"author,omitempty" gorm:"column:author_id;type:uuid" format:"uuid" ui:"hide:list" doc:"The user who created this" readOnly:"true"`
}

// TableName pins the table, so the entity and migrations/000017 agree.
func (Content) TableName() string { return "contents" }

// Validate is the entity's own check, run by kit/crud on every write whichever
// door it came through. It normalises as well as refuses, and it stamps the
// author: the context is the one thing that knows whose request this is, which
// is the same reason kit/events reads the actor off it.
func (c *Content) Validate(ctx context.Context) error {
	c.Slug = Slugify(c.Slug)
	c.Title = strings.TrimSpace(c.Title)
	if c.Kind == "" {
		c.Kind = KindPost
	}
	if c.Status == "" {
		c.Status = StatusDraft
	}
	if c.AuthorID == uuid.Nil {
		if actor, ok := tenancy.ActorFrom(ctx); ok {
			c.AuthorID = actor
		}
	}
	switch {
	case c.Slug == "":
		return fmt.Errorf("a page needs a slug, and %q leaves nothing a URL can carry", c.Slug)
	case c.Title == "":
		return fmt.Errorf("a page needs a title")
	// Characters, not bytes: len() counts bytes, so a title of 80 Chinese
	// characters used to be refused as 240 and a 200-character column was
	// handed 200 characters that did not fit.
	case utf8.RuneCountInString(c.Title) > MaxTitle:
		return fmt.Errorf("a title is at most %d characters", MaxTitle)
	// Bytes here, and deliberately: this one is a bound on what is stored and
	// on what the public route renders per anonymous request, and both are
	// measured in bytes. The column carries the same check.
	case len(c.Body) > MaxBody:
		return fmt.Errorf("a body is at most %d bytes, and this one is %d; a document longer than that is a file and not a page", MaxBody, len(c.Body))
	case !slices.Contains(kinds, c.Kind):
		return fmt.Errorf("kind %q is not %s or %s", c.Kind, KindPage, KindPost)
	case !slices.Contains(statuses, c.Status):
		return fmt.Errorf("status %q is not a lifecycle state", c.Status)
	// Both halves of "published means published at a time", so anything that is
	// served can say when it started being served and nothing else pretends to.
	case c.Status == StatusPublished && c.PublishedAt == nil:
		return fmt.Errorf("published content needs a publication time")
	case c.Status != StatusPublished && c.PublishedAt != nil:
		return fmt.Errorf("%s content cannot have a publication time", c.Status)
	}
	return nil
}

// Service is the content lifecycle: the three transitions generic CRUD cannot
// safely infer, and the one read the public site makes.
//
// Every command takes the caller's transaction rather than opening one, so the
// state change and its event commit together. The errors are kit/crud's:
// ErrNotFound for content this tenant does not have, ErrConflict for a state
// that refuses the command. Each command is idempotent: a browser that retries,
// or a redelivered event, must not produce a second announcement.
type Service interface {
	// Publish serves it to anybody, and records when. Publishing what is
	// already published changes nothing — the publication time does not move,
	// because a page has one — and archived content is refused: it is taken out
	// of the archive first, which is what Unpublish does.
	Publish(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Content, error)

	// Unpublish takes it back to a draft, from published or from archived, and
	// clears the publication time with it. A draft again changes nothing.
	Unpublish(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Content, error)

	// Archive keeps it and serves it to nobody. Archiving twice changes
	// nothing.
	Archive(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Content, error)

	// Public is the published content at this slug, which is what the public
	// site serves. Anything else — a draft, an archived page, a slug nobody has
	// used — is ErrNotFound, because from outside, content that is not served
	// and content that does not exist are the same fact.
	Public(ctx context.Context, tx db.Tx[db.Tenant], slug string) (*Content, error)
}
