// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

package world

import (
	"errors"
	"fmt"
	"testing"
)

// who lists, for one operation, whether each of the three principals in the
// matrix may perform it. The expected values are written out literally in
// the tables below rather than computed, so the tests state §4.4's rules a
// second time, independently of perms.go, instead of repeating its logic.
type who struct{ owner, other, wizard bool }

// principals returns the three principals in a fixed order with their names,
// so a failing case names the principal that failed.
func (f *fixture) principals(w who) []struct {
	name string
	id   ObjID
	want bool
} {
	return []struct {
		name string
		id   ObjID
		want bool
	}{
		{"owner", f.owner, w.owner},
		{"non-owner", f.other, w.other},
		{"wizard", f.wiz, w.wizard},
	}
}

// expect checks one permission decision: err must be nil if want is true,
// and must wrap ErrPermission if want is false.
func expect(t *testing.T, err error, want bool) {
	t.Helper()
	if want && err != nil {
		t.Errorf("denied, want allowed: %v", err)
	}
	if !want && !errors.Is(err, ErrPermission) {
		t.Errorf("got %v, want a permission error", err)
	}
}

// allPropPerms is every combination of the three property bits: 2^3 = 8.
var allPropPerms = func() []PropPerms {
	var out []PropPerms
	for p := PropPerms(0); p <= PropRead|PropWrite|PropCopy; p++ {
		out = append(out, p)
	}
	return out
}()

// TestPropertyPermissionMatrix crosses owner / non-owner / wizard with every
// combination of the r, w and c bits and every property operation. For each
// operation the table gives the expected outcome with the operation's
// relevant bit clear and set; an operation that no bit affects lists the same
// outcome twice, and the loop still runs it under all eight combinations to
// prove the other bits really make no difference (in particular that c
// grants no access).
func TestPropertyPermissionMatrix(t *testing.T) {
	ops := []struct {
		name    string
		bit     PropPerms // the bit that matters, or 0 if none does
		off, on who
		check   func(w *World, p PlayerID, obj ObjID, prop string) error
	}{
		{"read", PropRead, who{true, false, true}, who{true, true, true}, (*World).CheckReadProp},
		{"write", PropWrite, who{true, false, true}, who{true, true, true}, (*World).CheckWriteProp},
		{"change info", 0, who{true, false, true}, who{true, false, true}, (*World).CheckChangePropInfo},
		{"chown", 0, who{false, false, true}, who{false, false, true}, (*World).CheckChownProp},
	}

	// Two placements of each property: defined directly on the object, and
	// inherited by a child (owned by the same player) from its parent. The
	// inherited case proves the checks use the slot found by lookup.
	for _, placement := range []string{"local", "inherited"} {
		for _, perms := range allPropPerms {
			f := newFixture(t)
			thing := f.create(t, Nothing, f.owner, "thing", 0)
			must(t, f.w.DefineProp(thing, "p", prop(f.owner, perms, `1`)))
			target := thing
			if placement == "inherited" {
				target = f.create(t, thing, f.owner, "child", 0)
			}
			for _, op := range ops {
				expected := op.off
				if op.bit != 0 && perms&op.bit != 0 {
					expected = op.on
				}
				for _, pr := range f.principals(expected) {
					name := fmt.Sprintf("%s/perms=%q/%s/%s", placement, perms.String(), op.name, pr.name)
					t.Run(name, func(t *testing.T) {
						expect(t, op.check(f.w, pr.id, target, "p"), pr.want)
					})
				}
			}
		}
	}
}

// TestPropertyOwnerNotObjectOwner checks that it is the property's owner,
// not the object's, whose rights count: a wizard-owned property on the
// owner's object is out of the object owner's reach without the r and w bits.
func TestPropertyOwnerNotObjectOwner(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	must(t, f.w.DefineProp(thing, "secret", prop(f.wiz, 0, `"xyzzy"`)))
	expect(t, f.w.CheckReadProp(f.owner, thing, "secret"), false)
	expect(t, f.w.CheckWriteProp(f.owner, thing, "secret"), false)
	expect(t, f.w.CheckChangePropInfo(f.owner, thing, "secret"), false)
	expect(t, f.w.CheckReadProp(f.wiz, thing, "secret"), true)
}

// TestBuiltinPropertyPermissions covers the names that are not ordinary
// property slots: everyone may read them; only the object's controller may
// write name and aliases; no one may write the structural attributes.
func TestBuiltinPropertyPermissions(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	for _, name := range []string{"name", "aliases", "location", "contents", "parent", "owner", "flags"} {
		for _, pr := range f.principals(who{true, true, true}) {
			t.Run("read "+name+"/"+pr.name, func(t *testing.T) {
				expect(t, f.w.CheckReadProp(pr.id, thing, name), pr.want)
			})
		}
	}
	for _, name := range []string{"name", "aliases"} {
		for _, pr := range f.principals(who{true, false, true}) {
			t.Run("write "+name+"/"+pr.name, func(t *testing.T) {
				expect(t, f.w.CheckWriteProp(pr.id, thing, name), pr.want)
			})
		}
	}
	for _, name := range []string{"location", "contents", "parent", "owner", "flags"} {
		for _, pr := range f.principals(who{}) {
			t.Run("write "+name+"/"+pr.name, func(t *testing.T) {
				wantErr(t, f.w.CheckWriteProp(pr.id, thing, name), ErrNotWritable)
			})
		}
	}
}

// TestMethodPermissionMatrix crosses the three principals with the x bit
// clear and set, and with the object frozen or not, for every method
// operation, on both a local and an inherited method.
func TestMethodPermissionMatrix(t *testing.T) {
	type outcome struct{ xOff, xOn who }
	ops := []struct {
		name             string
		unfrozen, frozen outcome
		check            func(w *World, p PlayerID, obj ObjID, name string) error
	}{
		{
			name:     "exec",
			unfrozen: outcome{who{true, false, true}, who{true, true, true}},
			frozen:   outcome{who{true, false, true}, who{true, true, true}}, // freezing stops edits, not calls
			check:    (*World).CheckExecMethod,
		},
		{
			name:     "chown",
			unfrozen: outcome{who{false, false, true}, who{false, false, true}},
			frozen:   outcome{who{false, false, true}, who{false, false, true}},
			check:    (*World).CheckChownMethod,
		},
	}
	for _, placement := range []string{"local", "inherited"} {
		for _, frozen := range []bool{false, true} {
			for _, x := range []MethodPerms{0, MethodExec} {
				f := newFixture(t)
				thing := f.create(t, Nothing, f.owner, "thing", 0)
				must(t, f.w.SetMethod(thing, nlMethod("rub", f.owner, x, "Glow.")))
				target := thing
				if placement == "inherited" {
					target = f.create(t, thing, f.owner, "child", 0)
				}
				if frozen {
					must(t, f.w.SetFlags(target, FlagFrozen))
				}
				for _, op := range ops {
					o := op.unfrozen
					if frozen {
						o = op.frozen
					}
					expected := o.xOff
					if x != 0 {
						expected = o.xOn
					}
					for _, pr := range f.principals(expected) {
						name := fmt.Sprintf("%s/frozen=%v/x=%v/%s/%s", placement, frozen, x != 0, op.name, pr.name)
						t.Run(name, func(t *testing.T) {
							expect(t, op.check(f.w, pr.id, target, "rub"), pr.want)
						})
					}
				}
			}
		}
	}
}

// TestProgramMethodMatrix covers CheckProgramMethod's three cases (changing
// an existing local method, overriding an inherited one, adding a brand-new
// one), each on a frozen and an unfrozen object, crossed with the three
// principals and the x bit (which must make no difference: x is about
// calling, not editing).
func TestProgramMethodMatrix(t *testing.T) {
	cases := []struct {
		name             string
		unfrozen, frozen who
	}{
		{"existing local method", who{true, false, true}, who{false, false, true}},
		{"override inherited method", who{true, false, true}, who{false, false, true}},
		{"new method", who{true, false, true}, who{false, false, true}},
	}
	for _, c := range cases {
		for _, frozen := range []bool{false, true} {
			for _, x := range []MethodPerms{0, MethodExec} {
				f := newFixture(t)
				proto := f.create(t, Nothing, f.owner, "proto", 0)
				thing := f.create(t, proto, f.owner, "thing", 0)
				name := "rub"
				switch c.name {
				case "existing local method":
					must(t, f.w.SetMethod(thing, nlMethod(name, f.owner, x, "Glow.")))
				case "override inherited method":
					must(t, f.w.SetMethod(proto, nlMethod(name, f.owner, x, "Glow.")))
				case "new method":
					name = "polish"
				}
				if frozen {
					must(t, f.w.SetFlags(thing, FlagFrozen))
				}
				expected := c.unfrozen
				if frozen {
					expected = c.frozen
				}
				for _, pr := range f.principals(expected) {
					t.Run(fmt.Sprintf("%s/frozen=%v/x=%v/%s", c.name, frozen, x != 0, pr.name), func(t *testing.T) {
						expect(t, f.w.CheckProgramMethod(pr.id, thing, name), pr.want)
					})
				}
			}
		}
	}
}

// TestProgramForeignMethod: a method on the owner's object that belongs to
// someone else stays that someone's to change; owning the object is not
// enough.
func TestProgramForeignMethod(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	must(t, f.w.SetMethod(thing, nlMethod("rub", f.other, MethodExec, "Glow.")))
	expect(t, f.w.CheckProgramMethod(f.owner, thing, "rub"), false)
	expect(t, f.w.CheckProgramMethod(f.other, thing, "rub"), true)
	expect(t, f.w.CheckProgramMethod(f.wiz, thing, "rub"), true)
}

// TestObjectPermissionMatrix crosses the three principals with the fertile
// bit clear and set, for every object-level operation.
func TestObjectPermissionMatrix(t *testing.T) {
	ops := []struct {
		name    string
		off, on who // fertile clear, fertile set
		check   func(f *fixture, p PlayerID, thing ObjID) error
	}{
		{"create child", who{true, false, true}, who{true, true, true},
			func(f *fixture, p PlayerID, thing ObjID) error { return f.w.CheckCreateChild(p, thing) }},
		{"recycle", who{true, false, true}, who{true, false, true},
			func(f *fixture, p PlayerID, thing ObjID) error { return f.w.CheckRecycle(p, thing) }},
		{"move", who{true, false, true}, who{true, false, true},
			func(f *fixture, p PlayerID, thing ObjID) error { return f.w.CheckMove(p, thing) }},
		{"define property", who{true, false, true}, who{true, false, true},
			func(f *fixture, p PlayerID, thing ObjID) error { return f.w.CheckDefineProp(p, thing) }},
		{"chown", who{false, false, true}, who{false, false, true},
			func(f *fixture, p PlayerID, thing ObjID) error { return f.w.CheckChown(p, thing) }},
		// Re-parenting the thing itself (onto a fertile prototype anyone may
		// use) needs control of it, whatever its own fertile bit says.
		{"chparent self", who{true, false, true}, who{true, false, true},
			func(f *fixture, p PlayerID, thing ObjID) error {
				proto, err := f.w.Create(Nothing, f.wiz, "generic thing", FlagFertile)
				if err != nil {
					return err
				}
				return f.w.CheckChparent(p, thing, proto)
			}},
		// Re-parenting another object onto the thing needs control of that
		// object (each principal owns its own here) and the right to create
		// children of the thing, which fertile grants.
		{"chparent onto", who{true, false, true}, who{true, true, true},
			func(f *fixture, p PlayerID, thing ObjID) error {
				mine, err := f.w.Create(Nothing, p, "mine", 0)
				if err != nil {
					return err
				}
				return f.w.CheckChparent(p, mine, thing)
			}},
	}
	for _, fertile := range []bool{false, true} {
		for _, op := range ops {
			expected := op.off
			if fertile {
				expected = op.on
			}
			f := newFixture(t)
			var flags ObjFlags
			if fertile {
				flags = FlagFertile
			}
			thing := f.create(t, Nothing, f.owner, "thing", flags)
			for _, pr := range f.principals(expected) {
				t.Run(fmt.Sprintf("fertile=%v/%s/%s", fertile, op.name, pr.name), func(t *testing.T) {
					expect(t, op.check(f, pr.id, thing), pr.want)
				})
			}
		}
	}
}

// TestSetFlagMatrix crosses the three principals with every flag.
func TestSetFlagMatrix(t *testing.T) {
	wizardOnly := who{false, false, true}
	controller := who{true, false, true}
	cases := []struct {
		flag ObjFlags
		want who
	}{
		{FlagPlayer, wizardOnly},
		{FlagWizard, wizardOnly},
		{FlagFrozen, wizardOnly},
		{FlagFertile, controller},
		{FlagRoom, controller},
		{FlagExit, controller},
		{FlagVehicle, controller},
	}
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	for _, c := range cases {
		for _, pr := range f.principals(c.want) {
			t.Run(c.flag.String()+"/"+pr.name, func(t *testing.T) {
				expect(t, f.w.CheckSetFlag(pr.id, thing, c.flag), pr.want)
			})
		}
	}
	// A player may not make itself a wizard, even though it owns itself.
	expect(t, f.w.CheckSetFlag(f.owner, f.owner, FlagWizard), false)
	// Not a single defined flag.
	for _, bad := range []ObjFlags{0, FlagRoom | FlagExit, 1 << 20} {
		wantErr(t, f.w.CheckSetFlag(f.wiz, thing, bad), ErrBadFlags)
	}
}

// TestCreateRootObject: only wizards may create objects with no parent.
func TestCreateRootObject(t *testing.T) {
	f := newFixture(t)
	for _, pr := range f.principals(who{false, false, true}) {
		t.Run(pr.name, func(t *testing.T) {
			expect(t, f.w.CheckCreateChild(pr.id, Nothing), pr.want)
		})
	}
}

// TestNoAuthority checks that a principal that is not a live player is
// refused by every check, even for an object it "owns" in name, and even
// where the permission bits would let anyone through.
func TestNoAuthority(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", FlagFertile)
	must(t, f.w.DefineProp(thing, "p", prop(f.owner, PropRead|PropWrite, `1`)))
	must(t, f.w.SetMethod(thing, nlMethod("rub", f.owner, MethodExec, "Glow.")))
	// Fake a recycled player by marking an unused ID recycled; the public
	// API cannot recycle yet (M7).
	ghost := f.w.nextID
	f.w.nextID++
	f.w.recycled[ghost] = true

	checks := map[string]func(p PlayerID) error{
		"read":         func(p PlayerID) error { return f.w.CheckReadProp(p, thing, "p") },
		"read builtin": func(p PlayerID) error { return f.w.CheckReadProp(p, thing, "name") },
		"write":        func(p PlayerID) error { return f.w.CheckWriteProp(p, thing, "p") },
		"prop info":    func(p PlayerID) error { return f.w.CheckChangePropInfo(p, thing, "p") },
		"define":       func(p PlayerID) error { return f.w.CheckDefineProp(p, thing) },
		"chown prop":   func(p PlayerID) error { return f.w.CheckChownProp(p, thing, "p") },
		"exec":         func(p PlayerID) error { return f.w.CheckExecMethod(p, thing, "rub") },
		"program":      func(p PlayerID) error { return f.w.CheckProgramMethod(p, thing, "rub") },
		"chown method": func(p PlayerID) error { return f.w.CheckChownMethod(p, thing, "rub") },
		"create child": func(p PlayerID) error { return f.w.CheckCreateChild(p, thing) },
		"create root":  func(p PlayerID) error { return f.w.CheckCreateChild(p, Nothing) },
		"recycle":      func(p PlayerID) error { return f.w.CheckRecycle(p, thing) },
		"move":         func(p PlayerID) error { return f.w.CheckMove(p, thing) },
		"chparent":     func(p PlayerID) error { return f.w.CheckChparent(p, thing, Nothing) },
		"set flag":     func(p PlayerID) error { return f.w.CheckSetFlag(p, thing, FlagFertile) },
		"chown":        func(p PlayerID) error { return f.w.CheckChown(p, thing) },
	}
	principals := map[string]ObjID{
		"nothing":       Nothing,
		"non-player":    thing, // an object, but not a player
		"recycled":      ghost,
		"never existed": 9999,
	}
	for pname, p := range principals {
		for cname, check := range checks {
			t.Run(pname+"/"+cname, func(t *testing.T) {
				wantErr(t, check(p), ErrPermission)
			})
		}
		if f.w.IsWizard(p) || f.w.Controls(p, p) {
			t.Errorf("%s has authority", pname)
		}
	}
}

// TestMissingTargets: asking about something that does not exist is not a
// permission question; the error says what is missing, whoever asks.
func TestMissingTargets(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	f.w.recycled[f.w.nextID] = true
	gone := f.w.nextID
	f.w.nextID++

	cases := []struct {
		name string
		err  error
		want error
	}{
		{"read missing prop", f.w.CheckReadProp(f.wiz, thing, "nope"), ErrNoSuchProperty},
		{"write missing prop", f.w.CheckWriteProp(f.wiz, thing, "nope"), ErrNoSuchProperty},
		{"info missing prop", f.w.CheckChangePropInfo(f.wiz, thing, "nope"), ErrNoSuchProperty},
		{"chown missing prop", f.w.CheckChownProp(f.wiz, thing, "nope"), ErrNoSuchProperty},
		{"exec missing method", f.w.CheckExecMethod(f.wiz, thing, "nope"), ErrNoSuchMethod},
		{"chown missing method", f.w.CheckChownMethod(f.wiz, thing, "nope"), ErrNoSuchMethod},
		{"read on recycled", f.w.CheckReadProp(f.wiz, gone, "name"), ErrRecycled},
		{"write on recycled", f.w.CheckWriteProp(f.wiz, gone, "name"), ErrRecycled},
		{"info on recycled", f.w.CheckChangePropInfo(f.wiz, gone, "p"), ErrRecycled},
		{"define on recycled", f.w.CheckDefineProp(f.wiz, gone), ErrRecycled},
		{"chown prop on recycled", f.w.CheckChownProp(f.wiz, gone, "p"), ErrRecycled},
		{"exec on recycled", f.w.CheckExecMethod(f.wiz, gone, "rub"), ErrRecycled},
		{"program on recycled", f.w.CheckProgramMethod(f.wiz, gone, "rub"), ErrRecycled},
		{"chown method on recycled", f.w.CheckChownMethod(f.wiz, gone, "rub"), ErrRecycled},
		{"child of recycled", f.w.CheckCreateChild(f.wiz, gone), ErrRecycled},
		{"recycle recycled", f.w.CheckRecycle(f.wiz, gone), ErrRecycled},
		{"move recycled", f.w.CheckMove(f.wiz, gone), ErrRecycled},
		{"chparent recycled", f.w.CheckChparent(f.wiz, gone, Nothing), ErrRecycled},
		{"chparent onto recycled", f.w.CheckChparent(f.wiz, thing, gone), ErrRecycled},
		{"flag on recycled", f.w.CheckSetFlag(f.wiz, gone, FlagRoom), ErrRecycled},
		{"chown recycled", f.w.CheckChown(f.wiz, gone), ErrRecycled},
		{"never allocated", f.w.CheckMove(f.wiz, 9999), ErrNoSuchObject},
		{"method authority", func() error { _, err := f.w.MethodAuthority(thing, "nope"); return err }(), ErrNoSuchMethod},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantErr(t, c.err, c.want) })
	}
}

// TestWizardLosesPowers: wizard status is read at check time, so clearing
// the flag takes effect immediately.
func TestWizardLosesPowers(t *testing.T) {
	f := newFixture(t)
	thing := f.create(t, Nothing, f.owner, "thing", 0)
	expect(t, f.w.CheckMove(f.wiz, thing), true)
	must(t, f.w.SetFlags(f.wiz, FlagPlayer)) // clear Wizard
	expect(t, f.w.CheckMove(f.wiz, thing), false)
	if f.w.IsWizard(f.wiz) {
		t.Error("still a wizard")
	}
}

// TestOwnerAuthority is §4.4's vending machine: the machine's method runs
// with its owner's authority, so it may move the owner's soda into a buyer
// who has no rights over the soda at all.
func TestOwnerAuthority(t *testing.T) {
	f := newFixture(t)
	buyer := f.other
	must(t, f.w.Move(buyer, f.room))
	machine := f.create(t, Nothing, f.owner, "vending machine", 0)
	must(t, f.w.Move(machine, f.room))
	soda := f.create(t, Nothing, f.owner, "soda", 0)
	must(t, f.w.Move(soda, machine))
	must(t, f.w.SetMethod(machine, nlMethod("buy", f.owner, MethodExec, "Dispense a soda to the caller.")))

	// The buyer may call the method...
	expect(t, f.w.CheckExecMethod(buyer, machine, "buy"), true)
	// ...which runs as the machine's owner...
	auth, err := f.w.MethodAuthority(machine, "buy")
	must(t, err)
	if auth != f.owner {
		t.Fatalf("authority = %v, want %v", auth, f.owner)
	}
	// ...who may move the soda, though the buyer could not.
	expect(t, f.w.CheckMove(auth, soda), true)
	expect(t, f.w.CheckMove(buyer, soda), false)
	must(t, f.w.Move(soda, buyer))
	checkWorld(t, f.w)
}

// TestInheritedMethodAuthority: a method inherited from a wizard-owned
// prototype runs as the wizard, not as the owner of the object it is called
// on, while an override on the child runs as the override's owner.
func TestInheritedMethodAuthority(t *testing.T) {
	f := newFixture(t)
	proto := f.create(t, Nothing, f.wiz, "generic thing", FlagFertile)
	must(t, f.w.SetMethod(proto, nlMethod("look", f.wiz, MethodExec, "Describe it.")))
	child := f.create(t, proto, f.owner, "lamp", 0)

	auth, err := f.w.MethodAuthority(child, "look")
	must(t, err)
	if auth != f.wiz {
		t.Fatalf("authority = %v, want the wizard %v", auth, f.wiz)
	}
	must(t, f.w.SetMethod(child, nlMethod("look", f.owner, MethodExec, "Describe the lamp.")))
	auth, err = f.w.MethodAuthority(child, "look")
	must(t, err)
	if auth != f.owner {
		t.Fatalf("authority after override = %v, want %v", auth, f.owner)
	}
}
