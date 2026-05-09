package slirp

import "testing"

// TestQueueHeadInit verifies that Init creates an empty circular list.
func TestQueueHeadInit(t *testing.T) {
	var q QueueHead
	q.Init()

	if q.Link != &q {
		t.Error("Init: Link should point to self")
	}
	if q.RLink != &q {
		t.Error("Init: RLink should point to self")
	}
	if !q.IsEmpty() {
		t.Error("Init: queue should be empty")
	}
}

// TestInsqueSingleElement verifies inserting a single element.
func TestInsqueSingleElement(t *testing.T) {
	var head, elem QueueHead
	head.Init()

	Insque(&elem, &head)

	// After insert: head -> elem -> head (circular)
	if head.Link != &elem {
		t.Error("head.Link should point to elem")
	}
	if elem.Link != &head {
		t.Error("elem.Link should point to head")
	}
	if elem.RLink != &head {
		t.Error("elem.RLink should point to head")
	}
	if head.RLink != &elem {
		t.Error("head.RLink should point to elem")
	}
	if head.IsEmpty() {
		t.Error("queue should not be empty after insert")
	}
}

// TestInsqueMultipleElements verifies inserting multiple elements.
// Reference: tinyemu-2019-12-21/slirp/misc.c:19-29
func TestInsqueMultipleElements(t *testing.T) {
	var head, e1, e2, e3 QueueHead
	head.Init()

	// Insert elements: they go after head, so order is head -> e3 -> e2 -> e1 -> head
	Insque(&e1, &head)
	Insque(&e2, &head)
	Insque(&e3, &head)

	// Verify forward links: head -> e3 -> e2 -> e1 -> head
	if head.Link != &e3 {
		t.Error("head.Link should be e3")
	}
	if e3.Link != &e2 {
		t.Error("e3.Link should be e2")
	}
	if e2.Link != &e1 {
		t.Error("e2.Link should be e1")
	}
	if e1.Link != &head {
		t.Error("e1.Link should be head")
	}

	// Verify backward links: head <- e3 <- e2 <- e1 <- head
	if head.RLink != &e1 {
		t.Error("head.RLink should be e1")
	}
	if e1.RLink != &e2 {
		t.Error("e1.RLink should be e2")
	}
	if e2.RLink != &e3 {
		t.Error("e2.RLink should be e3")
	}
	if e3.RLink != &head {
		t.Error("e3.RLink should be head")
	}
}

// TestRemqueSingleElement verifies removing the only element.
// Reference: tinyemu-2019-12-21/slirp/misc.c:31-38
func TestRemqueSingleElement(t *testing.T) {
	var head, elem QueueHead
	head.Init()
	Insque(&elem, &head)

	Remque(&elem)

	// After remove: head should be circular to itself again
	if head.Link != &head {
		t.Error("head.Link should point to self after remove")
	}
	if head.RLink != &head {
		t.Error("head.RLink should point to self after remove")
	}
	if !head.IsEmpty() {
		t.Error("queue should be empty after removing only element")
	}
	// elem.RLink should be nil per C behavior
	if elem.RLink != nil {
		t.Error("removed element RLink should be nil")
	}
}

// TestRemqueMiddleElement verifies removing an element from the middle.
func TestRemqueMiddleElement(t *testing.T) {
	var head, e1, e2, e3 QueueHead
	head.Init()

	Insque(&e1, &head)
	Insque(&e2, &head)
	Insque(&e3, &head)
	// Order: head -> e3 -> e2 -> e1 -> head

	Remque(&e2)
	// Order should be: head -> e3 -> e1 -> head

	if e3.Link != &e1 {
		t.Error("e3.Link should be e1 after removing e2")
	}
	if e1.RLink != &e3 {
		t.Error("e1.RLink should be e3 after removing e2")
	}
	if e2.RLink != nil {
		t.Error("removed element RLink should be nil")
	}
}

// TestRemqueFirstElement verifies removing the first element after head.
func TestRemqueFirstElement(t *testing.T) {
	var head, e1, e2 QueueHead
	head.Init()

	Insque(&e1, &head)
	Insque(&e2, &head)
	// Order: head -> e2 -> e1 -> head

	Remque(&e2)
	// Order should be: head -> e1 -> head

	if head.Link != &e1 {
		t.Error("head.Link should be e1 after removing e2")
	}
	if e1.RLink != &head {
		t.Error("e1.RLink should be head after removing e2")
	}
}

// TestRemqueLastElement verifies removing the last element before head.
func TestRemqueLastElement(t *testing.T) {
	var head, e1, e2 QueueHead
	head.Init()

	Insque(&e1, &head)
	Insque(&e2, &head)
	// Order: head -> e2 -> e1 -> head

	Remque(&e1)
	// Order should be: head -> e2 -> head

	if head.RLink != &e2 {
		t.Error("head.RLink should be e2 after removing e1")
	}
	if e2.Link != &head {
		t.Error("e2.Link should be head after removing e1")
	}
}

// TestQueueTraversal verifies we can traverse the queue in both directions.
func TestQueueTraversal(t *testing.T) {
	var head QueueHead
	head.Init()

	elems := make([]QueueHead, 5)
	for i := range elems {
		Insque(&elems[i], &head)
	}
	// Order: head -> e4 -> e3 -> e2 -> e1 -> e0 -> head

	// Forward traversal
	count := 0
	for q := head.Link; q != &head; q = q.Link {
		count++
		if count > 10 {
			t.Fatal("infinite loop in forward traversal")
		}
	}
	if count != 5 {
		t.Errorf("forward traversal: expected 5 elements, got %d", count)
	}

	// Backward traversal
	count = 0
	for q := head.RLink; q != &head; q = q.RLink {
		count++
		if count > 10 {
			t.Fatal("infinite loop in backward traversal")
		}
	}
	if count != 5 {
		t.Errorf("backward traversal: expected 5 elements, got %d", count)
	}
}
