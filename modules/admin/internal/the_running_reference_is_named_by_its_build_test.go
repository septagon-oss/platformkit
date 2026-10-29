package internal

import (
	"runtime/debug"
	"testing"
)

// The footer and the start command both say which reference is running, and the
// artifact a person reaches decides which half of the build info carries that: a
// build through the module proxy knows its version and nothing about the
// repository, a build from a checkout knows the revision and nothing about the
// version. Each is the only answer the build that carries it has.
func TestTheFooterNamesTheStampTheRunningArtifactCarries(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{
			name: "a build made through the module proxy",
			info: &debug.BuildInfo{Main: debug.Module{Version: "v1.1.2-proof.20261027.e5fe4025"}},
			want: "v1.1.2-proof.20261027.e5fe4025",
		},
		{
			name: "a checkout build uses its revision when a module version is also present",
			info: &debug.BuildInfo{Main: debug.Module{Version: "v1.1.2-proof.20261029.263f8765e464"}, Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
			}},
			want: "01234567",
		},
		{
			name: "a build with no stamp at all",
			info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want: "(development)",
		},
		{name: "no build info at all", info: nil, want: "(unknown build)"},
	} {
		if got := version(c.info); got != c.want {
			t.Errorf("%s: the footer says %q, want %q", c.name, got, c.want)
		}
	}
}

// The footer is the brand beside the stamp, so a page and a terminal that both
// name the reference name the same string.
func TestTheReferenceTheFooterNamesCarriesTheBrand(t *testing.T) {
	t.Parallel()
	if got := Build(); got == "" || got[:len(brand)+1] != brand+" " {
		t.Errorf("Build() = %q, want %q followed by the stamp", got, brand+" ")
	}
}
