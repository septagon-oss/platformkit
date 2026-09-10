package design_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// Independent SHA-256 vectors for "abc" and "hello"; deliberately not fonts.
// Matching byte identity alone must not be confused with usable font evidence.
func assetFixture() (design.Asset, design.FontFace) {
	asset := design.Asset{
		ID: "brand/font/body/roman", MediaType: "font/woff2", Source: "brand/assets/body",
		SHA256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		License: design.LicenseEvidence{ID: "LicenseRef-Brand-Font", Source: "brand/notices/body",
			SHA256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
	}
	face := design.FontFace{ID: "brand/face/body/650", Asset: asset.ID, Family: "Example, Sans", PostScriptName: "ExampleSans-Book", Weight: "650", Style: "normal"}
	return asset, face
}

func TestAssetMetadataAndBytesAreSeparateFromProviderReadiness(t *testing.T) {
	t.Parallel()
	asset, face := assetFixture()
	before, beforeFace := asset, face
	if err := design.ValidateAssets([]design.Asset{asset}, []design.FontFace{face}); err != nil {
		t.Fatalf("Core should describe weight 650 and WOFF2 independently of a provider: %v", err)
	}
	data, notice := []byte("abc"), []byte("hello")
	if err := asset.VerifyBytes(data, notice); err != nil {
		t.Fatal(err)
	}
	if asset != before || face != beforeFace || !bytes.Equal(data, []byte("abc")) || !bytes.Equal(notice, []byte("hello")) {
		t.Fatal("metadata or byte preflight mutated caller input")
	}
	for _, weight := range []json.Number{"1", "650.5", "1000"} {
		face.Weight = weight
		if err := design.ValidateAssets([]design.Asset{asset}, []design.FontFace{face}); err != nil {
			t.Fatalf("valid source weight %s refused: %v", weight, err)
		}
	}
	icon := asset
	icon.ID, icon.MediaType = "brand/icon/mark", "image/svg+xml"
	if err := design.ValidateAssets([]design.Asset{icon}, nil); err != nil {
		t.Fatalf("asset-only packages do not require fonts, examples or layout: %v", err)
	}
	if err := design.ValidateAssets(nil, nil); err != nil {
		t.Fatalf("empty optional asset selection: %v", err)
	}
}

func TestAssetMetadataRejectsIncompleteEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*design.Asset)
	}{
		{"missing identity", func(a *design.Asset) { a.ID = "" }},
		{"padded identity", func(a *design.Asset) { a.ID = " asset" }},
		{"control identity", func(a *design.Asset) { a.ID = "asset\x00name" }},
		{"invalid UTF-8", func(a *design.Asset) { a.Source = "\xff" }},
		{"missing provenance", func(a *design.Asset) { a.Source = "" }},
		{"missing digest", func(a *design.Asset) { a.SHA256 = "" }},
		{"upper-case digest", func(a *design.Asset) { a.SHA256 = strings.ToUpper(a.SHA256) }},
		{"short digest", func(a *design.Asset) { a.SHA256 = strings.Repeat("a", 63) }},
		{"nonhex digest", func(a *design.Asset) { a.SHA256 = strings.Repeat("g", 64) }},
		{"missing format", func(a *design.Asset) { a.MediaType = "" }},
		{"incomplete format", func(a *design.Asset) { a.MediaType = "font" }},
		{"wildcard format", func(a *design.Asset) { a.MediaType = "font/*" }},
		{"format parameters", func(a *design.Asset) { a.MediaType = "font/woff2; charset=utf-8" }},
		{"noncanonical format", func(a *design.Asset) { a.MediaType = "Font/WOFF2" }},
		{"missing license", func(a *design.Asset) { a.License = design.LicenseEvidence{} }},
		{"missing notice digest", func(a *design.Asset) { a.License.SHA256 = "" }},
		{"missing notice provenance", func(a *design.Asset) { a.License.Source = "" }},
		{"missing license identity", func(a *design.Asset) { a.License.ID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asset, _ := assetFixture()
			tc.change(&asset)
			before := asset
			if err := asset.Validate(); err == nil {
				t.Fatal("incomplete asset evidence admitted")
			}
			if err := asset.VerifyBytes([]byte("abc"), []byte("hello")); err == nil {
				t.Fatal("matching bytes bypassed invalid metadata")
			}
			if asset != before {
				t.Fatal("refusal changed metadata")
			}
		})
	}
}

func TestAssetByteEvidenceRejectsSubstitutionAndMissingNotices(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, data, notice string
	}{
		{"substituted asset", "different font with the same family label", "hello"},
		{"substituted notice", "abc", "different terms"},
		{"absent bytes", "", "hello"},
		{"absent notice", "abc", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asset, _ := assetFixture()
			if err := asset.VerifyBytes([]byte(tc.data), []byte(tc.notice)); err == nil {
				t.Fatal("incorrect byte identity admitted")
			}
		})
	}
}

func TestAssetSelectionChecksEveryFaceAndReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*design.FontFace)
	}{
		{"missing face identity", func(f *design.FontFace) { f.ID = "" }},
		{"missing asset reference", func(f *design.FontFace) { f.Asset = "" }},
		{"unknown asset reference", func(f *design.FontFace) { f.Asset = "missing" }},
		{"missing family", func(f *design.FontFace) { f.Family = "" }},
		{"control family", func(f *design.FontFace) { f.Family = "Family\nName" }},
		{"leading control family", func(f *design.FontFace) { f.Family = "\nFamily" }},
		{"missing physical name", func(f *design.FontFace) { f.PostScriptName = "" }},
		{"unsupported style", func(f *design.FontFace) { f.Style = "oblique 10deg" }},
		{"missing style", func(f *design.FontFace) { f.Style = "" }},
		{"missing weight", func(f *design.FontFace) { f.Weight = "" }},
		{"zero weight", func(f *design.FontFace) { f.Weight = "0" }},
		{"high weight", func(f *design.FontFace) { f.Weight = "1001" }},
		{"rounded high weight", func(f *design.FontFace) { f.Weight = "1000.0000000000000001" }},
		{"rounded low weight", func(f *design.FontFace) { f.Weight = "0.99999999999999999" }},
		{"nonfinite weight", func(f *design.FontFace) { f.Weight = "NaN" }},
		{"overflow weight", func(f *design.FontFace) { f.Weight = "1e999" }},
		{"non-JSON weight", func(f *design.FontFace) { f.Weight = "+650" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asset, face := assetFixture()
			bad := face
			bad.ID = "second/face"
			tc.change(&bad)
			faces := []design.FontFace{face, bad}
			before := slices.Clone(faces)
			if err := design.ValidateAssets([]design.Asset{asset}, faces); err == nil {
				t.Fatal("invalid dependency after valid face was ignored")
			}
			if !reflect.DeepEqual(faces, before) {
				t.Fatal("preflight changed the selected faces")
			}
		})
	}
	asset, face := assetFixture()
	if err := design.ValidateAssets([]design.Asset{asset, asset}, nil); err == nil {
		t.Fatal("duplicate asset identity admitted")
	}
	if err := design.ValidateAssets([]design.Asset{asset}, []design.FontFace{face, face}); err == nil {
		t.Fatal("duplicate face identity admitted")
	}
	asset.MediaType = "image/svg+xml"
	if err := design.ValidateAssets([]design.Asset{asset}, []design.FontFace{face}); err == nil {
		t.Fatal("non-font asset accepted as a face dependency")
	}
}

func TestEverySelectedAssetOwnsItsLicenseEvidence(t *testing.T) {
	t.Parallel()
	first, _ := assetFixture()
	second := first
	second.ID, second.License = "another/asset", design.LicenseEvidence{}
	if err := design.ValidateAssets([]design.Asset{first, second}, nil); err == nil {
		t.Fatal("one asset's license implicitly covered another asset")
	}
}
