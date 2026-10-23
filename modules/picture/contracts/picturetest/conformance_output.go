package picturetest

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/modules/picture/contracts"
)

func literalOutput(t *testing.T, factory func() Decision) {
	d := factory()
	template, err := d.Declare("fixture", SVG)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", template.Digest()); got != "1fdaecd0f607fabc10659e15b9d0e00fcf97ffa4e465961d55e6ab2639f31293" {
		t.Fatalf("template digest %s", got)
	}
	r, err := contracts.Resolve(template, Value())
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Canonical()) != `{"options":[{"part":"shape","option":"round"}],"colours":["#AABBCC","#112233"],"background":"#FFFFFF"}` {
		t.Fatalf("canonical %s", r.Canonical())
	}
	if got := fmt.Sprintf("%x", r.Identity()); got != "e3bf5fc3f758a23386b85528b5c640c1d949741b36fa69702377ee35fee56fe5" {
		t.Fatalf("identity %s", got)
	}
	for range 3 {
		body, err := d.Render(template, Value())
		if err != nil || body != Oracle {
			t.Fatalf("render %q: %v", body, err)
		}
		if fmt.Sprintf("%x", sha256.Sum256([]byte(body))) != "2ab9f69bb3f58230f64771b7fdb0a55cee88c9536bde570a2546fd4be6e63fb1" {
			t.Fatal("body digest")
		}
	}
}

func utf8Declarations(t *testing.T, factory func() Decision) {
	for _, declaration := range []string{
		`<?xml version="1.0"?>`,
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<?xml version='1.0' encoding='utf-8' standalone='yes'?>`,
	} {
		d := factory()
		template, err := d.Declare("fixture", declaration+SVG)
		if err != nil {
			t.Fatal(err)
		}
		if body, err := d.Render(template, Value()); err != nil || body != Oracle {
			t.Fatalf("declared output %q: %v", body, err)
		}
	}
}
