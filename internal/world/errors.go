// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

package world

import "errors"

// Sentinel errors. Functions in this package wrap one of these with details
// using fmt.Errorf's %w verb, so callers test the kind of failure with
// errors.Is(err, world.ErrPermission) and still get a readable message. Later
// milestones map these kinds onto verdicts such as "rejected:perms" (§5).
var (
	// ErrNoSuchObject: the ID was never allocated, or is Nothing where an
	// object is required.
	ErrNoSuchObject = errors.New("no such object")
	// ErrRecycled: the ID belonged to an object that has been recycled.
	// Recycled IDs are never reused and may not be referenced (§6).
	ErrRecycled = errors.New("object has been recycled")
	// ErrNoSuchProperty: neither the object nor any ancestor has the property.
	ErrNoSuchProperty = errors.New("no such property")
	// ErrNoSuchMethod: neither the object nor any ancestor has the method.
	ErrNoSuchMethod = errors.New("no such method")
	// ErrPropertyExists: a property of that name is already defined on the
	// object, an ancestor or a descendant.
	ErrPropertyExists = errors.New("property already defined")
	// ErrBadName: an empty name, or one reserved for a built-in property.
	ErrBadName = errors.New("invalid name")
	// ErrNotWritable: a built-in such as location or owner, which changes
	// only through its own operation (move, chown, ...), never by a write.
	ErrNotWritable = errors.New("not writable as a property")
	// ErrBadValue: a value that is not valid JSON.
	ErrBadValue = errors.New("invalid value")
	// ErrBadMethod: a malformed method, such as one with both a rule and a
	// body.
	ErrBadMethod = errors.New("invalid method")
	// ErrBadOwner: an owner that is not a live player object.
	ErrBadOwner = errors.New("owner must be a live player")
	// ErrBadFlags: an unknown flag bit or a forbidden flag change.
	ErrBadFlags = errors.New("invalid flags")
	// ErrContainmentCycle: a move that would put an object inside itself,
	// directly or transitively (§6).
	ErrContainmentCycle = errors.New("containment cycle")
	// ErrInheritanceCycle: a parent chain that loops back on itself.
	ErrInheritanceCycle = errors.New("inheritance cycle")
	// ErrPlayerPlacement: a player placed somewhere other than a room or a
	// vehicle (§6).
	ErrPlayerPlacement = errors.New("players may only be in rooms or vehicles")
	// ErrInconsistent: a world whose derived state (Contents) disagrees with
	// its source of truth (Location), or which is otherwise malformed.
	ErrInconsistent = errors.New("inconsistent world")
	// ErrPermission: the principal lacks the authority for the operation.
	ErrPermission = errors.New("permission denied")
)
