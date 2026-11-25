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
	"fmt"
	"slices"
	"sort"
)

// World holds every live object, remembers which IDs have been recycled, and
// allocates new IDs. It is the in-memory object database of §5 ("the entire
// world is loaded into memory at boot").
//
// A World is not safe for concurrent use. From M4 the engine's single writer
// goroutine owns it (§6, §14.3).
//
// The mutators in this file are deliberately few: just the ones needed to
// build worlds whose invariants and inheritance rules can be tested. Each
// enforces the invariants of invariants.go and none checks permissions (see
// the package comment). Recycling, re-parenting and chown get their mutators
// with the building commands at M7, where their remaining semantics (what
// happens to a recycled object's contents and children, for example) are
// decided; perms.go already has their permission rules.
type World struct {
	objects  map[ObjID]*Object // live objects only
	recycled map[ObjID]bool    // IDs of recycled objects, kept so they stay unusable
	nextID   ObjID             // the ID Create will hand out next
}

// New returns an empty world whose first object will be #0.
func New() *World {
	return &World{
		objects:  make(map[ObjID]*Object),
		recycled: make(map[ObjID]bool),
	}
}

// Load builds a world from previously saved state, as the M3 store will do at
// boot, and checks every invariant over the result (CheckInvariants). It
// returns an error, and no world, if anything is malformed.
//
// objs are the live objects; Load takes ownership of them and of their maps
// and slices. Each object's Contents is ignored and rebuilt from the Location
// fields, because Contents is derived state. recycled lists the IDs of
// recycled objects. nextID is the next ID to allocate; it must exceed every
// ID in objs and recycled, so IDs are never reused.
func Load(objs []*Object, recycled []ObjID, nextID ObjID) (*World, error) {
	w := New()
	w.nextID = nextID
	for _, id := range recycled {
		if id < 0 || id >= nextID {
			return nil, fmt.Errorf("%w: recycled ID %v is outside [#0, %v)", ErrInconsistent, id, nextID)
		}
		w.recycled[id] = true
	}
	for _, o := range objs {
		switch {
		case o == nil:
			return nil, fmt.Errorf("%w: nil object", ErrInconsistent)
		case o.ID < 0 || o.ID >= nextID:
			return nil, fmt.Errorf("%w: object ID %v is outside [#0, %v)", ErrInconsistent, o.ID, nextID)
		case w.recycled[o.ID]:
			return nil, fmt.Errorf("%w: object %v is both live and recycled", ErrInconsistent, o.ID)
		case w.objects[o.ID] != nil:
			return nil, fmt.Errorf("%w: duplicate object %v", ErrInconsistent, o.ID)
		}
		if o.Props == nil {
			o.Props = make(map[string]Property)
		}
		if o.Methods == nil {
			o.Methods = make(map[string]Method)
		}
		o.Contents = nil
		w.objects[o.ID] = o
	}

	// Rebuild Contents from Location. A saved world has no record of the
	// order things were put into a container, so contents come back in ID
	// order (oldest object first); Move then appends to the end.
	for _, id := range w.sortedIDs() {
		o := w.objects[id]
		if c := w.objects[o.Location]; c != nil {
			c.Contents = append(c.Contents, id)
		}
	}

	if err := w.CheckInvariants(); err != nil {
		return nil, err
	}
	return w, nil
}

// sortedIDs returns the IDs of all live objects in increasing order, so that
// anything iterating over the world does so deterministically. (Go randomizes
// map iteration order on purpose, to stop code depending on it.)
func (w *World) sortedIDs() []ObjID {
	ids := make([]ObjID, 0, len(w.objects))
	for id := range w.objects {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// ObjectIDs returns the IDs of all live objects in increasing order.
func (w *World) ObjectIDs() []ObjID { return w.sortedIDs() }

// RecycledIDs returns the IDs of all recycled objects in increasing order.
func (w *World) RecycledIDs() []ObjID {
	ids := make([]ObjID, 0, len(w.recycled))
	for id := range w.recycled {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// NextID returns the ID the next Create will allocate.
func (w *World) NextID() ObjID { return w.nextID }

// Object returns the live object with the given ID. The second result is
// false if there is none (never allocated, recycled, or Nothing). The object
// is the World's own copy: treat it as read-only.
func (w *World) Object(id ObjID) (*Object, bool) {
	o, ok := w.objects[id]
	return o, ok
}

// IsLive reports whether id names a live object.
func (w *World) IsLive(id ObjID) bool { return w.objects[id] != nil }

// IsRecycled reports whether id names a recycled object.
func (w *World) IsRecycled(id ObjID) bool { return w.recycled[id] }

// live returns the live object id, or an error saying why there is none:
// ErrRecycled for a recycled ID and ErrNoSuchObject otherwise. Almost every
// operation starts with this.
func (w *World) live(id ObjID) (*Object, error) {
	if o := w.objects[id]; o != nil {
		return o, nil
	}
	if w.recycled[id] {
		return nil, fmt.Errorf("%w: %v", ErrRecycled, id)
	}
	return nil, fmt.Errorf("%w: %v", ErrNoSuchObject, id)
}

// livePlayer returns the live player object id, or ErrBadOwner. It is used to
// validate owners, which §4.4 says are always players.
func (w *World) livePlayer(id ObjID) (*Object, error) {
	o, err := w.live(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadOwner, err)
	}
	if !o.Flags.Has(FlagPlayer) {
		return nil, fmt.Errorf("%w: %v is not a player", ErrBadOwner, id)
	}
	return o, nil
}

// Create allocates a new object, as LambdaMOO's create() does, and returns
// its ID. The new object starts at Location Nothing; place it with Move.
//
// parent is the prototype to inherit from, or Nothing for a root object. It
// must be live. owner must be a live player, except that a new player object
// may pass Nothing as owner to own itself, which is how player objects are
// normally owned (and how the archwizard #2 can exist at bootstrap).
//
// For every property the new object inherits whose effective permissions
// include the c bit, Create copies the inherited value into a local slot on
// the new object, owned by the new object's owner (§4.2). Properties without
// c are not copied: the child sees the ancestor's value live until something
// writes the child's own value (see SetProp).
//
// Create checks no permissions (see CheckCreateChild in perms.go) and no
// quotas (M7).
func (w *World) Create(parent ObjID, owner PlayerID, name string, flags ObjFlags) (ObjID, error) {
	if flags&^allFlags != 0 {
		return Nothing, fmt.Errorf("%w: unknown flag bits in %v", ErrBadFlags, flags)
	}
	if flags.Has(FlagWizard) && !flags.Has(FlagPlayer) {
		return Nothing, fmt.Errorf("%w: only players may be wizards", ErrBadFlags)
	}
	if parent != Nothing {
		if _, err := w.live(parent); err != nil {
			return Nothing, fmt.Errorf("parent: %w", err)
		}
	}

	id := w.nextID
	if owner == Nothing && flags.Has(FlagPlayer) {
		owner = id // a new player owns itself
	} else if _, err := w.livePlayer(owner); err != nil {
		return Nothing, err
	}

	o := &Object{
		ID:       id,
		Parent:   parent,
		Owner:    owner,
		Name:     name,
		Location: Nothing,
		Flags:    flags,
		Props:    make(map[string]Property),
		Methods:  make(map[string]Method),
	}

	// Apply the c bit. effectiveProps returns, for each property name the
	// parent chain provides, the slot that lookup would find (the nearest
	// one), which is the value and permissions the child would inherit.
	if parent != Nothing {
		for name, p := range w.effectiveProps(parent) {
			if p.Perms&PropCopy != 0 {
				p.Value = bytes.Clone(p.Value) // never share bytes between slots
				p.Owner = owner
				o.Props[name] = p
			}
		}
	}

	w.objects[id] = o
	w.nextID++
	return id, nil
}

// Move moves obj into dest (Nothing to put it nowhere), keeping both
// containers' Contents up to date. It enforces the containment-tree and
// player-placement invariants (checkMove) and checks no permissions (see
// CheckMove in perms.go). Moving an object to where it already is does
// nothing and keeps its place in the container's contents.
func (w *World) Move(obj, dest ObjID) error {
	o, err := w.checkMove(obj, dest)
	if err != nil {
		return err
	}
	if o.Location == dest {
		return nil
	}
	if old := w.objects[o.Location]; old != nil {
		// slices.DeleteFunc removes every element for which the function
		// returns true, preserving the order of the rest.
		old.Contents = slices.DeleteFunc(old.Contents, func(id ObjID) bool { return id == obj })
	}
	if d := w.objects[dest]; d != nil {
		d.Contents = append(d.Contents, obj)
	}
	o.Location = dest
	return nil
}

// SetFlags replaces obj's flags with flags. It checks no permissions (see
// CheckSetFlag in perms.go, which decides who may change each flag) but
// enforces these invariants:
//
//   - no unknown bits;
//   - the Player flag cannot change after creation, because ownership rests
//     on it (owners must be players) and the plan schedules player creation
//     and its quota bookkeeping for later milestones;
//   - only players may be wizards;
//   - an object with players directly inside it must stay a room or a
//     vehicle, or those players would be stranded in a non-room (§6).
func (w *World) SetFlags(obj ObjID, flags ObjFlags) error {
	o, err := w.live(obj)
	if err != nil {
		return err
	}
	if flags&^allFlags != 0 {
		return fmt.Errorf("%w: unknown flag bits in %v", ErrBadFlags, flags)
	}
	if flags.Has(FlagPlayer) != o.Flags.Has(FlagPlayer) {
		return fmt.Errorf("%w: the player flag of %v cannot be changed", ErrBadFlags, obj)
	}
	if flags.Has(FlagWizard) && !flags.Has(FlagPlayer) {
		return fmt.Errorf("%w: only players may be wizards", ErrBadFlags)
	}
	if !canHoldPlayers(flags) {
		for _, c := range o.Contents {
			if w.objects[c].Flags.Has(FlagPlayer) {
				return fmt.Errorf("%w: %v contains player %v, so it must stay a room or vehicle", ErrPlayerPlacement, obj, c)
			}
		}
	}
	o.Flags = flags
	return nil
}

// reservedNames are names that can never be defined as ordinary properties,
// because selectors and the built-in property set (§4.2) already give them a
// meaning. "name" and "aliases" are the Object fields of those names; the
// rest are structural attributes that change only through their own
// operations. "description", "home" and "quota" are also built-in in §4.2,
// but they are ordinary properties defined on the bootstrap prototypes (M3),
// so they are not reserved here.
var reservedNames = map[string]bool{
	"name": true, "aliases": true,
	"location": true, "contents": true, "parent": true, "owner": true, "flags": true,
}

// builtinFieldNames are the reserved names that are writable (by the
// object's controller): the Name and Aliases fields.
var builtinFieldNames = map[string]bool{"name": true, "aliases": true}

// DefineProp adds a new property to obj, like LambdaMOO's add_property. The
// name must be non-empty, not reserved, and not already defined on obj, any
// of its ancestors, or any of its descendants, so a name has exactly one
// definition along every parent chain. (A child may still hold its own value
// for an inherited property; that is an override, made by SetProp, not a
// second definition.) p.Owner must be a live player, p.Value valid JSON, and
// every object reference in the value must name a live object.
func (w *World) DefineProp(obj ObjID, name string, p Property) error {
	o, err := w.live(obj)
	if err != nil {
		return err
	}
	if name == "" || reservedNames[name] {
		return fmt.Errorf("%w: %q cannot be a property name", ErrBadName, name)
	}
	if _, def, err := w.LookupProp(obj, name); err == nil {
		return fmt.Errorf("%w: %v.%s is already defined on %v", ErrPropertyExists, obj, name, def)
	}
	for _, d := range w.descendants(obj) {
		if _, ok := w.objects[d].Props[name]; ok {
			return fmt.Errorf("%w: descendant %v already defines %q", ErrPropertyExists, d, name)
		}
	}
	if _, err := w.livePlayer(p.Owner); err != nil {
		return err
	}
	if err := w.checkValue(p.Value); err != nil {
		return err
	}
	if len(p.Schema) > 0 && !json.Valid(p.Schema) {
		return fmt.Errorf("%w: schema of %q is not valid JSON", ErrBadValue, name)
	}
	if p.Perms&^(PropRead|PropWrite|PropCopy) != 0 {
		return fmt.Errorf("%w: unknown permission bits on %q", ErrBadValue, name)
	}
	p.Value = bytes.Clone(p.Value)
	p.Schema = bytes.Clone(p.Schema)
	o.Props[name] = p
	return nil
}

// SetProp writes value to property name as seen from obj. If obj has its own
// slot for the property, that slot's value changes. If obj only inherits it,
// SetProp creates an override slot on obj holding the new value, with the
// owner, permissions and schema of the inherited slot, so writing a child
// never changes its ancestors (LambdaMOO's semantics).
//
// The value must be valid JSON, and every object reference in it must name a
// live object (§6: recycled objects cannot be referenced). SetProp checks no
// permissions (see CheckWriteProp) and does not yet enforce schemas (M2).
// The writable built-ins "name" and "aliases" are not properties and are not
// set through SetProp; reserved names return ErrNotWritable.
func (w *World) SetProp(obj ObjID, name string, value JSONValue) error {
	o, err := w.live(obj)
	if err != nil {
		return err
	}
	if reservedNames[name] {
		return fmt.Errorf("%w: %q", ErrNotWritable, name)
	}
	p, _, err := w.LookupProp(obj, name)
	if err != nil {
		return err
	}
	if err := w.checkValue(value); err != nil {
		return err
	}
	if _, local := o.Props[name]; !local {
		p.Schema = bytes.Clone(p.Schema) // the override gets its own copy
	}
	p.Value = bytes.Clone(value)
	o.Props[name] = p
	return nil
}

// SetMethod adds m to obj, or replaces obj's own method of the same name.
// Adding a method that an ancestor also has overrides it for obj and its
// descendants (§4.3). The method must be exactly one kind (Method.Kind), its
// owner a live player, and its Rule and Contract valid JSON when present. An
// empty Care becomes CareNormal and a zero Budget becomes DefaultBudget, the
// §5 defaults. SetMethod checks no permissions (see CheckProgramMethod) and
// does not check the Frozen flag, which is a permission matter.
func (w *World) SetMethod(obj ObjID, m Method) error {
	o, err := w.live(obj)
	if err != nil {
		return err
	}
	if m.Name == "" {
		return fmt.Errorf("%w: a method needs a name", ErrBadName)
	}
	if _, err := m.Kind(); err != nil {
		return err
	}
	if m.hasRule() && !json.Valid(m.Rule) {
		return fmt.Errorf("%w: rule of %q is not valid JSON", ErrBadMethod, m.Name)
	}
	if len(m.Contract) > 0 && !json.Valid(m.Contract) {
		return fmt.Errorf("%w: contract of %q is not valid JSON", ErrBadMethod, m.Name)
	}
	if m.Perms&^MethodExec != 0 {
		return fmt.Errorf("%w: unknown permission bits on %q", ErrBadMethod, m.Name)
	}
	if m.Care == "" {
		m.Care = CareNormal
	} else if !m.Care.Valid() {
		return fmt.Errorf("%w: unknown care level %q", ErrBadMethod, m.Care)
	}
	switch {
	case m.Budget == 0:
		m.Budget = DefaultBudget
	case m.Budget < 0:
		return fmt.Errorf("%w: negative budget %d", ErrBadMethod, m.Budget)
	}
	if _, err := w.livePlayer(m.Owner); err != nil {
		return err
	}
	// Copy the slices so the caller cannot change the stored method later
	// by modifying the slices it passed in.
	m.Rule = bytes.Clone(m.Rule)
	m.Contract = bytes.Clone(m.Contract)
	m.Reads = slices.Clone(m.Reads)
	o.Methods[m.Name] = m
	return nil
}

// descendants returns every live object whose parent chain includes obj
// (children, grandchildren, ...), in increasing ID order. It scans the whole
// world, which is fine in memory at MOO scale; a parent-to-children index can
// come later if profiling asks for one.
func (w *World) descendants(obj ObjID) []ObjID {
	var out []ObjID
	for _, id := range w.sortedIDs() {
		if id != obj && w.isAncestor(obj, id) {
			out = append(out, id)
		}
	}
	return out
}

// sortedNames returns a map's keys in sorted order, for deterministic loops.
func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
