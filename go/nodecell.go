// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"reflect"
	"unsafe"
)

// Node cells: which container a rule's node belongs to, and how to
// replace that container everywhere it is held.
//
// TypeScript hands every rule building into a container the same array
// or object, and Rust shares one Rc<RefCell<Value>> cell between them,
// so in both "the same container" is plain object identity and a
// truncation is visible to every holder at once. A Go slice is a value:
// each rule holds its own header, and growing a list publishes a new one.
// These two methods give a caller outside the engine (a rule-done
// subscriber, typically) what the shared object gives the other ports.

// NodeCell returns the rule that holds the authoritative copy of the
// container r.Node belongs to: the Go counterpart of Rust's
// `Rc::as_ptr(&rule.node)` and of TypeScript's object identity on
// `rule.node`. Rules building into one container return the same *Rule,
// so the pointer serves as the container's identity, and that rule's
// Node field is the container's current value. A rule whose node is not
// shared (a scalar, or a container it allocated itself) returns itself.
//
// The answer starts from the engine's ownership bookkeeping, which the
// native-value builtins (`@array$`, `@object$`, `@push$`, `@value$`, …)
// keep exact. A Go action that assigns `r.Node` directly cannot update
// that bookkeeping, so NodeCell also recognises the shapes hand-written
// grammars use: a rule that put a container of its own in place of what
// it was seeded with is its own cell, and an element rule that writes
// its grown list back to the rule that pushed it shares that rule's
// cell (the unbroken run of Parent rules holding the same container).
//
// The cell is stable while the container is built in place. A rule that
// lifts a child's container with `@value$` or `@bubble$` keeps the
// child's cell; a Go action that assigns a container to a rule starts a
// new cell there, as Rust's fresh `Rc` does. A rule left holding an
// outdated copy of a list (one a later append re-allocated elsewhere)
// can no longer be matched to it and returns itself.
//
// A replacement chain (`r:`) hands one container from rule to rule, and
// a Go grammar that grows a list there assigns each successor's Node
// itself and writes the grown list back to the chain's head through
// `Parent.Child` (tabnas-yaml's yamlBlockList and yamlBlockElem rotation,
// and its yamlElemMap and yamlElemPair). The engine's bookkeeping never
// sees that hand-over, so NodeCell recognises it: a successor that holds
// the same container as the head of its chain (its parent's Child) or
// that head's cell, or as the rule it replaced, reports that rule's cell.
// A successor that put a container of a new kind in place of the one it
// was seeded with allocated it, and stays its own cell.
//
// Read-only. O(depth) at worst under a parent; a replacement chain with
// no parent (the start rule's own) is followed one predecessor at a time
// while each still holds the container. NoRule and nil return themselves.
func (r *Rule) NodeCell() *Rule {
	if r == nil || r == NoRule {
		return r
	}
	if r.snapshotNodeOwner != nil && r.snapshotNodeOwner != r {
		return r.snapshotNodeOwner.NodeCell()
	}
	for cur := r; ; {
		if c := cur.ownCell(); c != cur {
			return c
		}
		prev := cur.Prev
		if prev == nil || prev == NoRule || prev == cur {
			return cur
		}
		kind := nodeKind(cur.Node)
		if kind == nodeNone || kind != cur.ownedKind {
			// Not a container, or one the successor allocated itself.
			return cur
		}
		if p := cur.Parent; p != nil && p != NoRule {
			// The rule the parent pushed: the head of cur's chain, which
			// the parent reads and a write-back keeps current.
			if head := p.Child; head != nil && head != NoRule && head != cur {
				hc := head.NodeCell()
				if sameNode(hc.Node, cur.Node) || sameNode(head.Node, cur.Node) {
					return hc
				}
			}
		}
		if !sameNode(prev.Node, cur.Node) {
			return cur
		}
		cur = prev
	}
}

// ownCell is NodeCell without the replacement chain: the ownership
// bookkeeping, the kind guard and the unbroken Parent run.
func (r *Rule) ownCell() *Rule {
	h := r.nodeHolder()
	if h == r {
		return r
	}
	kind := nodeKind(r.Node)
	if kind != nodeNone && kind != r.ownedKind {
		// A container replaced what the rule was seeded with, and no
		// builtin recorded it: a Go action allocated it here.
		return r
	}
	top := r
	for p := r.Parent; p != nil && p != NoRule && sameNode(p.Node, r.Node); p = p.Parent {
		top = p
	}
	if top != r {
		return top.NodeCell()
	}
	if sameNode(h.Node, r.Node) {
		return h
	}
	return r
}

// SetNode replaces the container r.Node belongs to with v, in the rules
// that build into it: the cell (see NodeCell), r itself, the unbroken
// run of Parent rules holding the same container, and r.Next (the rule
// this pass created, seeded with r's node). It is the Go counterpart of
// writing through Rust's shared cell (`*rule.node.borrow_mut() = v`),
// and the way to truncate a list in place from a rule-done subscriber:
//
//	if l, ok := rule.NodeCell().Node.([]any); ok && 2 < len(l) {
//		rule.SetNode(l[:2]) // later appends grow from two elements
//	}
//
// Assigning `r.Node = v` instead changes r's own copy only; the rule that
// appends next still holds the old header and the change is lost.
//
// In a replacement chain (`r:`) whose rules write the grown list back to
// its head, the cell is that head, the rule the parent reads, and r.Next
// is the successor that appends next, so a truncation from any link
// sticks. SetNode does not change ownership, so the cell stays the same
// rule. Rules it does not reach keep the old value: in particular the
// links of a replacement chain between the cell and r, which nothing
// reads again. It does not walk that chain, which would cost the length
// of the list on every call. Nor can it reach a copy a grammar keeps
// outside Node (tabnas-yaml carries its list in K as well): a grammar
// that appends to such a copy discards the truncation. A rule whose node
// is not a container has nothing to share, and SetNode then sets r.Node
// alone.
func (r *Rule) SetNode(v any) {
	if r == nil || r == NoRule {
		return
	}
	old := r.Node
	cell := r.NodeCell()
	cell.Node = v
	r.Node = v
	for p := r.Parent; p != nil && p != NoRule && sameNode(p.Node, old); p = p.Parent {
		p.Node = v
	}
	if n := r.Next; n != nil && n != NoRule && n != r && sameNode(n.Node, old) {
		n.Node = v
	}
}

// Node kinds, as NodeCell compares them.
const (
	nodeNone uint8 = iota // a scalar, nil, Undefined: not a container
	nodeList              // []any or ListRef
	nodeMap               // map[string]any, *OrderedMap or MapRef
)

func nodeKind(v any) uint8 {
	switch v.(type) {
	case []any, ListRef:
		return nodeList
	case map[string]any, *OrderedMap, MapRef:
		return nodeMap
	}
	return nodeNone
}

// sameNode reports whether a and b are views of the same container.
//
// Maps compare by pointer. Lists compare by backing array, so a header
// grown in place or truncated still matches the one it came from. A
// zero-capacity list has no backing array of its own (every
// `make([]any, 0)` shares one), so two of those match only when one is a
// copy of the other's interface value, which is what a rule seeded from
// its pusher holds and what a fresh allocation is not (an empty ListRef
// compares by its Meta bag instead, which survives re-wrapping). Scalars,
// nil and Undefined are not containers and never match.
func sameNode(a, b any) bool {
	switch av := a.(type) {
	case *OrderedMap:
		bv, ok := b.(*OrderedMap)
		return ok && av != nil && av == bv
	case map[string]any:
		bv, ok := b.(map[string]any)
		return ok && av != nil && bv != nil &&
			reflect.ValueOf(av).Pointer() == reflect.ValueOf(bv).Pointer()
	case MapRef:
		bv, ok := b.(MapRef)
		return ok && av.Val != nil && bv.Val != nil &&
			reflect.ValueOf(av.Val).Pointer() == reflect.ValueOf(bv.Val).Pointer()
	case []any, ListRef:
		if reflect.TypeOf(a) != reflect.TypeOf(b) {
			return false
		}
		as, _ := listHeader(a)
		bs, _ := listHeader(b)
		if as == nil || bs == nil {
			return false
		}
		if 0 < cap(as) && 0 < cap(bs) {
			return unsafe.SliceData(as) == unsafe.SliceData(bs)
		}
		if 0 == cap(as) && 0 == cap(bs) {
			// A ListRef is re-wrapped whenever a field is set, which
			// boxes a new copy; its Meta bag is allocated once with it.
			if al, ok := a.(ListRef); ok && al.Meta != nil {
				bl := b.(ListRef)
				return bl.Meta != nil &&
					reflect.ValueOf(al.Meta).Pointer() == reflect.ValueOf(bl.Meta).Pointer()
			}
			return ifaceData(&a) == ifaceData(&b)
		}
		return false
	}
	return false
}

// ifaceData is the data word of an interface value: the address of the
// boxed copy made when a non-pointer value was converted to `any`. Two
// interface values share it only when one was copied from the other.
func ifaceData(v *any) unsafe.Pointer {
	return (*[2]unsafe.Pointer)(unsafe.Pointer(v))[1]
}
