package main

import "testing"

func TestForkDoesNotInheritAssignment(t *testing.T) {
	g := newAssignmentGate()
	parent, _ := g.register("parent")
	first, _ := g.register("first")
	second, _ := g.register("second")
	for _, id := range []string{"first", "second"} {
		if ok, err := g.admit(id, parent, "parent-task"); ok || err == nil {
			t.Fatal("parent work admitted by fork")
		}
	}
	if ok, err := g.admit("first", second, "cross-fork"); ok || err == nil {
		t.Fatal("sibling lease admitted")
	}
	if ok, err := g.admit("first", first, "fresh"); !ok || err != nil {
		t.Fatal(err)
	}
	if ok, err := g.admit("first", first, "fresh"); ok || err != nil {
		t.Fatal("duplicate was not suppressed")
	}
	if ok, err := g.admit("first", first, "other"); ok || err == nil {
		t.Fatal("conflicting assignment admitted")
	}
}

func TestRenewedLeaseFencesOldController(t *testing.T) {
	g := newAssignmentGate()
	old, _ := g.register("copy")
	current, _ := g.register("copy")
	if ok, err := g.admit("copy", old, "old"); ok || err == nil {
		t.Fatal("stale lease admitted")
	}
	if ok, err := g.admit("copy", current, "new"); !ok || err != nil {
		t.Fatal(err)
	}
}
