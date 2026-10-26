// Package internal holds saved's deterministic contract decisions. Persistence,
// source calls and transaction ownership belong to the service adapter.
package internal

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	picture "github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
)

var slug = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// ValidRef checks structural identity without changing a source's canonical key.
func ValidRef(ref contracts.Ref) error {
	if !slug.MatchString(ref.Module) || !slug.MatchString(ref.Kind) || !text(ref.Key, 512) {
		return contracts.ErrInvalid
	}
	return nil
}

// ValidSave validates caller-owned facts before the source is consulted.
func ValidSave(in contracts.Save) error {
	if ValidRef(in.Source) != nil || !text(in.ExpectedSourceVersion, 128) {
		return contracts.ErrInvalid
	}
	return nil
}

// ItemID accepts only the canonical, nonzero UUID spelling used in saved links.
func ItemID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || id.String() != raw {
		return uuid.Nil, contracts.ErrInvalid
	}
	return id, nil
}

func text(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

// ValidDeclarations refuses a composition's declaration set that this service
// could not honour: a pair that is not a slug pair, a pair with no source to ask,
// one pair declared twice — which would make the answer to "who captures this
// save" depend on the order the modules were listed in — and a declared word no
// screen could print. A source owns the wording of its own label and this module
// prints it wherever a person sees the pair, so a word is refused here, before
// anything opens, rather than reaching a page as a paragraph or a control sequence.
func ValidDeclarations(savables []contracts.Savable) error {
	seen := map[string]bool{}
	for _, declared := range savables {
		pair := declared.Module + "|" + declared.Kind
		if !slug.MatchString(declared.Module) || !slug.MatchString(declared.Kind) {
			return fmt.Errorf("%w: savable %q/%q is not a slug pair", contracts.ErrInvalid, declared.Module, declared.Kind)
		}
		if declared.Source == nil {
			return fmt.Errorf("%w: savable %q/%q names no source", contracts.ErrInvalid, declared.Module, declared.Kind)
		}
		for _, word := range []string{declared.Words.Save, declared.Words.Saved, declared.Words.Remove, declared.Words.ListTitle} {
			// An empty word is not a bad one: it selects the fallback the composition's
			// own catalogue answers with.
			if word != "" && !text(word, 256) {
				return fmt.Errorf("%w: savable %q/%q declares a word no screen can print", contracts.ErrInvalid, declared.Module, declared.Kind)
			}
		}
		if seen[pair] {
			return fmt.Errorf("%w: savable %q/%q is declared twice", contracts.ErrInvalid, declared.Module, declared.Kind)
		}
		seen[pair] = true
	}
	return nil
}

// DecideSave validates freshly authorized source data and preserves an existing
// copy. A nil prior creates a snapshot; errors return zero and never change prior.
func DecideSave(prior *contracts.Item, in contracts.Save, capture contracts.Capture, at time.Time) (contracts.Snapshot, bool, error) {
	if err := ValidSave(in); err != nil {
		return contracts.Snapshot{}, false, err
	}
	if capture.Version != in.ExpectedSourceVersion {
		return contracts.Snapshot{}, false, contracts.ErrStale
	}
	if !text(capture.Title, 512) || !text(capture.Locale, 64) || !text(capture.Version, 128) || at.IsZero() {
		return contracts.Snapshot{}, false, contracts.ErrInvalid
	}
	values := bytes.TrimSpace(capture.Values)
	if len(values) == 0 || len(capture.Values) > 65536 || !utf8.Valid(capture.Values) || values[0] != '{' || !json.Valid(values) {
		return contracts.Snapshot{}, false, contracts.ErrInvalid
	}
	image, err := retainPicture(capture.Picture)
	if err != nil {
		return contracts.Snapshot{}, false, err
	}
	if prior != nil {
		return prior.Snapshot, false, nil
	}
	return contracts.Snapshot{Title: capture.Title, Locale: capture.Locale, SourceVersion: capture.Version,
		Values: bytes.Clone(capture.Values), Picture: image, CapturedAt: at.UTC().Truncate(time.Microsecond)}, true, nil
}

func retainPicture(in contracts.PictureInput) (contracts.Picture, error) {
	digest := in.Template.Digest()
	if in.FileID != "" {
		_, err := ItemID(in.FileID)
		if err != nil || digest != [32]byte{} || len(in.Value.Options) != 0 || len(in.Value.Colours) != 0 || in.Value.Background != "" {
			return contracts.Picture{}, contracts.ErrInvalid
		}
		return contracts.Picture{FileID: in.FileID}, nil
	}
	if digest == [32]byte{} {
		if len(in.Value.Options) != 0 || len(in.Value.Colours) != 0 || in.Value.Background != "" {
			return contracts.Picture{}, contracts.ErrInvalid
		}
		return contracts.Picture{}, nil
	}
	resolved, err := picture.Resolve(in.Template, in.Value)
	if err != nil {
		return contracts.Picture{}, fmt.Errorf("%w: %v", contracts.ErrInvalid, err)
	}
	svg, err := picture.Compose(in.Template, resolved)
	if err != nil {
		return contracts.Picture{}, fmt.Errorf("%w: %v", contracts.ErrInvalid, err)
	}
	return contracts.Picture{SVG: svg, TemplateDigest: hex.EncodeToString(digest[:]), Value: resolved.Canonical()}, nil
}

// DecideUnsave checks a present own row's revision. Absence is already the
// requested state, including a row that is outside the caller's ownership.
func DecideUnsave(prior *contracts.Item, expected int64) (bool, error) {
	if prior == nil {
		return false, nil
	}
	if prior.Revision != expected {
		return false, contracts.ErrStale
	}
	return true, nil
}

// PageBounds refuses unsupported limits and filters rather than silently widening.
func PageBounds(q contracts.Query) (int, error) {
	if q.Offset < 0 || q.Limit < 0 || q.Limit > 100 || (q.Module != "" && !slug.MatchString(q.Module)) || (q.Kind != "" && !slug.MatchString(q.Kind)) {
		return 0, contracts.ErrInvalid
	}
	if q.Limit == 0 {
		return 24, nil
	}
	return q.Limit, nil
}

// Availability classifies a source observation without changing the saved copy.
// Unrecognized errors abort the read rather than returning a partially read page.
func Availability(err error) (string, error) {
	switch {
	case err == nil:
		return "available", nil
	case errors.Is(err, contracts.ErrUnavailable), errors.Is(err, contracts.ErrDenied):
		return "unavailable", nil
	case errors.Is(err, contracts.ErrTemporary):
		return "temporary", nil
	default:
		return "", err
	}
}

// Destination accepts only an application-local URL produced by an authorized
// source. The destination still owns its own authorization when followed.
func Destination(path string) (string, error) {
	u, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(path, "/app/") || strings.ContainsAny(path, "\\\r\n") || u.IsAbs() || u.Host != "" {
		return "", contracts.ErrInvalid
	}
	return path, nil
}
