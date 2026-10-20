package entity

import (
	"encoding/base64"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Picture is a derived composed-image reference, never a stored-file identifier.
type Picture string

// TypePicture describes a derived picture in entity and shell metadata.
const TypePicture FieldType = "picture"

func validPictureKey(key string) bool {
	return len(key) > 0 && len(key) <= 512 && utf8.ValidString(key) && !strings.ContainsFunc(key, unicode.IsControl)
}

// ComposedPicture encodes a product source key as one path segment. It grants no
// access; keys are 1–512 UTF-8 bytes without controls. Invalid keys return empty.
func ComposedPicture(key string) (Picture, error) {
	if !validPictureKey(key) {
		return "", errors.New("picture: source key must be 1–512 UTF-8 bytes without controls")
	}
	return Picture("c1." + base64.RawURLEncoding.EncodeToString([]byte(key))), nil
}

// PictureKey decodes a canonical nonempty reference. Empty values mean no picture
// in presentation but are refused here, as are padded or alternative encodings.
func PictureKey(p Picture) (string, error) {
	bad := errors.New("picture: use a canonical composed-picture reference")
	if len(p) > 686 || !strings.HasPrefix(string(p), "c1.") {
		return "", bad
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(string(p)[3:])
	if err != nil || !validPictureKey(string(b)) {
		return "", bad
	}
	canonical, _ := ComposedPicture(string(b))
	if canonical != p {
		return "", bad
	}
	return string(b), nil
}
