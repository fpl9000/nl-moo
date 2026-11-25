// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

// Package world is NL-MOO's object model: objects, their properties and
// methods, single-inheritance lookup through the parent chain, the ownership
// and permission rules of design §4.4, and the engine invariants of design §6.
//
// At milestone M1 everything here is in memory only. There is no persistence
// (that is M3, internal/store) and no concurrency: a World is not safe for
// use by more than one goroutine at a time. The M4 engine will own the single
// World instance on its writer goroutine and give readers snapshots.
//
// The package is split by concern:
//
//   - objid.go: ObjID, the object reference type, and its "#123" notation.
//   - object.go: the data types Object, Property and Method, plus their flag
//     and permission-bit types. Methods are data only; nothing here runs them.
//   - world.go: World, the container of all objects, and the small set of
//     in-memory mutators that the invariants and inheritance rules need.
//   - inherit.go: parent-chain lookup of inherited properties and methods.
//   - invariants.go: the containment, recycled-reference and player-placement
//     invariants, checked on every mutation and over a whole world at load.
//   - perms.go: the permission rules, in one auditable file, as the design's
//     §14.1 asks.
//
// Two kinds of rule are deliberately kept apart. Invariants (invariants.go)
// describe what the world may look like at all; every World mutator enforces
// them and nothing, not even a wizard, can break them. Permissions (perms.go)
// describe who may ask for a change; the mutators do not check them, because
// the caller (later the validator and engine) decides whose authority applies,
// which per §4.4 is usually a method's owner rather than the player who typed
// the command.
package world
