// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

package world

import "testing"

// TestObjIDNotation: String and ParseObjID round-trip, and only the
// canonical spelling parses.
func TestObjIDNotation(t *testing.T) {
	good := []struct {
		s  string
		id ObjID
	}{
		{"#0", 0}, {"#1023", 1023}, {"#-1", Nothing}, {"#9223372036854775807", 1<<63 - 1},
	}
	for _, g := range good {
		id, err := ParseObjID(g.s)
		if err != nil || id != g.id {
			t.Errorf("ParseObjID(%q) = %v, %v; want %v", g.s, id, err, g.id)
		}
		if g.id.String() != g.s {
			t.Errorf("%d.String() = %q, want %q", int64(g.id), g.id.String(), g.s)
		}
	}
	for _, bad := range []string{"", "#", "1023", "#007", "#+7", "#-0", "# 7", "#7 ", "#x", "#99999999999999999999"} {
		if id, err := ParseObjID(bad); err == nil {
			t.Errorf("ParseObjID(%q) = %v, want an error", bad, id)
		}
	}
}

// TestFlagsAndPermsStrings checks the human-readable renderings, which show
// up in error messages and, later, in @show output.
func TestFlagsAndPermsStrings(t *testing.T) {
	cases := []struct{ got, want string }{
		{ObjFlags(0).String(), "none"},
		{(FlagPlayer | FlagWizard).String(), "player|wizard"},
		{(FlagRoom | FlagExit | FlagFertile | FlagFrozen | FlagVehicle).String(), "room|exit|fertile|frozen|vehicle"},
		{(FlagRoom | 1<<20).String(), "room|0x100000"},
		{PropPerms(0).String(), ""},
		{(PropRead | PropCopy).String(), "rc"},
		{(PropRead | PropWrite | PropCopy).String(), "rwc"},
		{PropWrite.String(), "w"},
		{MethodExec.String(), "x"},
		{MethodPerms(0).String(), ""},
		{Mechanical.String(), "mechanical"},
		{NaturalLanguage.String(), "natural-language"},
		{MethodKind(9).String(), "MethodKind(9)"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
	if !(FlagPlayer | FlagRoom).Has(FlagPlayer) || FlagPlayer.Has(FlagPlayer|FlagRoom) {
		t.Error("Has is wrong")
	}
}

// TestCareLevel checks the three levels and nothing else are valid.
func TestCareLevel(t *testing.T) {
	for _, c := range []CareLevel{CareRoutine, CareNormal, CareHigh} {
		if !c.Valid() {
			t.Errorf("%q not valid", c)
		}
	}
	for _, c := range []CareLevel{"", "Normal", "low"} {
		if c.Valid() {
			t.Errorf("%q valid", c)
		}
	}
}
