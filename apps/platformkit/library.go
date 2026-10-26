package main

// library.go is this application's share of the one screen modules/file serves: the
// chrome it is drawn in and the authorizer its two write controls ask about. The
// page itself, the grid's read, the meter's sum and the caption's refusal are the
// module's; which shell this product puts its library inside is not anything a byte
// store has an opinion about.

import (
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/ui/page"
)

// media supplies the chrome the media library is drawn in.
//
// It is a module of its own rather than a line in product because of one edge: the
// authorizer that decides whether a person is shown the caption form and the removal
// form is the auth service, and product needs nobody and is therefore built before
// anything that has a service to hand out. It is composed immediately before
// file.Module for the same reason read the other way — the library's page is mounted
// by the module that owns the files, so the chrome has to be on the table by then.
//
// What it hands over is the shell and the authorizer only. The grid, the meter and
// the caption are filled by modules/file from the one service its resolver builds, so
// no composition can put a library in front of a different service than the doors the
// same module mounts.
func media() *pkit.Module {
	return pkit.NewModule("media", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[*file.LibraryChrome](w, &file.LibraryChrome{
			Shell:     libraryShell(catalogues()),
			Authorize: pkit.Get[httpx.Authorizer](w),
		})
		return module.Module{Name: "media"}, nil
	},
		pkit.Needs[httpx.Authorizer](),
		pkit.Provides[*file.LibraryChrome](),
	)
}

// libraryShell is the chrome and the frame the library is read in: the same four
// facts the fault pages and the review queue use, with the module's own tag on its
// operations. A page that invented its own stylesheet or brand would be a second
// shell in one application, which is the drift ui/page exists to prevent.
func libraryShell(messages page.Messages) page.Shell {
	return page.Shell{
		Chrome:    faultChrome(),
		Frame:     faultFrame,
		Tag:       "file",
		Back:      pinnedHome,
		BackLabel: "Back to the workspace",
		Messages:  messages,
	}
}
