package entity

import "testing"

// The four words a record's locale switcher wears, and the fold that chooses one.
// The numbers come from kit/rest's read; what is decided here is which shortfall a
// reader is told about, and that is a rule about words rather than about rows, so it
// belongs beside the struct it names.
func TestALocaleStateNamesTheWordItIsBehindBy(t *testing.T) {
	for _, tc := range []struct {
		what  string
		state LocaleState
		want  string
	}{
		{what: "every field is this language's own",
			state: LocaleState{Reviewed: 2, Fields: 2}, want: LocaleComplete},
		{what: "no field was ever translated",
			state: LocaleState{Fields: 2}, want: LocaleMissing},
		{what: "one field of two is translated and the other has no row",
			state: LocaleState{Reviewed: 1, Fields: 2}, want: LocaleMissing},
		{what: "one field of two is a draft nobody read",
			state: LocaleState{Reviewed: 1, Drafted: 1, Fields: 2}, want: LocaleMachine},
		{what: "one reviewed field's source moved",
			state: LocaleState{Reviewed: 1, Behind: 1, Fields: 2}, want: LocaleOutdated},
		{what: "the stale field outranks the draft nobody opened",
			state: LocaleState{Reviewed: 1, Behind: 1, Drafted: 1, Fields: 3}, want: LocaleOutdated},
		{what: "a record with nothing translatable",
			state: LocaleState{}, want: LocaleComplete},
	} {
		if got := tc.state.State(); got != tc.want {
			t.Errorf("%s: State() = %q, want %q", tc.what, got, tc.want)
		}
	}
}

// The word and the number have to be two answers to two questions, or the badge that
// wears both contradicts itself: a language that is whole reads complete, and one
// that is not never does.
func TestALocaleStatesWordAgreesWithItsShare(t *testing.T) {
	for _, state := range []LocaleState{
		{Reviewed: 0, Fields: 0}, {Reviewed: 0, Fields: 3}, {Reviewed: 2, Fields: 3},
		{Reviewed: 3, Fields: 3}, {Reviewed: 1, Behind: 2, Fields: 3},
	} {
		if whole := state.Percent() == 100; whole != (state.State() == LocaleComplete) {
			t.Errorf("%+v reads %d%% and %s, which disagree", state, state.Percent(), state.State())
		}
	}
}
