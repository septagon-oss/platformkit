package file

import (
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
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
	pkit.Provides[filecontracts.Service](),
	pkit.Provides[richtext.Files](),
	pkit.Provides[rest.FileUses](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	settings := pkit.Config(w, func(c config.Config) config.Files { return c.Files })
	svc, manifest := New(Deps{
		Storage:        pkit.Get[filecontracts.Storage](w),
		MaxBytes:       settings.MaxBytes,
		QuotaBytes:     settings.QuotaBytes,
		MaxImagePixels: settings.MaxImagePixels,
		Retention:      settings.Retention,
		Tenants:        pkit.Get[jobs.TenantLister](w),
	})
	pkit.Put(w, svc)
	pkit.Put[richtext.Files](w, RichTextFiles{Opener: svc})
	pkit.Put[rest.FileUses](w, RecordUses{Service: svc})
	return manifest, nil
}
