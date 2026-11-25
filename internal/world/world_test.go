// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

package world

import (
	"slices"
	"testing"
)

// loadable returns the objects of a small valid saved world: the archwizard
// #0 (self-owned), a room #1 holding the wizard and a lamp #2, and the lamp's
// prototype #3. #4 is recycled and the next ID is #5. Each call returns fresh
// objects, since Load takes ownership of what it is given.
func loadable() ([]*Object, []ObjID, ObjID) {
	objs := []*Object{
		{ID: 0, Parent: Nothing, Owner: 0, Name: "Wizard", Location: 1, Flags: FlagPlayer | FlagWizard},
		{ID: 1, Parent: Nothing, Owner: 0, Name: "Lobby", Location: Nothing, Flags: FlagRoom},
		{ID: 2, Parent: 3, Owner: 0, Name: "lamp", Location: 1,
			Props: map[string]Property{"lit": prop(0, PropRead, `false`)}},
		{ID: 3, Parent: Nothing, Owner: 0, Name: "generic lamp", Location: Nothing, Flags: FlagFertile,
			Methods: map[string]Method{"rub": nlMethod("rub", 0, MethodExec, "Glow.")}},
	}
	return objs, []ObjID{4}, 5
}

// TestLoad: a valid saved world loads, with Contents rebuilt in ID order
// whatever the saved objects claimed.
func TestLoad(t *testing.T) {
	objs, recycled, next := loadable()
	objs[1].Contents = []ObjID{99} // stale derived state must be ignored
	w, err := Load(objs, recycled, next)
	must(t, err)
	if got := contentsOf(t, w, 1); !slices.Equal(got, []ObjID{0, 2}) {
		t.Errorf("room contents = %v, want [#0 #2]", got)
	}
	if !slices.Equal(w.ObjectIDs(), []ObjID{0, 1, 2, 3}) || !slices.Equal(w.RecycledIDs(), []ObjID{4}) {
		t.Errorf("IDs: live %v, recycled %v", w.ObjectIDs(), w.RecycledIDs())
	}
	if w.NextID() != 5 || !w.IsLive(2) || w.IsLive(4) || !w.IsRecycled(4) || w.IsRecycled(2) {
		t.Error("ID bookkeeping is wrong")
	}
	// New objects continue from nextID; recycled IDs are never reused.
	id, err := w.Create(3, 0, "another lamp", 0)
	must(t, err)
	if id != 5 {
		t.Errorf("new object got %v, want #5", id)
	}
	if _, _, err := w.LookupMethod(2, "rub"); err != nil {
		t.Errorf("inherited method lost: %v", err)
	}
	checkWorld(t, w)
}

// TestLoadRejects: Load refuses saved worlds that are malformed or break an
// invariant, which is what will protect the server from a damaged database
// file at M3.
func TestLoadRejects(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(objs []*Object) ([]*Object, []ObjID, ObjID)
		want    error
	}{
		{"nil object", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			return append(o, nil), []ObjID{4}, 5
		}, ErrInconsistent},
		{"duplicate ID", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			dup := *o[3]
			return append(o, &dup), []ObjID{4}, 5
		}, ErrInconsistent},
		{"ID beyond nextID", func(o []*Object) ([]*Object, []ObjID, ObjID) { return o, []ObjID{4}, 3 }, ErrInconsistent},
		{"negative ID", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			o[3].ID = -5
			return o, []ObjID{4}, 5
		}, ErrInconsistent},
		{"recycled beyond nextID", func(o []*Object) ([]*Object, []ObjID, ObjID) { return o, []ObjID{4, 7}, 5 }, ErrInconsistent},
		{"live and recycled", func(o []*Object) ([]*Object, []ObjID, ObjID) { return o, []ObjID{3, 4}, 5 }, ErrInconsistent},
		{"self containment", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			o[2].Location = 2
			return o, []ObjID{4}, 5
		}, ErrContainmentCycle},
		{"two-object containment loop", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			o[2].Location, o[3].Location = 3, 2
			return o, []ObjID{4}, 5
		}, ErrContainmentCycle},
		{"three-object containment loop", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			o[1].Location, o[2].Location, o[3].Location = 3, 1, 2
			return o, []ObjID{4}, 5
		}, ErrContainmentCycle},
		{"inheritance loop", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			o[3].Parent = 2 // 2 -> 3 -> 2
			return o, []ObjID{4}, 5
		}, ErrInheritanceCycle},
		{"player in a thing", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			o[0].Location = 2
			return o, []ObjID{4}, 5
		}, ErrPlayerPlacement},
		{"located in a recycled object", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			o[2].Location = 4
			return o, []ObjID{4}, 5
		}, ErrRecycled},
		{"child of a recycled object", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			o[2].Parent = 4
			return o, []ObjID{4}, 5
		}, ErrRecycled},
		{"owned by a recycled player", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			o[2].Owner = 4
			return o, []ObjID{4}, 5
		}, ErrRecycled},
		{"owned by a non-player", func(o []*Object) ([]*Object, []ObjID, ObjID) {
			o[2].Owner = 1
			return o, []ObjID{4}, 5
		}, ErrBadOwner},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objs, _, _ := loadable()
			w, err := Load(c.corrupt(objs))
			wantErr(t, err, c.want)
			if w != nil {
				t.Error("Load returned a world along with an error")
			}
		})
	}
}

// TestObjectAccessor covers Object for live, recycled and missing IDs.
func TestObjectAccessor(t *testing.T) {
	objs, recycled, next := loadable()
	w, err := Load(objs, recycled, next)
	must(t, err)
	if o, ok := w.Object(2); !ok || o.Name != "lamp" {
		t.Errorf("Object(#2) = %v, %v", o, ok)
	}
	for _, id := range []ObjID{4, 99, Nothing} {
		if _, ok := w.Object(id); ok {
			t.Errorf("Object(%v) found something", id)
		}
	}
}
