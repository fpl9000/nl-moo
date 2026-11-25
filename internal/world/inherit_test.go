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

// getProp looks up obj.name and returns its value as a string, the holder's
// ID and the slot's owner, failing the test if the lookup fails.
func getProp(t *testing.T, w *World, obj ObjID, name string) (string, ObjID, PlayerID) {
	t.Helper()
	p, holder, err := w.LookupProp(obj, name)
	if err != nil {
		t.Fatalf("LookupProp(%v, %q): %v", obj, name, err)
	}
	return string(p.Value), holder, p.Owner
}

// TestPropertyInheritanceAndOverride: a child sees its parent's value live
// until it is written; writing the child creates an override on the child,
// owned like the inherited slot, and leaves the parent alone.
func TestPropertyInheritanceAndOverride(t *testing.T) {
	f := newFixture(t)
	proto := f.create(t, Nothing, f.wiz, "generic lamp", FlagFertile)
	must(t, f.w.DefineProp(proto, "lit", prop(f.wiz, PropRead, `false`)))
	lamp := f.create(t, proto, f.owner, "brass lamp", 0)

	if v, holder, owner := getProp(t, f.w, lamp, "lit"); v != "false" || holder != proto || owner != f.wiz {
		t.Fatalf("inherited: got %s from %v owned by %v", v, holder, owner)
	}

	// No c bit, so the child has no slot of its own and sees changes to the
	// parent's value.
	must(t, f.w.SetProp(proto, "lit", JSONValue(`"flicker"`)))
	if v, _, _ := getProp(t, f.w, lamp, "lit"); v != `"flicker"` {
		t.Fatalf("live inheritance: got %s", v)
	}

	// Writing the child overrides for the child only.
	must(t, f.w.SetProp(lamp, "lit", JSONValue(`true`)))
	if v, holder, owner := getProp(t, f.w, lamp, "lit"); v != "true" || holder != lamp || owner != f.wiz {
		t.Fatalf("override: got %s from %v owned by %v; want true from the lamp, still owned by the wizard", v, holder, owner)
	}
	if v, _, _ := getProp(t, f.w, proto, "lit"); v != `"flicker"` {
		t.Fatalf("parent changed by child write: %s", v)
	}

	// Once overridden, the child no longer follows the parent.
	must(t, f.w.SetProp(proto, "lit", JSONValue(`false`)))
	if v, _, _ := getProp(t, f.w, lamp, "lit"); v != "true" {
		t.Fatalf("override lost: got %s", v)
	}

	// The override keeps the inherited slot's owner, so the lamp's owner
	// still may not write it; the wizard-owned prototype's methods may.
	expect(t, f.w.CheckWriteProp(f.owner, lamp, "lit"), false)
	checkWorld(t, f.w)
}

// TestCopyBit: a property with the c bit is copied into each new child at
// creation, the copy belongs to the child's owner, and later changes on
// either side do not reach the other.
func TestCopyBit(t *testing.T) {
	f := newFixture(t)
	proto := f.create(t, Nothing, f.wiz, "generic container", FlagFertile)
	must(t, f.w.DefineProp(proto, "open", prop(f.wiz, PropRead|PropCopy, `false`)))
	must(t, f.w.DefineProp(proto, "capacity", prop(f.wiz, PropRead, `10`))) // no c
	chest := f.create(t, proto, f.owner, "chest", 0)

	o, _ := f.w.Object(chest)
	slot, ok := o.Props["open"]
	if !ok {
		t.Fatal("c property was not copied into the child")
	}
	if string(slot.Value) != "false" || slot.Owner != f.owner || slot.Perms != PropRead|PropCopy {
		t.Fatalf("copied slot = %+v; want value false, owner %v, perms rc", slot, f.owner)
	}
	if _, ok := o.Props["capacity"]; ok {
		t.Fatal("property without c was copied")
	}

	// The copy is the chest owner's, so the owner may write it directly.
	expect(t, f.w.CheckWriteProp(f.owner, chest, "open"), true)
	expect(t, f.w.CheckWriteProp(f.owner, chest, "capacity"), false)

	// Changing the prototype does not reach the existing copy...
	must(t, f.w.SetProp(proto, "open", JSONValue(`true`)))
	if v, _, _ := getProp(t, f.w, chest, "open"); v != "false" {
		t.Fatalf("copy followed the parent: %s", v)
	}
	// ...and changing the copy does not reach the prototype.
	must(t, f.w.SetProp(chest, "open", JSONValue(`"ajar"`)))
	if v, _, _ := getProp(t, f.w, proto, "open"); v != "true" {
		t.Fatalf("parent followed the copy: %s", v)
	}
	// The copy does not share bytes with the parent's slot.
	po, _ := f.w.Object(proto)
	po.Props["open"].Value[0] = 'X' // deliberately corrupt the parent's bytes
	if v, _, _ := getProp(t, f.w, chest, "open"); v != `"ajar"` {
		t.Fatalf("copy shares memory with the parent: %s", v)
	}
	must(t, f.w.SetProp(proto, "open", JSONValue(`true`))) // repair it
	checkWorld(t, f.w)
}

// TestCopyBitThroughChain: the c bit applies to the value the new object
// would inherit, which is the nearest override, not the original definition.
func TestCopyBitThroughChain(t *testing.T) {
	f := newFixture(t)
	root := f.create(t, Nothing, f.wiz, "root", FlagFertile)
	must(t, f.w.DefineProp(root, "color", prop(f.wiz, PropRead|PropCopy, `"red"`)))
	mid := f.create(t, root, f.owner, "mid", FlagFertile) // gets a copy: "red"
	must(t, f.w.SetProp(mid, "color", JSONValue(`"blue"`)))
	leaf := f.create(t, mid, f.other, "leaf", 0)
	v, holder, owner := getProp(t, f.w, leaf, "color")
	if v != `"blue"` || holder != leaf || owner != f.other {
		t.Fatalf("leaf color = %s from %v owned by %v; want \"blue\" copied to the leaf, owned by %v", v, holder, owner, f.other)
	}
}

// TestDeepParentChain: lookup works through a long chain, finding the
// nearest definition or override.
func TestDeepParentChain(t *testing.T) {
	const depth = 2000
	f := newFixture(t)
	chain := []ObjID{f.create(t, Nothing, f.wiz, "root", FlagFertile)}
	must(t, f.w.DefineProp(chain[0], "depth", prop(f.wiz, PropRead, `0`)))
	must(t, f.w.SetMethod(chain[0], nlMethod("greet", f.wiz, MethodExec, "Say hello.")))
	for i := 1; i <= depth; i++ {
		chain = append(chain, f.create(t, chain[i-1], f.wiz, "link", FlagFertile))
	}
	leaf := chain[depth]

	if v, holder, _ := getProp(t, f.w, leaf, "depth"); v != "0" || holder != chain[0] {
		t.Fatalf("leaf sees %s from %v", v, holder)
	}
	if m, holder, err := f.w.LookupMethod(leaf, "greet"); err != nil || holder != chain[0] || m.Body != "Say hello." {
		t.Fatalf("method from leaf: %+v from %v, %v", m, holder, err)
	}

	// Override halfway down: the leaf now sees the middle, the root's
	// upper half still sees the root.
	mid := chain[depth/2]
	must(t, f.w.SetProp(mid, "depth", JSONValue(`1000`)))
	must(t, f.w.SetMethod(mid, nlMethod("greet", f.owner, MethodExec, "Wave.")))
	if v, holder, _ := getProp(t, f.w, leaf, "depth"); v != "1000" || holder != mid {
		t.Fatalf("leaf after override sees %s from %v", v, holder)
	}
	if v, holder, _ := getProp(t, f.w, chain[depth/4], "depth"); v != "0" || holder != chain[0] {
		t.Fatalf("upper chain sees %s from %v", v, holder)
	}
	if m, holder, _ := f.w.LookupMethod(leaf, "greet"); holder != mid || m.Owner != f.owner {
		t.Fatalf("method after override: %+v from %v", m, holder)
	}

	anc, err := f.w.Ancestors(leaf)
	must(t, err)
	if len(anc) != depth || anc[0] != chain[depth-1] || anc[depth-1] != chain[0] {
		t.Fatalf("Ancestors: len %d, first %v, last %v", len(anc), anc[0], anc[len(anc)-1])
	}
	checkWorld(t, f.w)
}

// TestMethodOverride: a child's method of the same name replaces the
// parent's for the child and its descendants, not for the parent.
func TestMethodOverride(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.wiz, "generic thing", FlagFertile)
	must(t, f.w.SetMethod(thing, Method{Name: "take", Owner: f.wiz, Perms: MethodExec, Rule: JSONValue(`{"opaque":true}`)}))
	lamp := f.create(t, thing, f.owner, "lamp", FlagFertile)
	must(t, f.w.SetMethod(lamp, nlMethod("take", f.owner, MethodExec, "The lamp resists unless rubbed first.")))
	small := f.create(t, lamp, f.owner, "small lamp", 0)

	for _, c := range []struct {
		obj, holder ObjID
		kind        MethodKind
	}{
		{thing, thing, Mechanical},
		{lamp, lamp, NaturalLanguage},
		{small, lamp, NaturalLanguage},
	} {
		m, holder, err := f.w.LookupMethod(c.obj, "take")
		must(t, err)
		kind, err := m.Kind()
		must(t, err)
		if holder != c.holder || kind != c.kind {
			t.Errorf("%v:take from %v (%v), want from %v (%v)", c.obj, holder, kind, c.holder, c.kind)
		}
	}
	// Replacing the child's own method works and does not touch the parent.
	must(t, f.w.SetMethod(lamp, nlMethod("take", f.owner, MethodExec, "Take it gently.")))
	if m, _, _ := f.w.LookupMethod(small, "take"); m.Body != "Take it gently." {
		t.Errorf("replacement not seen: %q", m.Body)
	}
	if m, _, _ := f.w.LookupMethod(thing, "take"); m.Body != "" {
		t.Errorf("parent's method changed")
	}
}

// TestDefinePropConflicts: a property name may be defined only once along
// any parent chain.
func TestDefinePropConflicts(t *testing.T) {
	f := newFixture(t)
	root := f.create(t, Nothing, f.wiz, "root", FlagFertile)
	mid := f.create(t, root, f.wiz, "mid", FlagFertile)
	leaf := f.create(t, mid, f.wiz, "leaf", 0)
	sibling := f.create(t, root, f.wiz, "sibling", 0)
	must(t, f.w.DefineProp(mid, "color", prop(f.wiz, PropRead, `"red"`)))

	cases := []struct {
		name string
		obj  ObjID
		prop string
		want error
	}{
		{"same object", mid, "color", ErrPropertyExists},
		{"descendant redefines", leaf, "color", ErrPropertyExists},
		{"ancestor defines what a descendant has", root, "color", ErrPropertyExists},
		{"unrelated branch is fine", sibling, "color", nil},
		{"empty name", leaf, "", ErrBadName},
		{"reserved: name", leaf, "name", ErrBadName},
		{"reserved: location", leaf, "location", ErrBadName},
		{"reserved: owner", leaf, "owner", ErrBadName},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantErr(t, f.w.DefineProp(c.obj, c.prop, prop(f.wiz, PropRead, `1`)), c.want)
		})
	}
	checkWorld(t, f.w)
}

// TestLookupErrors covers lookups that find nothing.
func TestLookupErrors(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	_, _, err := f.w.LookupProp(thing, "nope")
	wantErr(t, err, ErrNoSuchProperty)
	_, _, err = f.w.LookupMethod(thing, "nope")
	wantErr(t, err, ErrNoSuchMethod)
	_, _, err = f.w.LookupProp(9999, "x")
	wantErr(t, err, ErrNoSuchObject)
	_, _, err = f.w.LookupMethod(Nothing, "x")
	wantErr(t, err, ErrNoSuchObject)
	_, err = f.w.Ancestors(9999)
	wantErr(t, err, ErrNoSuchObject)
	if anc, err := f.w.Ancestors(thing); err != nil || len(anc) != 0 {
		t.Errorf("root object ancestors = %v, %v", anc, err)
	}
	// Built-in names are not property slots.
	_, _, err = f.w.LookupProp(thing, "name")
	wantErr(t, err, ErrNoSuchProperty)
}

// TestLookupGuardsAgainstCorruption: if the parent chain were ever corrupted
// (which the public API cannot do), lookup must fail rather than loop
// forever or crash. These tests corrupt the world directly.
func TestLookupGuardsAgainstCorruption(t *testing.T) {
	t.Run("cycle", func(t *testing.T) {
		f := newFixture(t)
		a := f.create(t, Nothing, f.wiz, "a", FlagFertile)
		b := f.create(t, a, f.wiz, "b", FlagFertile)
		f.w.objects[a].Parent = b // a -> b -> a
		_, _, err := f.w.LookupProp(b, "x")
		wantErr(t, err, ErrInheritanceCycle)
		_, _, err = f.w.LookupMethod(a, "x")
		wantErr(t, err, ErrInheritanceCycle)
		_, err = f.w.Ancestors(a)
		wantErr(t, err, ErrInheritanceCycle)
		wantErr(t, f.w.CheckInvariants(), ErrInheritanceCycle)
	})
	t.Run("dead parent", func(t *testing.T) {
		f := newFixture(t)
		a := f.create(t, Nothing, f.wiz, "a", 0)
		f.w.objects[a].Parent = 9999
		_, _, err := f.w.LookupProp(a, "x")
		wantErr(t, err, ErrInconsistent)
		wantErr(t, f.w.CheckInvariants(), ErrNoSuchObject)
	})
}

// TestAncestorsOrder checks Ancestors lists nearest first.
func TestAncestorsOrder(t *testing.T) {
	f := newFixture(t)
	a := f.create(t, Nothing, f.wiz, "a", FlagFertile)
	b := f.create(t, a, f.wiz, "b", FlagFertile)
	c := f.create(t, b, f.wiz, "c", 0)
	anc, err := f.w.Ancestors(c)
	must(t, err)
	if !slices.Equal(anc, []ObjID{b, a}) {
		t.Fatalf("Ancestors(c) = %v, want [%v %v]", anc, b, a)
	}
}
