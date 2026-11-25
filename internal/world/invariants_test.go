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

// contentsOf returns obj's Contents, failing the test if obj is not live.
func contentsOf(t *testing.T, w *World, obj ObjID) []ObjID {
	t.Helper()
	o, ok := w.Object(obj)
	if !ok {
		t.Fatalf("%v is not live", obj)
	}
	return o.Contents
}

// TestMoveMaintainsContents: Move keeps Contents the exact inverse of
// Location, in arrival order.
func TestMoveMaintainsContents(t *testing.T) {
	f := newFixture(t)
	box := f.create(t, Nothing, f.owner, "box", 0)
	a := f.create(t, Nothing, f.owner, "a", 0)
	b := f.create(t, Nothing, f.owner, "b", 0)
	c := f.create(t, Nothing, f.owner, "c", 0)
	for _, x := range []ObjID{b, a, c} {
		must(t, f.w.Move(x, box))
	}
	if got := contentsOf(t, f.w, box); !slices.Equal(got, []ObjID{b, a, c}) {
		t.Fatalf("contents = %v, want arrival order [%v %v %v]", got, b, a, c)
	}
	must(t, f.w.Move(a, Nothing))
	if got := contentsOf(t, f.w, box); !slices.Equal(got, []ObjID{b, c}) {
		t.Fatalf("after removing a: %v", got)
	}
	must(t, f.w.Move(b, box)) // already there: no change in order
	if got := contentsOf(t, f.w, box); !slices.Equal(got, []ObjID{b, c}) {
		t.Fatalf("after no-op move: %v", got)
	}
	must(t, f.w.Move(box, f.room))
	must(t, f.w.Move(c, f.room)) // out of the box, into the room it is in
	if got := contentsOf(t, f.w, f.room); !slices.Equal(got, []ObjID{box, c}) {
		t.Fatalf("room contents: %v", got)
	}
	checkWorld(t, f.w)
}

// TestContainmentCycles: no move may put an object inside itself, directly
// or through any number of intermediate containers.
func TestContainmentCycles(t *testing.T) {
	f := newFixture(t)
	// A nest of five boxes: box[0] contains box[1] contains ... box[4].
	var box []ObjID
	for i := 0; i < 5; i++ {
		box = append(box, f.create(t, Nothing, f.owner, "box", 0))
		if i > 0 {
			must(t, f.w.Move(box[i], box[i-1]))
		}
	}
	cases := []struct {
		name      string
		obj, dest ObjID
		want      error
	}{
		{"into itself", box[2], box[2], ErrContainmentCycle},
		{"into its direct content", box[0], box[1], ErrContainmentCycle},
		{"into a transitive content", box[0], box[4], ErrContainmentCycle},
		{"middle into its own descendant", box[1], box[3], ErrContainmentCycle},
		{"inner into outer is fine", box[4], box[0], nil},
		{"outer box into the room is fine", box[0], f.room, nil},
		{"room into a box it now holds, two levels down", f.room, box[4], ErrContainmentCycle},
		{"room into a box it holds directly", f.room, box[0], ErrContainmentCycle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantErr(t, f.w.Move(c.obj, c.dest), c.want)
			checkWorld(t, f.w)
		})
	}
	// Taking box[4] out into the room breaks that chain, so then the room
	// may go into box[4] (rooms can be inside things, as in LambdaMOO).
	must(t, f.w.Move(box[4], Nothing))
	must(t, f.w.Move(f.room, box[4]))
	checkWorld(t, f.w)
}

// TestPlayerPlacement: players may be only in rooms, vehicles or nowhere.
func TestPlayerPlacement(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "chest", 0)
	car := f.create(t, Nothing, f.owner, "car", FlagVehicle)
	exit := f.create(t, Nothing, f.wiz, "north", FlagExit)
	cases := []struct {
		name string
		dest ObjID
		want error
	}{
		{"room", f.room, nil},
		{"vehicle", car, nil},
		{"nowhere", Nothing, nil},
		{"thing", thing, ErrPlayerPlacement},
		{"exit", exit, ErrPlayerPlacement},
		{"another player", f.other, ErrPlayerPlacement},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantErr(t, f.w.Move(f.owner, c.dest), c.want)
			checkWorld(t, f.w)
		})
	}
	// Non-players may go inside players (that is an inventory).
	must(t, f.w.Move(thing, f.other))
	checkWorld(t, f.w)
}

// TestFlagChangesKeepPlayersPlaced: a room or vehicle with players in it
// cannot stop being one, but may switch from one to the other.
func TestFlagChangesKeepPlayersPlaced(t *testing.T) {
	f := newFixture(t)
	must(t, f.w.Move(f.owner, f.room))
	wantErr(t, f.w.SetFlags(f.room, 0), ErrPlayerPlacement)
	must(t, f.w.SetFlags(f.room, FlagVehicle)) // still somewhere players may be
	must(t, f.w.SetFlags(f.room, FlagRoom|FlagFertile))
	must(t, f.w.Move(f.owner, Nothing))
	must(t, f.w.SetFlags(f.room, 0)) // empty now, so fine
	checkWorld(t, f.w)
}

// TestFlagInvariants covers the remaining SetFlags and Create rules.
func TestFlagInvariants(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	cases := []struct {
		name  string
		obj   ObjID
		flags ObjFlags
		want  error
	}{
		{"make a thing a player", thing, FlagPlayer, ErrBadFlags},
		{"make a player a non-player", f.owner, 0, ErrBadFlags},
		{"wizard on a non-player", thing, FlagWizard, ErrBadFlags},
		{"unknown bit", thing, 1 << 20, ErrBadFlags},
		{"wizard on a player", f.owner, FlagPlayer | FlagWizard, nil},
		{"freeze a thing", thing, FlagFrozen | FlagFertile, nil},
		{"recycled object", 9999, 0, ErrNoSuchObject},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantErr(t, f.w.SetFlags(c.obj, c.flags), c.want) })
	}
	checkWorld(t, f.w)
}

// TestCreateRules covers Create's validation.
func TestCreateRules(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	f.w.recycled[f.w.nextID] = true
	gone := f.w.nextID
	f.w.nextID++

	cases := []struct {
		name   string
		parent ObjID
		owner  PlayerID
		flags  ObjFlags
		want   error
	}{
		{"self-owned player", Nothing, Nothing, FlagPlayer, nil},
		{"wizard player", Nothing, Nothing, FlagPlayer | FlagWizard, nil},
		{"thing owned by a player", thing, f.owner, 0, nil},
		{"non-player wizard", Nothing, f.wiz, FlagWizard, ErrBadFlags},
		{"unknown flag", Nothing, f.wiz, 1 << 20, ErrBadFlags},
		{"no owner for a non-player", Nothing, Nothing, 0, ErrBadOwner},
		{"owner is not a player", Nothing, thing, 0, ErrBadOwner},
		{"owner is recycled", Nothing, gone, 0, ErrRecycled},
		{"parent is recycled", gone, f.wiz, 0, ErrRecycled},
		{"parent never existed", 9999, f.wiz, 0, ErrNoSuchObject},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := f.w.NextID()
			id, err := f.w.Create(c.parent, c.owner, "x", c.flags)
			wantErr(t, err, c.want)
			if c.want != nil && f.w.NextID() != before {
				t.Error("a failed Create used up an ID")
			}
			if c.want == nil && c.owner == Nothing {
				if o, _ := f.w.Object(id); o.Owner != id {
					t.Errorf("player %v is owned by %v, want itself", id, o.Owner)
				}
			}
		})
	}
	checkWorld(t, f.w)
}

// TestRecycledReferences: once an ID is recycled, nothing may point at it,
// whether by moving into it, inheriting from it, or naming it in a value.
func TestRecycledReferences(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	must(t, f.w.DefineProp(thing, "target", prop(f.owner, PropRead, `null`)))
	// Mark an ID recycled directly: the recycle mutator comes at M7.
	gone := f.w.nextID
	f.w.nextID++
	f.w.recycled[gone] = true
	g := gone.String()

	t.Run("move into it", func(t *testing.T) { wantErr(t, f.w.Move(thing, gone), ErrRecycled) })
	t.Run("move it", func(t *testing.T) { wantErr(t, f.w.Move(gone, f.room), ErrRecycled) })
	t.Run("inherit from it", func(t *testing.T) {
		_, err := f.w.Create(gone, f.owner, "x", 0)
		wantErr(t, err, ErrRecycled)
	})
	t.Run("set flags", func(t *testing.T) { wantErr(t, f.w.SetFlags(gone, 0), ErrRecycled) })
	t.Run("define a property on it", func(t *testing.T) {
		wantErr(t, f.w.DefineProp(gone, "x", prop(f.owner, 0, `1`)), ErrRecycled)
	})
	t.Run("set a property on it", func(t *testing.T) {
		wantErr(t, f.w.SetProp(gone, "x", JSONValue(`1`)), ErrRecycled)
	})
	t.Run("add a method to it", func(t *testing.T) {
		wantErr(t, f.w.SetMethod(gone, nlMethod("m", f.owner, 0, "x")), ErrRecycled)
	})

	values := []struct {
		name  string
		value string
		want  error
	}{
		{"bare reference", `"` + g + `"`, ErrRecycled},
		{"in a list", `[1, "a", "` + g + `"]`, ErrRecycled},
		{"in a map value", `{"to": "` + g + `"}`, ErrRecycled},
		{"deeply nested", `{"a": [{"b": {"c": ["` + g + `"]}}]}`, ErrRecycled},
		{"never allocated", `"#99999"`, ErrNoSuchObject},
		{"as a map key is not a reference", `{"` + g + `": 1}`, nil},
		{"nothing (#-1) is fine", `"#-1"`, nil},
		{"live reference", `"` + f.room.String() + `"`, nil},
		{"non-canonical spelling is just a string", `"#00` + g[1:] + `"`, nil},
		{"inside prose is just a string", `"see ` + g + ` for details"`, nil},
	}
	for _, v := range values {
		t.Run("SetProp "+v.name, func(t *testing.T) {
			wantErr(t, f.w.SetProp(thing, "target", JSONValue(v.value)), v.want)
		})
		t.Run("DefineProp "+v.name, func(t *testing.T) {
			wantErr(t, f.w.DefineProp(thing, "fresh", prop(f.owner, 0, v.value)), v.want)
			delete(f.w.objects[thing].Props, "fresh") // so the next case can define it again
		})
	}
	checkWorld(t, f.w)
}

// TestRefsInValue tests the reference scanner directly.
func TestRefsInValue(t *testing.T) {
	cases := []struct {
		value string
		want  []ObjID
	}{
		{`"#5"`, []ObjID{5}},
		{`"#-1"`, nil},
		{`"#0"`, []ObjID{0}},
		{`["#1", "#2", 3, true, null]`, []ObjID{1, 2}},
		{`{"#1": "#2", "k": {"#3": ["#4"]}, "z": "#5"}`, []ObjID{2, 4, 5}},
		{`[{"a": "#1"}, {"b": "#2"}]`, []ObjID{1, 2}},
		{`{"a": {"b": 1}, "c": "#7"}`, []ObjID{7}},
		{`"#07"`, nil},
		{`"#"`, nil},
		{`"lamp"`, nil},
		{`12345678901234567890`, nil},
	}
	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			got, err := refsInValue(JSONValue(c.value))
			must(t, err)
			if !slices.Equal(got, c.want) {
				t.Fatalf("refsInValue(%s) = %v, want %v", c.value, got, c.want)
			}
		})
	}
	if _, err := refsInValue(JSONValue(`[1,`)); err == nil {
		t.Error("truncated JSON accepted")
	}
}

// TestBadValues: property values must be valid JSON.
func TestBadValues(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	must(t, f.w.DefineProp(thing, "p", prop(f.owner, 0, `1`)))
	for _, v := range []string{``, `   `, `{`, `lamp`, `[1,]`} {
		wantErr(t, f.w.SetProp(thing, "p", JSONValue(v)), ErrBadValue)
		wantErr(t, f.w.DefineProp(thing, "q", prop(f.owner, 0, v)), ErrBadValue)
	}
	wantErr(t, f.w.DefineProp(thing, "q", Property{Value: JSONValue(`1`), Owner: f.owner, Schema: JSONValue(`{`)}), ErrBadValue)
	wantErr(t, f.w.DefineProp(thing, "q", Property{Value: JSONValue(`1`), Owner: f.owner, Perms: 1 << 6}), ErrBadValue)
	wantErr(t, f.w.DefineProp(thing, "q", prop(thing, 0, `1`)), ErrBadOwner) // owner not a player
	wantErr(t, f.w.SetProp(thing, "nope", JSONValue(`1`)), ErrNoSuchProperty)
	wantErr(t, f.w.SetProp(thing, "location", JSONValue(`"#1"`)), ErrNotWritable)
	must(t, f.w.DefineProp(thing, "q", Property{Value: JSONValue(`1`), Owner: f.owner, Schema: JSONValue(`{"type":"number"}`)}))
	checkWorld(t, f.w)
}

// TestSetMethodRules covers SetMethod's validation and defaults.
func TestSetMethodRules(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	rule := JSONValue(`{"anything":"until §17.9 is settled"}`)
	cases := []struct {
		name string
		m    Method
		want error
	}{
		{"nl method", Method{Name: "rub", Owner: f.owner, Body: "Glow."}, nil},
		{"mechanical method", Method{Name: "take", Owner: f.owner, Rule: rule}, nil},
		{"both kinds", Method{Name: "x", Owner: f.owner, Rule: rule, Body: "Glow."}, ErrBadMethod},
		{"neither kind", Method{Name: "x", Owner: f.owner}, ErrBadMethod},
		{"null rule and no body", Method{Name: "x", Owner: f.owner, Rule: JSONValue(` null `)}, ErrBadMethod},
		{"null rule with body is NL", Method{Name: "x", Owner: f.owner, Rule: JSONValue(`null`), Body: "Glow."}, nil},
		{"rule not JSON", Method{Name: "x", Owner: f.owner, Rule: JSONValue(`{`)}, ErrBadMethod},
		{"contract not JSON", Method{Name: "x", Owner: f.owner, Body: "b", Contract: JSONValue(`{`)}, ErrBadMethod},
		{"unknown perms", Method{Name: "x", Owner: f.owner, Body: "b", Perms: 1 << 5}, ErrBadMethod},
		{"bad care", Method{Name: "x", Owner: f.owner, Body: "b", Care: "extreme"}, ErrBadMethod},
		{"negative budget", Method{Name: "x", Owner: f.owner, Body: "b", Budget: -1}, ErrBadMethod},
		{"no name", Method{Owner: f.owner, Body: "b"}, ErrBadName},
		{"owner not a player", Method{Name: "x", Owner: thing, Body: "b"}, ErrBadOwner},
		{"owner never existed", Method{Name: "x", Owner: 9999, Body: "b"}, ErrBadOwner},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantErr(t, f.w.SetMethod(thing, c.m), c.want) })
	}

	// Defaults from §5, and an explicit care level and budget kept as given.
	m, _, err := f.w.LookupMethod(thing, "rub")
	must(t, err)
	if m.Care != CareNormal || m.Budget != DefaultBudget {
		t.Errorf("defaults: care %q budget %d", m.Care, m.Budget)
	}
	must(t, f.w.SetMethod(thing, Method{Name: "wish", Owner: f.owner, Body: "Grant it.", Care: CareHigh, Budget: 2}))
	m, _, _ = f.w.LookupMethod(thing, "wish")
	if m.Care != CareHigh || m.Budget != 2 {
		t.Errorf("explicit: care %q budget %d", m.Care, m.Budget)
	}

	// The stored method does not share the caller's slices.
	reads := []string{"this.songs"}
	must(t, f.w.SetMethod(thing, Method{Name: "open", Owner: f.owner, Body: "Play.", Reads: reads}))
	reads[0] = "changed"
	m, _, _ = f.w.LookupMethod(thing, "open")
	if m.Reads[0] != "this.songs" {
		t.Error("stored method shares the caller's Reads slice")
	}
	checkWorld(t, f.w)
}

// TestCheckInvariantsDetectsCorruption corrupts a valid world in each way
// the mutators prevent, directly through the unexported fields, and checks
// CheckInvariants reports each one.
func TestCheckInvariantsDetectsCorruption(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(f *fixture, a, b ObjID)
		want    error
	}{
		{"self containment", func(f *fixture, a, b ObjID) {
			f.w.objects[a].Location = a
			f.w.objects[a].Contents = []ObjID{a}
		}, ErrContainmentCycle},
		{"two-object loop", func(f *fixture, a, b ObjID) {
			f.w.objects[a].Location, f.w.objects[b].Location = b, a
			f.w.objects[a].Contents, f.w.objects[b].Contents = []ObjID{b}, []ObjID{a}
		}, ErrContainmentCycle},
		{"player in a thing", func(f *fixture, a, b ObjID) {
			f.w.objects[f.owner].Location = a
			f.w.objects[a].Contents = []ObjID{f.owner}
		}, ErrPlayerPlacement},
		{"contents lists a stranger", func(f *fixture, a, b ObjID) {
			f.w.objects[a].Contents = []ObjID{b}
		}, ErrInconsistent},
		{"contents lists twice", func(f *fixture, a, b ObjID) {
			f.w.objects[b].Location = a
			f.w.objects[a].Contents = []ObjID{b, b}
		}, ErrInconsistent},
		{"contents misses an object", func(f *fixture, a, b ObjID) {
			f.w.objects[b].Location = a
		}, ErrInconsistent},
		{"location recycled", func(f *fixture, a, b ObjID) {
			f.w.recycled[f.w.nextID] = true
			f.w.objects[a].Location = f.w.nextID
			f.w.nextID++
		}, ErrRecycled},
		{"owner not a player", func(f *fixture, a, b ObjID) { f.w.objects[a].Owner = b }, ErrBadOwner},
		{"non-player wizard", func(f *fixture, a, b ObjID) { f.w.objects[a].Flags = FlagWizard }, ErrBadFlags},
		{"unknown flag", func(f *fixture, a, b ObjID) { f.w.objects[a].Flags = 1 << 20 }, ErrBadFlags},
		{"stored under the wrong ID", func(f *fixture, a, b ObjID) { f.w.objects[a].ID = b }, ErrInconsistent},
		{"reserved property name", func(f *fixture, a, b ObjID) {
			f.w.objects[a].Props["location"] = prop(f.owner, 0, `1`)
		}, ErrBadName},
		{"invalid property JSON", func(f *fixture, a, b ObjID) {
			f.w.objects[a].Props["p"] = prop(f.owner, 0, `{`)
		}, ErrBadValue},
		{"property owner not a player", func(f *fixture, a, b ObjID) {
			f.w.objects[a].Props["p"] = prop(b, 0, `1`)
		}, ErrBadOwner},
		{"malformed method", func(f *fixture, a, b ObjID) {
			f.w.objects[a].Methods["m"] = Method{Name: "m", Owner: f.owner}
		}, ErrBadMethod},
		{"method stored under another name", func(f *fixture, a, b ObjID) {
			f.w.objects[a].Methods["m"] = nlMethod("n", f.owner, 0, "b")
		}, ErrBadMethod},
		{"method owner not a player", func(f *fixture, a, b ObjID) {
			f.w.objects[a].Methods["m"] = nlMethod("m", b, 0, "b")
		}, ErrBadOwner},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			a := f.create(t, Nothing, f.owner, "a", 0)
			b := f.create(t, Nothing, f.owner, "b", 0)
			checkWorld(t, f.w)
			c.corrupt(f, a, b)
			wantErr(t, f.w.CheckInvariants(), c.want)
		})
	}
}

// TestMoveGuardsAgainstCorruption: if containment were ever already looped
// (which the public API cannot do), Move must fail rather than spin forever.
func TestMoveGuardsAgainstCorruption(t *testing.T) {
	f := newFixture(t)
	a := f.create(t, Nothing, f.owner, "a", 0)
	b := f.create(t, Nothing, f.owner, "b", 0)
	c := f.create(t, Nothing, f.owner, "c", 0)
	f.w.objects[a].Location, f.w.objects[b].Location = b, a // a and b inside each other
	f.w.objects[a].Contents, f.w.objects[b].Contents = []ObjID{b}, []ObjID{a}
	wantErr(t, f.w.Move(c, a), ErrContainmentCycle)
}
