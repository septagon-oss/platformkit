// Package content is the module manifest: the pages and posts a tenant's public
// site is made of.
//
// It is the shape every module follows: contracts/, internal/, and one exported
// function taking a struct of typed dependencies and returning a module.Module.
// Two absences are decisions rather than omissions.
//
// No version is stored here. A history table, a diff, a restore and a retention
// rule are a module of their own — modules/history — and a page's writes record
// through the httpx.Versions port that module provides, numbered with this
// entity's own write count. What content owns is the lifecycle: draft, published,
// archived, and the events that announce it.
//
// There is no stored HTML. The body is Markdown and the public route renders it
// on read, through goldmark with raw HTML left out and bluemonday over the
// result, so a policy that tightens applies to everything ever written instead
// of to whatever is saved next. See internal/render.go.
package content

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/internal"
)

// Deps is what this module cannot make for itself. It is a struct rather than
// no parameter so that the day it needs something, every call site gains a
// named field instead of a new argument.
type Deps struct {
	Files richtext.Files
	// Uses records which file a body of this resource references, in the
	// transaction that writes the body. Unwired, nothing is recorded and mount
	// says so in the log: rest.RecordNoUses is that answer, and it is safe only
	// for as long as no sweep releases a file nobody reads.
	Uses rest.FileUses
	// Versions is where a page's committed writes are kept, in the transaction
	// that wrote them. Content turns history on and does not own the keeping:
	// modules/history answers this port, and this module declares the need, so a
	// composition with no version store is refused where the plan is checked —
	// before anything opens — rather than serving pages whose writes silently keep
	// no version. Called by hand with the field left empty, mount says the same
	// thing in its own refusal. The files a stored body shows are filed under the
	// version that shows them, so a retained version holds its pictures and no
	// longer through the same ledger the body files them under.
	Versions httpx.Versions
	// Translations is the record's door onto another module's table: the title
	// and the body declare themselves translatable, and a mount that says so
	// without a port to answer `?lang=` is refused by the Spec's own check. The
	// composition writes this one line; the translation module is told about
	// content by TranslationSource below, and by nothing else.
	Translations rest.Translations
	// Shown asserts, for a record this module publishes, that the files its body
	// shows are now showing to anonymous readers — or that they stopped. Unwired,
	// nothing is asserted: an image keeps the visibility its upload asked for, which
	// for an upload that asked for nothing is private, so a published page shows a
	// broken picture rather than leaking bytes.
	Shown rest.FileShown
}

// TranslationSource is the content entity's row set as the translation module
// has to be handed it: the module may not reach into this table, so the
// composition carries the pair — content names the port it writes to, and the
// translation module names the rows it may be given. It asks nothing of the
// mounted Spec but its two names and its entity, which is why a mounted Spec
// and this are the same answer.
func TranslationSource() rest.TranslationSource {
	return rest.TranslationSourceOf(rest.Spec[*contracts.Content]{Module: "content", Entity: "content"})
}

// Spec is the entity's presence in the application: five routes, two
// permissions, three events and the schema a generated screen reads. It is
// exported because the write it describes is the one write path content has: an
// application that seeds a page, or writes one from a process that is not a
// person's request, goes through it rather than through a copy of what it does.
var Spec = rest.Spec[*contracts.Content]{
	Module:     "content",
	Entity:     "content",
	Path:       "/contents",
	Read:       contracts.PermissionContentRead,
	Write:      contracts.PermissionContentManage,
	SoftDelete: true,
	// The three fields a route of its own owns. status and publishedAt belong
	// to publish, unpublish and archive, which move them together and announce
	// it; author belongs to the create that stamped it from the caller, and a
	// patch that could rewrite it would forge a byline.
	Immutable: []string{"status", "publishedAt", "author"},
	// Audience is the one fact a body cannot answer and the one content owns: a page
	// shows its pictures to anybody exactly when it is published. It reads the row
	// this transaction already locked and merged, so it costs no query, and status is
	// immutable just above, so a merged row cannot be claiming a lifecycle this write
	// moved. A create arrives with the default status, which files a draft's uses.
	Audience: func(_ context.Context, _ db.Tx[db.Tenant], c *contracts.Content) (bool, error) {
		return c.Status == contracts.StatusPublished, nil
	},
	// Nothing here publishes from a hook, so there is nothing for a Spec to
	// declare on the write routes' behalf. The lifecycle's three events are
	// declared by the three commands that publish them, in internal/handler.go.
	HookEvents: nil,
	// Present is how a page reads to the person who maintains it. `previewField`
	// is written nowhere here: the body is Markdown source, and a row previewing
	// raw Markdown is the fault decision 0085 photographs.
	Present: entity.EntryHints{
		Description:      "Write, review and publish the pages your audience sees.",
		EmptyDescription: "Create a page and save a draft before publishing.",
		Singular:         "page",
		Plural:           "pages",
		Icon:             "document",
		Group:            &entity.ResourceGroup{Key: "content", Label: "Content"},
		PrimaryField:     "title",
		StatusField:      "status",
		// The one picture this entity holds, and so both the record's opening
		// photo and the row's thumbnail: `thumbnailField` is said anyway, because
		// a document that echoes what a shell would have computed still answers
		// "did somebody choose this?" and a shell that reads only the one key it
		// was told to read has a thumbnail either way.
		CoverField:     "coverImageId",
		ThumbnailField: "coverImageId",
		SummaryFields:  []string{"kind"},
		Sortable:       []string{"title", "publishedAt", "updatedAt"},
		Deck: &entity.DeckHints{
			Queue: []string{"status:draft"}, Order: "updatedAt",
			Directions: map[string]entity.DeckDirection{
				"right": {Command: "publish", Label: "Publish", Tone: "success", Icon: "document"},
				"left":  {Command: "archive", Label: "Archive", Tone: "warning", Icon: "folder"},
				"down":  {Skip: true},
			},
		},
	},
}

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is
// not here fails startup instead of denying everyone forever.
var permissions = []module.Permission{
	{Key: contracts.PermissionContentRead, Label: "read content"},
	{Key: contracts.PermissionContentManage, Label: "manage content"},
}

// Module is the manifest. The implementation is constructed here, in one line,
// and handed to the one place that uses it.
//
// Deprecated: use NewChecked, which answers this refusal as an error.
func New(deps Deps) (contracts.Service, module.Module) {
	svc, manifest, err := NewChecked(deps)
	if err != nil {
		panic(err.Error())
	}
	return svc, manifest
}

// NewChecked builds the service and the manifest. A nil Files refuses an inline
// image rather than storing it unaccounted, a nil Uses records no use and says
// so at mount, and nothing else is refused, so the error is nil for every Deps
// today. It opens nothing, runs no query and is safe to call twice.
func NewChecked(deps Deps) (contracts.Service, module.Module, error) {
	files := deps.Files
	if files == nil {
		files = richtext.RejectImages{}
	}
	uses := deps.Uses
	if uses == nil {
		uses = rest.RecordNoUses{}
	}
	resource := Spec
	resource.RichTextFiles = files
	resource.FileUses = uses
	// A page keeps a version of every write this Spec commits, inside the write.
	// The store, the retention rule and the restore's event belong to
	// modules/history; what is declared here is that a page's writes are worth
	// keeping, which is the one decision content makes about history.
	resource.History = true
	resource.Versions = deps.Versions
	resource.Translations = deps.Translations
	svc := internal.NewService(internal.Deps{Shown: deps.Shown})
	return svc, module.Module{
		Name:       "content",
		Migrations: Migrations.Files,
		// `author_id` carries no foreign key on purpose, so the claim that it names a
		// person is the module's to make.
		PersonalData: personaldata.Implements(personaldata.Table{
			Name:   "contents",
			Person: "author_id",
			Text:   []string{"title", "body"},
			// A cover is a row of the file ledger, not a person: the erasure that
			// anonymises a page leaves the picture alone and lets the file owner's
			// own contributor deal with the picture it points at.
			NotPerson: []string{"cover_image_id"},
		}),
		Adopts:      Migrations.Adopts,
		Permissions: permissions,
		Declared:    contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Content", Screen: "content/contents", Permission: contracts.PermissionContentRead},
		},
		// No periodic work: nothing about a page happens because time passed.
		// No subscriptions: this module has no opinion about anybody else's
		// events, and the site that reads it takes the read route.
		Jobs:          nil,
		Subscriptions: nil,
		Routes: func(s httpx.Surfaces) {
			resource.Mount(s)
			internal.RegisterRoutes(s, resource, svc)
		},
	}, nil
}
