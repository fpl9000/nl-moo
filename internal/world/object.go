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
	"strings"
)

// ObjFlags is the bit set of object flags from §4.1. Each flag is one bit,
// so a set of flags is the bitwise OR of its members.
//
// The numeric bit positions are part of the on-disk format from M3 onward
// (the objects.flags column of §5 stores this integer), so existing values
// must never be renumbered; new flags take the next unused bit.
type ObjFlags uint32

const (
	// FlagPlayer marks a player object: something a person connects as, and
	// the only kind of object that can own things.
	FlagPlayer ObjFlags = 1 << iota
	// FlagRoom marks a room. The game map is the set of rooms plus the exits
	// that connect them (§4.1).
	FlagRoom
	// FlagExit marks an exit. An exit lives inside its source room and has a
	// "destination" property naming the room it leads to.
	FlagExit
	// FlagWizard marks a wizard, who bypasses every permission check (§4.4).
	// Only player objects may carry it (an invariant; see SetFlags).
	FlagWizard
	// FlagFertile ("f") lets anyone create children of this object (§4.1).
	FlagFertile
	// FlagFrozen means no method on this object may be edited, except by a
	// wizard (§4.1).
	FlagFrozen
	// FlagVehicle marks an object that players may be inside even though it
	// is not a room. §6's player-placement invariant refers to destinations
	// "flagged as a vehicle", but §4.1's flag list omits such a flag, so M1
	// adds it here (recorded in docs/decisions.md).
	FlagVehicle
)

// flagNames gives each flag a lower-case name for String. The order is the
// bit order, so String's output is stable.
var flagNames = []struct {
	flag ObjFlags
	name string
}{
	{FlagPlayer, "player"},
	{FlagRoom, "room"},
	{FlagExit, "exit"},
	{FlagWizard, "wizard"},
	{FlagFertile, "fertile"},
	{FlagFrozen, "frozen"},
	{FlagVehicle, "vehicle"},
}

// allFlags is every defined flag ORed together, used to reject unknown bits.
const allFlags = FlagPlayer | FlagRoom | FlagExit | FlagWizard | FlagFertile | FlagFrozen | FlagVehicle

// Has reports whether every flag in want is set in f. With a single flag as
// the argument, that is simply "is this flag set".
func (f ObjFlags) Has(want ObjFlags) bool { return f&want == want }

// String renders the set as names joined with "|", for example
// "player|wizard", or "none" for the empty set. Unknown bits are shown in hex
// so they are never silently hidden.
func (f ObjFlags) String() string {
	if f == 0 {
		return "none"
	}
	var parts []string
	for _, fn := range flagNames {
		if f&fn.flag != 0 {
			parts = append(parts, fn.name)
		}
	}
	if rest := f &^ allFlags; rest != 0 { // &^ is Go's "AND NOT" (bit clear)
		parts = append(parts, fmt.Sprintf("0x%x", uint32(rest)))
	}
	return strings.Join(parts, "|")
}

// PropPerms is the permission bit set of a property (§4.2, §4.4). The bits
// grant access to everyone other than the property's owner; the owner and
// wizards have full access regardless. As with ObjFlags, the bit values are
// persisted from M3 onward and must not be renumbered.
type PropPerms uint8

const (
	// PropRead ("r"): any principal may read the value.
	PropRead PropPerms = 1 << iota
	// PropWrite ("w"): any principal may write the value. The design notes
	// this is rare and usually a bug.
	PropWrite
	// PropCopy ("c"): when a child of the defining object is created, the
	// value is copied into the child, and the copy is owned by the child's
	// owner. Without c, a child inherits the value live from its ancestor and
	// the inherited slot keeps the defining property's owner. See Create and
	// docs/decisions.md for why ownership follows the copy.
	PropCopy
)

// String renders the bits in LambdaMOO's style, for example "rc", or "" when
// no bit is set.
func (p PropPerms) String() string {
	var b strings.Builder
	if p&PropRead != 0 {
		b.WriteByte('r')
	}
	if p&PropWrite != 0 {
		b.WriteByte('w')
	}
	if p&PropCopy != 0 {
		b.WriteByte('c')
	}
	return b.String()
}

// MethodPerms is the permission bit set of a method. The design defines only
// one bit (§4.3, §4.4).
type MethodPerms uint8

const (
	// MethodExec ("x"): anyone may call the method. Without it, only the
	// method's owner and wizards may.
	MethodExec MethodPerms = 1 << iota
)

// String renders the bits, "x" or "".
func (p MethodPerms) String() string {
	if p&MethodExec != 0 {
		return "x"
	}
	return ""
}

// CareLevel says how much a method's outcome matters, which selects the model
// tier and verification at execution time (§7.5). It is stored as the same
// lower-case word the §5 schema uses.
type CareLevel string

const (
	CareRoutine CareLevel = "routine"
	CareNormal  CareLevel = "normal" // the default (§5 methods.care)
	CareHigh    CareLevel = "high"
)

// Valid reports whether c is one of the three defined levels.
func (c CareLevel) Valid() bool {
	return c == CareRoutine || c == CareNormal || c == CareHigh
}

// DefaultBudget is the inference budget a method gets when none is given
// (§4.3, §5 methods.budget).
const DefaultBudget = 4

// JSONValue holds a property value as raw JSON text: a string, number,
// boolean, null, list or map, where an object reference is written as a
// string in MOO notation such as "#1023" (see refsInValue). It is an alias for
// json.RawMessage, the standard library's type for "JSON we keep as bytes and
// do not decode yet".
type JSONValue = json.RawMessage

// Property is a named, typed, permission-bearing slot on an object (§4.2).
//
// The design's Schema is a parsed JSON Schema. Schema enforcement on writes is
// listed among §6's invariants but the plan schedules it with the validator,
// so at M1 Schema is kept as uninterpreted JSON text and nothing checks
// values against it yet.
type Property struct {
	Value  JSONValue // the current value, as JSON text
	Owner  PlayerID  // usually the object's owner; may differ after @chown
	Perms  PropPerms // r, w, c
	Schema JSONValue // optional JSON Schema, uninterpreted at M1; nil if none
}

// MethodKind distinguishes the two kinds of method (§4.3).
type MethodKind int

const (
	// Mechanical methods carry a MechanicalRule and no prose body; the
	// engine evaluates them directly with no inference.
	Mechanical MethodKind = iota + 1
	// NaturalLanguage methods carry a prose body and no rule; the
	// orchestrator sends them to the interpreter.
	NaturalLanguage
)

func (k MethodKind) String() string {
	switch k {
	case Mechanical:
		return "mechanical"
	case NaturalLanguage:
		return "natural-language"
	}
	return fmt.Sprintf("MethodKind(%d)", int(k))
}

// Method is the unit of behavior, LambdaMOO's "verb" (§4.3). At M1 it is data
// only: nothing in this package runs a method.
//
// Several fields hold formats that later milestones define, so M1 stores
// them without interpreting them, rather than inventing formats early:
//
//   - Rule is the MechanicalRule of a mechanical method. Its format is design
//     §17 item 9, to be settled before M5, so it is raw JSON here (the §5
//     schema also stores it as JSON text). M1 only checks it is valid JSON.
//   - Contract is the EffectContract, defined with the validator at M2 in
//     internal/contract. Keeping it as raw JSON here also avoids an import
//     cycle, since internal/contract will import this package.
//   - Signature is the command-binding spec, whose grammar is plan item B2
//     (M5-M6). It is an uninterpreted string here.
//   - Reads lists StateSelectors, whose grammar is plan item B3 (M2). Each
//     selector is an uninterpreted string here.
type Method struct {
	Name      string      // "rub", "open", "on_enter", ...
	Signature string      // how commands bind; grammar settled at M5-M6
	Owner     PlayerID    // the method runs with this player's authority
	Perms     MethodPerms // x
	Rule      JSONValue   // MechanicalRule as raw JSON; nil for NL methods
	Body      string      // the prose program; "" for mechanical methods
	Contract  JSONValue   // EffectContract as raw JSON; parsed at M2
	Care      CareLevel   // routine | normal | high
	Reads     []string    // StateSelectors as raw strings; parsed at M2
	Budget    int         // max inferences for the whole call tree
}

// hasRule reports whether m carries a mechanical rule. An empty value or a
// JSON null both mean "no rule", since §5 stores NULL for NL methods.
func (m *Method) hasRule() bool {
	r := bytes.TrimSpace(m.Rule)
	return len(r) > 0 && !bytes.Equal(r, []byte("null"))
}

// Kind classifies m, enforcing §4.3's rule that a method is exactly one of
// the two kinds: mechanical (a rule and no body) or natural-language (a body
// and no rule). A method with both, or with neither, is malformed.
func (m *Method) Kind() (MethodKind, error) {
	rule, body := m.hasRule(), m.Body != ""
	switch {
	case rule && !body:
		return Mechanical, nil
	case body && !rule:
		return NaturalLanguage, nil
	case rule && body:
		return 0, fmt.Errorf("%w: method %q has both a mechanical rule and a prose body", ErrBadMethod, m.Name)
	default:
		return 0, fmt.Errorf("%w: method %q has neither a mechanical rule nor a prose body", ErrBadMethod, m.Name)
	}
}

// Object is the unit of everything in the world (§4.1). This struct is the
// in-memory representation; §5 gives the on-disk schema used from M3.
//
// Contents is derived state: the World maintains it as the inverse of every
// other object's Location, and it is never persisted, so the containment tree
// has exactly one source of truth.
//
// Callers outside this package should treat an *Object obtained from a World
// as read-only and make every change through World's methods, which keep
// Contents consistent and enforce the invariants.
type Object struct {
	ID       ObjID               // immutable; never reused after recycle
	Parent   ObjID               // single-inheritance prototype; Nothing for roots
	Owner    PlayerID            // the player who controls this object
	Name     string              // primary display name, e.g. "brass lamp"
	Aliases  []string            // other names the parser may match, e.g. "lamp"
	Location ObjID               // the one object this object is inside, or Nothing
	Contents []ObjID             // inverse of Location, maintained by World
	Flags    ObjFlags            // player, room, exit, wizard, fertile, frozen, vehicle
	Props    map[string]Property // local property slots (definitions and overrides)
	Methods  map[string]Method   // local methods (definitions and overrides)
}
