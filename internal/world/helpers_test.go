// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

package world

// Shared test helpers. The tests are in package world itself (a "white-box"
// test), so they can reach unexported fields when they need to build a
// deliberately corrupt world that the public API would refuse to create.

import (
	"errors"
	"testing"
)

// fixture is a small world most tests start from: an archwizard, two
// ordinary players and a room owned by the wizard.
type fixture struct {
	w     *World
	wiz   ObjID // a wizard
	owner ObjID // the player who owns the things under test
	other ObjID // a player with no special relationship to them
	room  ObjID // a room the players can stand in
}

func newFixture(t *testing.T) *fixture {
	t.Helper() // makes failures report the caller's line, not this one
	f := &fixture{w: New()}
	f.wiz = f.create(t, Nothing, Nothing, "Wizard", FlagPlayer|FlagWizard)
	f.owner = f.create(t, Nothing, Nothing, "Fran", FlagPlayer)
	f.other = f.create(t, Nothing, Nothing, "Visitor", FlagPlayer)
	f.room = f.create(t, Nothing, f.wiz, "Lobby", FlagRoom)
	return f
}

// create calls Create and fails the test on error.
func (f *fixture) create(t *testing.T, parent ObjID, owner PlayerID, name string, flags ObjFlags) ObjID {
	t.Helper()
	id, err := f.w.Create(parent, owner, name, flags)
	if err != nil {
		t.Fatalf("Create(%v, %v, %q, %v): %v", parent, owner, name, flags, err)
	}
	return id
}

// must fails the test if err is not nil.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// wantErr fails the test unless err wraps want (or is nil when want is nil).
func wantErr(t *testing.T, err, want error) {
	t.Helper()
	switch {
	case want == nil && err != nil:
		t.Fatalf("unexpected error: %v", err)
	case want != nil && !errors.Is(err, want):
		t.Fatalf("got error %v, want one wrapping %q", err, want)
	}
}

// checkWorld fails the test if the world breaks any invariant. Tests call it
// after mutating, so every scenario also proves the mutators kept the world
// consistent.
func checkWorld(t *testing.T, w *World) {
	t.Helper()
	if err := w.CheckInvariants(); err != nil {
		t.Fatalf("invariants broken: %v", err)
	}
}

// prop is a shorthand for a Property with a JSON value.
func prop(owner PlayerID, perms PropPerms, value string) Property {
	return Property{Value: JSONValue(value), Owner: owner, Perms: perms}
}

// nlMethod is a shorthand for a natural-language method.
func nlMethod(name string, owner PlayerID, perms MethodPerms, body string) Method {
	return Method{Name: name, Owner: owner, Perms: perms, Body: body}
}
