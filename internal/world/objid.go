// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

package world

import (
	"fmt"
	"strconv"
)

// ObjID identifies an object. IDs are allocated in increasing order starting
// at #0 and are never reused, even after an object is recycled (§4.1, §5), so
// a stale reference can never silently start pointing at a different object.
//
// It is a signed 64-bit integer because the special value Nothing (#-1) is
// negative, and because SQLite's INTEGER (used at M3) is a signed 64-bit
// value, so the type round-trips through the database unchanged.
type ObjID int64

// Nothing is the special object #-1, "nowhere". It is the location of objects
// that are not inside anything (the root of the containment tree) and the
// parent of root objects (the root of the inheritance tree). No live object
// ever has this ID.
const Nothing ObjID = -1

// PlayerID is an ObjID that is expected to name a player object. Owners are
// always players (§4.4). It is a type alias, not a distinct type, so it needs
// no conversions: it documents intent in signatures and struct fields, and
// the code that cares (for example perms.go) checks the Player flag itself.
type PlayerID = ObjID

// String renders an ObjID in MOO notation, for example "#1023" or "#-1".
// Implementing String (the fmt.Stringer interface) makes fmt's %v and %s
// verbs print IDs this way automatically.
func (id ObjID) String() string {
	return "#" + strconv.FormatInt(int64(id), 10)
}

// ParseObjID parses the MOO notation produced by String. Only the canonical
// form is accepted: a "#", an optional "-", and decimal digits with no
// leading zeros, no "+" sign and no spaces. Insisting on one spelling per ID
// matters because the same notation identifies object references inside JSON
// property values (see refsInValue in invariants.go); a lenient parser would
// make "#007" and "#7" two different strings naming the same object.
func ParseObjID(s string) (ObjID, error) {
	if len(s) < 2 || s[0] != '#' {
		return Nothing, fmt.Errorf("invalid object reference %q: want #<number>", s)
	}
	n, err := strconv.ParseInt(s[1:], 10, 64)
	if err != nil {
		return Nothing, fmt.Errorf("invalid object reference %q: %w", s, err)
	}
	id := ObjID(n)
	// Round-trip check: rejects "#007", "#+7" and "#-0", which ParseInt
	// accepts but which are not how String would write the number.
	if id.String() != s {
		return Nothing, fmt.Errorf("invalid object reference %q: not in canonical form (want %q)", s, id.String())
	}
	return id, nil
}
