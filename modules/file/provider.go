package file

import (
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the byte store as the resolver sees it, for an application that
// names it in Use: `Use(file.Module)`.
//
// Two contracts come out of this module. Service is the whole of it — a module
// that has to open, list or upload a file takes that; filecontracts.Opener is the
// narrow half for a consumer that only reads one. richtext.Files is the reason
// content and web are built after file rather than handed an opener across a
// composition file: rich text resolves the images a body embeds, and the module
// that can resolve them says so, so withdrawing it is a refusal naming both.
//
// A fourth comes out beside it: rest.FileShown, the audience of those uses, which
// is the fact a public face asserts and this module turns into visibility.
//
// A third contract comes out: rest.FileUses is the ledger a mounted richtext
// resource writes its references into, and the module that owns the file rows is
// the one that can write them, in its caller's transaction.
//
// What it cannot decide is where bytes go, which is the deployment's and arrives
// as a filecontracts.Storage this product provides (file.Local for a disk, file.S3
// for a store), and how large and how long an upload is, which kit/config says.
var Module = pkit.NewModule("file", wire,
	pkit.Needs[filecontracts.Storage](),
	pkit.Needs[jobs.TenantLister](),
	pkit.Optional[*LibraryChrome](),
	pkit.Provides[filecontracts.Service](),
	pkit.Provides[richtext.Files](),
	pkit.Provides[rest.FileUses](),
	pkit.Contributes[personaldata.Part](),
	pkit.Provides[rest.FileShown](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	settings := pkit.Config(w, func(c config.Config) config.Files { return c.Files })
	svc, manifest, err := NewChecked(Deps{
		Storage:        pkit.Get[filecontracts.Storage](w),
		MaxBytes:       settings.MaxBytes,
		QuotaBytes:     settings.QuotaBytes,
		MaxImagePixels: settings.MaxImagePixels,
		Retention:      settings.Retention,
		Tenants:        pkit.Get[jobs.TenantLister](w),
		// The library's page arrives from the composition or does not. Nothing here
		// guesses at which chrome its grid sits in, and the reads it draws are the
		// service below rather than anything a composition could hand in.
		Library: pkit.Get[*LibraryChrome](w),
	})

	if err != nil {
		return module.Module{}, err
	}
	pkit.Put(w, svc)
	// The image port reads the widths this file can serve today, and the service
	// is what answers that. The assertion is a narrowing and not a discovery: the
	// concrete service this function just built has the method, and a composition
	// handed a Service that cannot answer it gets today's one-descriptor srcset
	// rather than a compile-time demand that every fake grow a pixel pipeline.
	widths, _ := svc.(filecontracts.VariantServable)
	pkit.Put[richtext.Files](w, RichTextFiles{Opener: svc, Variants: widths})
	pkit.Put[rest.FileUses](w, RecordUses{Service: svc})
	pkit.Put[personaldata.Part](w, internal.PersonalData())
	pkit.Put[rest.FileShown](w, ShowFiles{Service: svc})
	return manifest, nil
}
