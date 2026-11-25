// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

package world

import "fmt"

// This file implements single-inheritance lookup (§4.1): an object has every
// property and method of its parent chain, and its own slot of a given name,
// if it has one, overrides the inherited one. Lookup therefore walks from the
// object up through its parents and returns the first slot it finds.

// walkChain calls visit for id and then each of its ancestors, nearest first,
// until visit returns true or the chain ends at Nothing. It returns
// ErrInheritanceCycle if the chain loops, which the invariants make
// impossible in a World built through Create and Load but which this loop
// guards against anyway so a bug elsewhere cannot hang the server. An
// ancestor that is not live is reported as an inconsistency for the same
// reason.
func (w *World) walkChain(id ObjID, visit func(*Object) bool) error {
	o, err := w.live(id)
	if err != nil {
		return err
	}
	// A chain with no cycle visits each live object at most once, so any
	// walk longer than the number of live objects must be going round.
	for steps := 0; ; steps++ {
		if steps > len(w.objects) {
			return fmt.Errorf("%w: the parent chain of %v loops", ErrInheritanceCycle, id)
		}
		if visit(o) {
			return nil
		}
		if o.Parent == Nothing {
			return nil
		}
		next := w.objects[o.Parent]
		if next == nil {
			return fmt.Errorf("%w: %v has parent %v, which is not a live object", ErrInconsistent, o.ID, o.Parent)
		}
		o = next
	}
}

// Ancestors returns id's parent chain, nearest first, not including id
// itself. A root object has no ancestors.
func (w *World) Ancestors(id ObjID) ([]ObjID, error) {
	var out []ObjID
	err := w.walkChain(id, func(o *Object) bool {
		if o.ID != id {
			out = append(out, o.ID)
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// isAncestor reports whether anc is id itself or one of id's ancestors.
func (w *World) isAncestor(anc, id ObjID) bool {
	found := false
	_ = w.walkChain(id, func(o *Object) bool { // errors just mean "not found"
		found = o.ID == anc
		return found
	})
	return found
}

// LookupProp finds property name as seen from obj: obj's own slot if it has
// one, otherwise the nearest ancestor's. It returns the slot and the ID of
// the object that holds it. The returned Property is a copy of the struct,
// but its Value and Schema share bytes with the World's copy, so treat them
// as read-only.
//
// LookupProp knows nothing of the built-in names ("name", "location", ...);
// they are not property slots. It returns ErrNoSuchProperty for them.
func (w *World) LookupProp(obj ObjID, name string) (Property, ObjID, error) {
	var (
		found Property
		where = Nothing
	)
	err := w.walkChain(obj, func(o *Object) bool {
		if p, ok := o.Props[name]; ok {
			found, where = p, o.ID
			return true
		}
		return false
	})
	if err != nil {
		return Property{}, Nothing, err
	}
	if where == Nothing {
		return Property{}, Nothing, fmt.Errorf("%w: %v.%s", ErrNoSuchProperty, obj, name)
	}
	return found, where, nil
}

// LookupMethod finds method name as seen from obj, exactly as LookupProp
// does for properties: obj's own method if it has one, otherwise the nearest
// ancestor's. It returns the method and the ID of the object defining it.
// The Rule, Contract and Reads of the returned Method share memory with the
// World's copy, so treat them as read-only.
func (w *World) LookupMethod(obj ObjID, name string) (Method, ObjID, error) {
	var (
		found Method
		where = Nothing
	)
	err := w.walkChain(obj, func(o *Object) bool {
		if m, ok := o.Methods[name]; ok {
			found, where = m, o.ID
			return true
		}
		return false
	})
	if err != nil {
		return Method{}, Nothing, err
	}
	if where == Nothing {
		return Method{}, Nothing, fmt.Errorf("%w: %v:%s", ErrNoSuchMethod, obj, name)
	}
	return found, where, nil
}

// effectiveProps returns every property obj has, own or inherited, keyed by
// name, each being the slot LookupProp would return for that name. Create
// uses it to apply the c bit. Walking nearest first and keeping only the
// first slot seen for each name is what makes an override win over the
// definition it overrides.
func (w *World) effectiveProps(obj ObjID) map[string]Property {
	out := make(map[string]Property)
	_ = w.walkChain(obj, func(o *Object) bool { // obj is known live here
		for name, p := range o.Props {
			if _, seen := out[name]; !seen {
				out[name] = p
			}
		}
		return false
	})
	return out
}
