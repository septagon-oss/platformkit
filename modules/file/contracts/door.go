package contracts

import (
	"strings"

	"github.com/google/uuid"
)

// door.go names the one address this module answers a stored picture's bytes at,
// so that the composition which publishes an address for a shell to fetch has
// something to publish that it did not retype.
//
// The route itself is owned by internal/handler.go, which mounts it and guards it
// with this module's own read permission: holding a file id is not holding the
// bytes, which is why a record's picture field is an id and every reader of one
// fetches rather than embeds.

// PictureSource is the session-addressed door to a stored picture, as a template:
// the literal `{id}` stands for the file id in its lower-case hyphenated UUID
// text, and nothing else in the string is substituted. The widths the door will
// answer are VariantWidthSet, asked for as `?w=`.
//
// It is the workspace door and only that one. The anonymous door
// (`/api/v1/public/file/files/{id}`) answers a different question — what a
// visitor may see — from a different surface, and is published by the OpenAPI
// document of the surface that mounts it rather than by a record's catalogue.
const PictureSource = "/api/v1/file/files/{id}/content"

// PictureURL is PictureSource with one id written into it, so that the address in
// a body's `<img src>` and the address in a catalogue are the same string rather
// than two spellings of it.
func PictureURL(id uuid.UUID) string {
	return strings.Replace(PictureSource, "{id}", id.String(), 1)
}
