package design

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"mime"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// LicenseEvidence identifies the owner's license declaration and exact notice
// bytes. ID may be an SPDX identifier or an owner-defined reference. Evidence
// is not a determination of authorship or permission to redistribute an asset.
type LicenseEvidence struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
	Source string `json:"source"`
}

// Asset is caller-supplied source metadata, not another asset registry. Source
// and License.Source are provenance references, never implicit fetch locations.
// Physical paths, delivery authorization and provider support live downstream.
type Asset struct {
	ID        string          `json:"id"`
	SHA256    string          `json:"sha256"`
	MediaType string          `json:"mediaType"`
	Source    string          `json:"source"`
	License   LicenseEvidence `json:"license"`
}

// FontFace describes one named static face in an Asset, not a fallback stack.
// Family and PostScriptName are exact owner-supplied names, not filename guesses.
// Weights retain the CSS numeric domain independently of a provider's subset.
// Variable axes, collections, width matching and glyph coverage are not certified
// by this metadata profile; the provider must inspect actual font bytes.
type FontFace struct {
	ID             string      `json:"id"`
	Asset          string      `json:"asset"`
	Family         string      `json:"family"`
	PostScriptName string      `json:"postScriptName"`
	Weight         json.Number `json:"weight"`
	Style          string      `json:"style"`
}

var assetDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func evidenceText(s string) bool {
	return s != "" && s == strings.TrimSpace(s) && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}

// Validate checks metadata only. It neither reads resources nor validates the
// claimed media format, font names or license terms against physical bytes.
func (a Asset) Validate() error {
	if !evidenceText(a.ID) || !evidenceText(a.Source) || !assetDigest.MatchString(a.SHA256) {
		return fmt.Errorf("asset %q requires an identity, provenance and lowercase SHA-256", a.ID)
	}
	if !evidenceText(a.License.ID) || !evidenceText(a.License.Source) || !assetDigest.MatchString(a.License.SHA256) {
		return fmt.Errorf("asset %q requires its own license identity, provenance and notice SHA-256", a.ID)
	}
	mediaType, params, err := mime.ParseMediaType(a.MediaType)
	if err != nil || mediaType != a.MediaType || !strings.Contains(mediaType, "/") || strings.Contains(mediaType, "*") || len(params) != 0 {
		return fmt.Errorf("asset %q requires a canonical concrete media type without parameters", a.ID)
	}
	return nil
}

// VerifyBytes checks supplied asset and nonempty license notice bytes against
// their declared digests, without mutation or I/O. Success proves byte identity
// only: decoding, exact face/glyph checks and legal review remain separate gates.
func (a Asset) VerifyBytes(data, notice []byte) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if len(data) == 0 || fmt.Sprintf("%x", sha256.Sum256(data)) != a.SHA256 {
		return fmt.Errorf("asset %q bytes are missing or disagree with SHA-256", a.ID)
	}
	if len(bytes.TrimSpace(notice)) == 0 || fmt.Sprintf("%x", sha256.Sum256(notice)) != a.License.SHA256 {
		return fmt.Errorf("asset %q license notice is missing or disagrees with SHA-256", a.ID)
	}
	return nil
}

// ValidateAssets checks the entire selected metadata dependency closure. An
// asset-only package needs no fonts, components or layout. Distinct identities
// are not merged by equal bytes; font matching ambiguity remains a provider gate.
func ValidateAssets(assets []Asset, faces []FontFace) error {
	byID := make(map[string]Asset, len(assets))
	for _, asset := range assets {
		if err := asset.Validate(); err != nil {
			return err
		}
		if _, exists := byID[asset.ID]; exists {
			return fmt.Errorf("duplicate asset %q", asset.ID)
		}
		byID[asset.ID] = asset
	}
	seen := make(map[string]bool, len(faces))
	for _, face := range faces {
		if !evidenceText(face.ID) || seen[face.ID] {
			return fmt.Errorf("invalid or duplicate face %q", face.ID)
		}
		seen[face.ID] = true
		if strings.TrimSpace(face.Family) == "" || !utf8.ValidString(face.Family) ||
			strings.IndexFunc(face.Family, unicode.IsControl) >= 0 || !evidenceText(face.PostScriptName) {
			return fmt.Errorf("face %q requires a literal family and physical PostScript name", face.ID)
		}
		if err := ValidateFontWeight(face.Weight); err != nil {
			return fmt.Errorf("face %q: %w", face.ID, err)
		}
		if face.Style != "normal" && face.Style != "italic" {
			return fmt.Errorf("face %q requires explicit normal or italic style in the static profile", face.ID)
		}
		asset, exists := byID[face.Asset]
		if !exists || !strings.HasPrefix(asset.MediaType, "font/") {
			return fmt.Errorf("face %q requires an existing font asset %q", face.ID, face.Asset)
		}
	}
	return nil
}
