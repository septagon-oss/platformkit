package rest

import (
	"context"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// FileShown is the audience half of FileUses: the same rows, and the one fact a
// use row cannot derive on its own, namely whether the record that shows this file
// is showing it to somebody who has not signed in.
//
// kit/rest declares it for the same reason it declares FileUses — the kit may not
// know what a file is, and the composition wires the one implementation the
// application has. It is a second port and not a method on FileUses because the
// two have different callers: a body write files uses and knows nothing about
// anybody's lifecycle, and only the command that moves a record in and out of the
// public face knows which audience it is asserting for.
type FileShown interface {
	SetShown(ctx context.Context, tx db.Tx[db.Tenant], in ShownInput) error
}

// ShownInput names one record and the files it is showing. Field and Locale play
// no part: a record's public face covers every field and every locale of it, which
// is what makes the audience one fact per (record, file) and not one per body.
type ShownInput struct {
	Module, Entity string
	Record         uuid.UUID
	Files          []uuid.UUID
	Public         bool
}

// ShowNothing is the port a module with a public face gets when nobody wired it to
// a file module: it accepts every publish and flips nothing, which is what an
// application with no visibility rule costs nobody — a published page keeps an
// image its owner uploaded and left private, so the page shows a broken picture
// rather than leaking bytes. A named type and not a nil check, so the day an
// installation wants its published images readable is a decision to search for.
type ShowNothing struct{}

func (ShowNothing) SetShown(ctx context.Context, tx db.Tx[db.Tenant], in ShownInput) error {
	return nil
}
