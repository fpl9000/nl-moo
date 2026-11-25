// Copyright (C) 2026 Francis Litterio
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version. See the LICENSE file for the full text.

package world

import "fmt"

// This file is the whole permission model of §4.4, in one place so it can be
// audited (§14.1). It is a close copy of LambdaMOO's:
//
//   - Every object, property and method has an owner, always a player.
//     Owners may read, write and recycle their own things regardless of
//     permission bits.
//   - Permission bits grant access to everyone else: r and w on properties,
//     x on methods, f (FlagFertile) on objects.
//   - Wizards bypass every check here.
//   - A method runs with the authority of its owner, not its caller
//     (MethodAuthority).
//
// Every Check function takes the principal p whose authority applies and
// returns nil if the operation is allowed, or an error wrapping
// ErrPermission if it is not. A missing object, property or method is not a
// permission matter and is reported with its own error (ErrNoSuchObject and
// so on), whoever is asking.
//
// The principal must be a live player. Anything else (an ID that was never a
// player, a recycled player, Nothing) has no authority at all and every check
// fails. This is checked at call time, so a method whose owner has been
// recycled can do nothing, and a method whose owner loses the wizard flag
// stops having wizard powers at once.
//
// These checks say who may ask for a change. Whether the change is possible
// at all (no containment cycles, and so on) is the separate question that
// the invariants in invariants.go answer, and the World mutators enforce.

// principal returns p's live player object, or an ErrPermission error if p
// has no authority.
func (w *World) principal(p PlayerID) (*Object, error) {
	o := w.objects[p]
	if o == nil || !o.Flags.Has(FlagPlayer) {
		return nil, fmt.Errorf("%w: %v is not a live player and has no authority", ErrPermission, p)
	}
	return o, nil
}

// IsWizard reports whether p is a live player with the wizard flag.
func (w *World) IsWizard(p PlayerID) bool {
	o, err := w.principal(p)
	return err == nil && o.Flags.Has(FlagWizard)
}

// Controls reports whether principal p has an owner's rights over something
// owned by owner: p is a live player and is either that owner or a wizard.
func (w *World) Controls(p, owner PlayerID) bool {
	o, err := w.principal(p)
	return err == nil && (o.Flags.Has(FlagWizard) || p == owner)
}

// deny builds the standard permission error.
func deny(p PlayerID, what string) error {
	return fmt.Errorf("%w: %v may not %s", ErrPermission, p, what)
}

// target returns obj's live object for a check by p. It checks p's authority
// first, so a principal with no authority learns nothing about which objects
// exist.
func (w *World) target(p PlayerID, obj ObjID) (*Object, error) {
	if _, err := w.principal(p); err != nil {
		return nil, err
	}
	return w.live(obj)
}

// ---------------------------------------------------------------------------
// Properties

// CheckReadProp decides whether p may read obj.name, where name may be an
// inherited property. The owner of the slot found by LookupProp and wizards
// may always read it; anyone else needs the r bit.
//
// The built-in names "name" and "aliases", and the structural attributes
// "location", "contents", "parent", "owner" and "flags", are readable by
// anyone, as in LambdaMOO.
func (w *World) CheckReadProp(p PlayerID, obj ObjID, name string) error {
	if _, err := w.target(p, obj); err != nil {
		return err
	}
	if reservedNames[name] {
		return nil
	}
	prop, _, err := w.LookupProp(obj, name)
	if err != nil {
		return err
	}
	if w.Controls(p, prop.Owner) || prop.Perms&PropRead != 0 {
		return nil
	}
	return deny(p, fmt.Sprintf("read %v.%s", obj, name))
}

// CheckWriteProp decides whether p may write obj.name. The owner of the slot
// found by LookupProp and wizards may always write it; anyone else needs the
// w bit.
//
// Note that it is the property's owner, not the object's, who decides. A
// child object inherits a non-c property with the defining property's owner
// (see Create and SetProp), so the child's owner cannot overwrite it, while
// the prototype's own methods, which run as the prototype's owner, can. This
// is LambdaMOO's behavior and is what lets a prototype keep state on its
// children that the children's owners cannot tamper with.
//
// "name" and "aliases" are writable by whoever controls the object. The
// structural attributes are never writable as properties, by anyone: they
// change only through their own operations, so this returns ErrNotWritable.
func (w *World) CheckWriteProp(p PlayerID, obj ObjID, name string) error {
	o, err := w.target(p, obj)
	if err != nil {
		return err
	}
	if builtinFieldNames[name] {
		if w.Controls(p, o.Owner) {
			return nil
		}
		return deny(p, fmt.Sprintf("rename %v", obj))
	}
	if reservedNames[name] {
		return fmt.Errorf("%w: %v.%s changes only through its own operation", ErrNotWritable, obj, name)
	}
	prop, _, err := w.LookupProp(obj, name)
	if err != nil {
		return err
	}
	if w.Controls(p, prop.Owner) || prop.Perms&PropWrite != 0 {
		return nil
	}
	return deny(p, fmt.Sprintf("write %v.%s", obj, name))
}

// CheckChangePropInfo decides whether p may change obj.name's permission bits
// or schema: only the property's owner or a wizard. The r and w bits grant
// nothing here, since they are about the value, not the slot.
func (w *World) CheckChangePropInfo(p PlayerID, obj ObjID, name string) error {
	if _, err := w.target(p, obj); err != nil {
		return err
	}
	prop, _, err := w.LookupProp(obj, name)
	if err != nil {
		return err
	}
	if w.Controls(p, prop.Owner) {
		return nil
	}
	return deny(p, fmt.Sprintf("change the permissions of %v.%s", obj, name))
}

// CheckDefineProp decides whether p may add a new property to obj: only
// whoever controls the object. (LambdaMOO also allows this with an object
// "w" bit; the design has no such bit, so the owner and wizards only.)
func (w *World) CheckDefineProp(p PlayerID, obj ObjID) error {
	o, err := w.target(p, obj)
	if err != nil {
		return err
	}
	if w.Controls(p, o.Owner) {
		return nil
	}
	return deny(p, fmt.Sprintf("add properties to %v", obj))
}

// CheckChownProp decides whether p may change obj.name's owner: wizards only
// (§4.4: wizards may @chown).
func (w *World) CheckChownProp(p PlayerID, obj ObjID, name string) error {
	if _, err := w.target(p, obj); err != nil {
		return err
	}
	if _, _, err := w.LookupProp(obj, name); err != nil {
		return err
	}
	if w.IsWizard(p) {
		return nil
	}
	return deny(p, fmt.Sprintf("change the owner of %v.%s", obj, name))
}

// ---------------------------------------------------------------------------
// Methods

// CheckExecMethod decides whether p may call obj:name, which may be
// inherited: the method's owner and wizards always may; anyone else needs the
// x bit.
//
// Calling a method is only the start: the method then runs as its owner, so
// what it may do is decided by checks with MethodAuthority's result as the
// principal, not with p.
func (w *World) CheckExecMethod(p PlayerID, obj ObjID, name string) error {
	if _, err := w.target(p, obj); err != nil {
		return err
	}
	m, _, err := w.LookupMethod(obj, name)
	if err != nil {
		return err
	}
	if w.Controls(p, m.Owner) || m.Perms&MethodExec != 0 {
		return nil
	}
	return deny(p, fmt.Sprintf("call %v:%s", obj, name))
}

// CheckProgramMethod decides whether p may program obj:name, meaning add it,
// replace it, or change its body, rule, contract, permissions or other
// settings.
//
//   - On a Frozen object, only wizards may program any method (§4.1).
//   - If obj itself has a method of that name, only that method's owner or a
//     wizard may change it.
//   - Otherwise the method is new on obj (perhaps overriding an inherited
//     one), and adding it needs control of obj.
//
// Note the second rule follows the method's owner, not the object's, just as
// properties do: a wizard's method installed on a player's object stays the
// wizard's to change.
func (w *World) CheckProgramMethod(p PlayerID, obj ObjID, name string) error {
	o, err := w.target(p, obj)
	if err != nil {
		return err
	}
	if w.IsWizard(p) {
		return nil
	}
	if o.Flags.Has(FlagFrozen) {
		return deny(p, fmt.Sprintf("program %v:%s, because %v is frozen", obj, name, obj))
	}
	if m, ok := o.Methods[name]; ok {
		if p == m.Owner {
			return nil
		}
		return deny(p, fmt.Sprintf("change %v:%s, which belongs to %v", obj, name, m.Owner))
	}
	if p == o.Owner {
		return nil
	}
	return deny(p, fmt.Sprintf("add methods to %v", obj))
}

// CheckChownMethod decides whether p may change obj:name's owner: wizards
// only.
func (w *World) CheckChownMethod(p PlayerID, obj ObjID, name string) error {
	if _, err := w.target(p, obj); err != nil {
		return err
	}
	if _, _, err := w.LookupMethod(obj, name); err != nil {
		return err
	}
	if w.IsWizard(p) {
		return nil
	}
	return deny(p, fmt.Sprintf("change the owner of %v:%s", obj, name))
}

// MethodAuthority returns the principal that obj:name runs as: the owner of
// the method that LookupMethod finds, which may be defined on an ancestor
// (§4.4's owner-authority rule). A method inherited from a wizard-owned
// prototype therefore runs with the wizard's authority wherever it is
// called. That is why wizard-owned methods deserve the scrutiny §13 gives
// them, and why the effect contract (M2) bounds what any method may do.
func (w *World) MethodAuthority(obj ObjID, name string) (PlayerID, error) {
	m, _, err := w.LookupMethod(obj, name)
	if err != nil {
		return Nothing, err
	}
	return m.Owner, nil
}

// ---------------------------------------------------------------------------
// Objects

// CheckCreateChild decides whether p may create a child of parent: whoever
// controls parent, or anyone if parent is fertile (§4.1). Only wizards may
// create root objects (parent Nothing); LambdaMOO allows any programmer to,
// but a root object inherits none of the audited prototype behavior, so the
// conservative choice is recorded in docs/decisions.md. Quotas (§12.5) are
// checked separately from M7.
func (w *World) CheckCreateChild(p PlayerID, parent ObjID) error {
	if _, err := w.principal(p); err != nil {
		return err
	}
	if parent == Nothing {
		if w.IsWizard(p) {
			return nil
		}
		return deny(p, "create root objects")
	}
	o, err := w.live(parent)
	if err != nil {
		return err
	}
	if w.Controls(p, o.Owner) || o.Flags.Has(FlagFertile) {
		return nil
	}
	return deny(p, fmt.Sprintf("create children of %v", parent))
}

// CheckRecycle decides whether p may recycle obj: whoever controls it.
func (w *World) CheckRecycle(p PlayerID, obj ObjID) error {
	o, err := w.target(p, obj)
	if err != nil {
		return err
	}
	if w.Controls(p, o.Owner) {
		return nil
	}
	return deny(p, fmt.Sprintf("recycle %v", obj))
}

// CheckMove decides whether p may move obj: whoever controls obj.
//
// The destination's owner is not consulted. In §4.4's example, the vending
// machine's method moves a soda (owned, like the machine, by the machine's
// owner) into the buyer, whom the machine's owner does not control; that
// must work. Whether a destination accepts an object is behavior, decided by
// its methods at later milestones, and whether the move is possible at all
// is the invariants' business (checkMove).
func (w *World) CheckMove(p PlayerID, obj ObjID) error {
	o, err := w.target(p, obj)
	if err != nil {
		return err
	}
	if w.Controls(p, o.Owner) {
		return nil
	}
	return deny(p, fmt.Sprintf("move %v", obj))
}

// CheckChparent decides whether p may change obj's parent to newParent:
// p must control obj, and must also be allowed to create a child of
// newParent (CheckCreateChild), since re-parenting is equivalent to that.
func (w *World) CheckChparent(p PlayerID, obj, newParent ObjID) error {
	o, err := w.target(p, obj)
	if err != nil {
		return err
	}
	if !w.Controls(p, o.Owner) {
		return deny(p, fmt.Sprintf("change the parent of %v", obj))
	}
	return w.CheckCreateChild(p, newParent)
}

// CheckSetFlag decides whether p may set or clear one flag on obj.
//
//   - Player, Wizard and Frozen: wizards only. §4.4 says wizardhood is
//     settable only by another wizard and §4.1 calls Frozen wizard-settable.
//     Player is wizard-only as in LambdaMOO, since it confers the ability to
//     own things.
//   - Fertile, Room, Exit and Vehicle: whoever controls obj.
//
// flag must be exactly one defined flag. (Whether the change is possible is
// separate: SetFlags never lets the Player flag change, for example.)
func (w *World) CheckSetFlag(p PlayerID, obj ObjID, flag ObjFlags) error {
	o, err := w.target(p, obj)
	if err != nil {
		return err
	}
	switch flag {
	case FlagPlayer, FlagWizard, FlagFrozen:
		if w.IsWizard(p) {
			return nil
		}
	case FlagFertile, FlagRoom, FlagExit, FlagVehicle:
		if w.Controls(p, o.Owner) {
			return nil
		}
	default:
		return fmt.Errorf("%w: %v is not a single defined flag", ErrBadFlags, flag)
	}
	return deny(p, fmt.Sprintf("change the %v flag of %v", flag, obj))
}

// CheckChown decides whether p may change obj's owner: wizards only (§4.4).
func (w *World) CheckChown(p PlayerID, obj ObjID) error {
	if _, err := w.target(p, obj); err != nil {
		return err
	}
	if w.IsWizard(p) {
		return nil
	}
	return deny(p, fmt.Sprintf("change the owner of %v", obj))
}
