// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

package world

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// This file holds the engine invariants of §6, which hold no matter what an
// interpreter proposes or who asks:
//
//  1. Containment is a tree: no object is inside itself, directly or
//     transitively.
//  2. Recycled objects cannot be referenced.
//  3. Players are only ever in rooms or vehicles.
//
// (§6's fourth invariant, that property values satisfy their declared
// schemas, arrives with the validator at M2.)
//
// The per-operation checks here are used by World's mutators, so no mutation
// can break an invariant. CheckInvariants re-checks a whole world, which Load
// uses on data read from disk and the tests use after every scenario.

// canHoldPlayers reports whether an object with these flags may have players
// directly inside it.
func canHoldPlayers(f ObjFlags) bool {
	return f&(FlagRoom|FlagVehicle) != 0
}

// checkMove verifies that moving obj into dest keeps every invariant, and
// returns obj's live Object if so.
//
// Moving into Nothing is always allowed, players included: LambdaMOO puts
// objects (and disconnected or newly created players) "nowhere", and §6's
// placement rule is about which objects a player may be inside.
func (w *World) checkMove(obj, dest ObjID) (*Object, error) {
	o, err := w.live(obj)
	if err != nil {
		return nil, err
	}
	if dest == Nothing {
		return o, nil
	}
	d, err := w.live(dest)
	if err != nil {
		return nil, fmt.Errorf("destination: %w", err)
	}

	// Invariant 1. Walk up from the destination through its containers to
	// the top of the tree. If obj is on that path (or is dest itself), the
	// move would put obj inside itself. The step limit guards against a
	// cycle that already exists, which the invariants rule out but which
	// must not hang the server if it ever happened.
	for c, steps := d, 0; c != nil; c, steps = w.objects[c.Location], steps+1 {
		if c.ID == obj {
			return nil, fmt.Errorf("%w: moving %v into %v would put it inside itself", ErrContainmentCycle, obj, dest)
		}
		if steps > len(w.objects) {
			return nil, fmt.Errorf("%w: the containers of %v loop", ErrContainmentCycle, dest)
		}
	}

	// Invariant 3.
	if o.Flags.Has(FlagPlayer) && !canHoldPlayers(d.Flags) {
		return nil, fmt.Errorf("%w: cannot move player %v into %v, which is neither a room nor a vehicle", ErrPlayerPlacement, obj, dest)
	}
	return o, nil
}

// checkValue verifies that v is valid JSON and that every object reference in
// it names a live object (invariant 2). It is used on every property write.
//
// Refusing references to never-allocated IDs as well as recycled ones is
// deliberate: an ID beyond NextID would later start naming whatever object
// happened to be created with it.
func (w *World) checkValue(v JSONValue) error {
	if len(bytes.TrimSpace(v)) == 0 || !json.Valid(v) {
		return fmt.Errorf("%w: not valid JSON: %q", ErrBadValue, v)
	}
	refs, err := refsInValue(v)
	if err != nil {
		return err
	}
	for _, id := range refs {
		if _, err := w.live(id); err != nil {
			return fmt.Errorf("value refers to a dead object: %w", err)
		}
	}
	return nil
}

// refsInValue returns the object references in a JSON value: every JSON
// string, at any depth, that is exactly an ObjID in canonical MOO notation
// ("#1023") other than #-1, which means "nothing" rather than naming an
// object. Map keys are not references, only values.
//
// This is how §4.2's "ObjID ref" value type is represented in JSON at M1. It
// uses the same "#123" notation as effects documents (§7.3) and the building
// commands (§12.2). It is a proposal recorded in docs/decisions.md, to be
// confirmed before M3 persists values in this form.
func refsInValue(v JSONValue) ([]ObjID, error) {
	// A json.Decoder reads the value token by token, so we can see every
	// string without building a tree. UseNumber stops large integers from
	// being rounded through float64, though only strings matter here.
	dec := json.NewDecoder(bytes.NewReader(v))
	dec.UseNumber()

	// Inside a JSON object, tokens alternate key, value, key, value. The
	// stack records, for each open container, whether it is an object and,
	// if so, whether the next token is a key.
	type frame struct{ object, expectKey bool }
	var stack []frame

	var refs []ObjID
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			if len(stack) > 0 { // the value ended inside an open list or map
				return nil, fmt.Errorf("%w: truncated JSON", ErrBadValue)
			}
			return refs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadValue, err)
		}

		isKey := len(stack) > 0 && stack[len(stack)-1].object && stack[len(stack)-1].expectKey

		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				if len(stack) > 0 && stack[len(stack)-1].object {
					stack[len(stack)-1].expectKey = true // a value just started
				}
				stack = append(stack, frame{object: t == '{', expectKey: t == '{'})
			case '}', ']':
				stack = stack[:len(stack)-1]
			}
			continue
		case string:
			if !isKey {
				if id, err := ParseObjID(t); err == nil && id != Nothing {
					refs = append(refs, id)
				}
			}
		}

		// After a key, a value follows; after a value, a key follows.
		if len(stack) > 0 && stack[len(stack)-1].object {
			stack[len(stack)-1].expectKey = !isKey
		}
	}
}

// CheckInvariants checks a whole world: every invariant, plus the internal
// consistency the mutators maintain (Contents is exactly the inverse of
// Location, owners are live players, parent chains end at Nothing, and every
// method is well-formed). It returns nil if all is well, or every problem
// found, joined with errors.Join so errors.Is works on each one.
//
// Object references inside property values are not checked here: a value may
// still name an object recycled after the value was written, as in LambdaMOO.
// Invariant 2 is enforced when a value is written (checkValue), and the
// recycling command (M7) cannot recycle an object that other objects are
// structurally attached to.
func (w *World) CheckInvariants() error {
	var errs []error
	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	for _, id := range w.sortedIDs() {
		o := w.objects[id]

		if o.ID != id {
			bad("%w: object stored as %v has ID %v", ErrInconsistent, id, o.ID)
		}
		if o.Flags&^allFlags != 0 {
			bad("%w: %v has unknown flag bits %v", ErrBadFlags, id, o.Flags)
		}
		if o.Flags.Has(FlagWizard) && !o.Flags.Has(FlagPlayer) {
			bad("%w: %v is a wizard but not a player", ErrBadFlags, id)
		}
		if _, err := w.livePlayer(o.Owner); err != nil {
			bad("owner of %v: %w", id, err)
		}

		// The parent chain must end at Nothing through live objects. A chain
		// that is too long to be acyclic, or that reaches a dead object, is
		// reported by walkChain.
		if o.Parent != Nothing {
			if _, err := w.live(o.Parent); err != nil {
				bad("parent of %v: %w", id, err)
			} else if err := w.walkChain(id, func(*Object) bool { return false }); err != nil {
				bad("%w", err)
			}
		}

		// Containment: the location must be Nothing or live, the chain of
		// containers must not loop (invariant 1), and players must be in a
		// room or vehicle (invariant 3).
		if o.Location != Nothing {
			c, err := w.live(o.Location)
			if err != nil {
				bad("location of %v: %w", id, err)
			} else {
				if o.Flags.Has(FlagPlayer) && !canHoldPlayers(c.Flags) {
					bad("%w: player %v is inside %v", ErrPlayerPlacement, id, c.ID)
				}
				for steps := 0; c != nil; c, steps = w.objects[c.Location], steps+1 {
					if c.ID == id || steps > len(w.objects) {
						bad("%w: %v is inside a containment loop", ErrContainmentCycle, id)
						break
					}
				}
			}
		}

		// Contents must list exactly the objects whose Location is o, once
		// each. The reverse direction is checked by counting below.
		seen := make(map[ObjID]bool, len(o.Contents))
		for _, c := range o.Contents {
			switch co := w.objects[c]; {
			case seen[c]:
				bad("%w: %v lists %v twice in its contents", ErrInconsistent, id, c)
			case co == nil || co.Location != id:
				bad("%w: %v lists %v in its contents, but %v is not there", ErrInconsistent, id, c, c)
			}
			seen[c] = true
		}

		for _, name := range sortedNames(o.Props) {
			p := o.Props[name]
			if name == "" || reservedNames[name] {
				bad("%w: %v has a property named %q", ErrBadName, id, name)
			}
			if !json.Valid(p.Value) {
				bad("%w: %v.%s is not valid JSON", ErrBadValue, id, name)
			}
			if _, err := w.livePlayer(p.Owner); err != nil {
				bad("owner of %v.%s: %w", id, name, err)
			}
		}
		for _, name := range sortedNames(o.Methods) {
			m := o.Methods[name]
			if m.Name != name {
				bad("%w: %v stores method %q under the name %q", ErrBadMethod, id, m.Name, name)
			}
			if _, err := m.Kind(); err != nil {
				bad("%v: %w", id, err)
			}
			if _, err := w.livePlayer(m.Owner); err != nil {
				bad("owner of %v:%s: %w", id, name, err)
			}
		}
	}

	// Every object inside a live container must appear in its Contents.
	for _, id := range w.sortedIDs() {
		o := w.objects[id]
		if c := w.objects[o.Location]; c != nil {
			n := 0
			for _, x := range c.Contents {
				if x == id {
					n++
				}
			}
			if n == 0 {
				bad("%w: %v is located in %v but missing from its contents", ErrInconsistent, id, c.ID)
			}
		}
	}

	return errors.Join(errs...)
}
