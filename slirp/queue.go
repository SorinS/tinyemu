// Package slirp provides user-mode TCP/IP networking.
// Reference: tinyemu-2019-12-21/slirp/
package slirp

// QueueHead is a doubly-linked list element. Structures that need to be
// queued should embed this as their first field.
//
// Reference: tinyemu-2019-12-21/slirp/misc.c:14-17
type QueueHead struct {
	Link  *QueueHead // Next element (qh_link in C)
	RLink *QueueHead // Previous element (qh_rlink in C)
}

// Init initializes a queue head as an empty circular list.
// After Init, q.Link == q and q.RLink == q.
func (q *QueueHead) Init() {
	q.Link = q
	q.RLink = q
}

// Insque inserts element after head in a doubly-linked queue.
// This matches BSD insque() semantics exactly.
//
// Reference: tinyemu-2019-12-21/slirp/misc.c:19-29
func Insque(element, head *QueueHead) {
	// element->qh_link = head->qh_link;
	element.Link = head.Link
	// head->qh_link = element;
	head.Link = element
	// element->qh_rlink = head;
	element.RLink = head
	// element->qh_link->qh_rlink = element;
	element.Link.RLink = element
}

// Remque removes element from a doubly-linked queue.
// This matches BSD remque() semantics exactly.
//
// Reference: tinyemu-2019-12-21/slirp/misc.c:31-38
func Remque(element *QueueHead) {
	// element->qh_link->qh_rlink = element->qh_rlink;
	element.Link.RLink = element.RLink
	// element->qh_rlink->qh_link = element->qh_link;
	element.RLink.Link = element.Link
	// element->qh_rlink = NULL;
	element.RLink = nil
}

// IsEmpty returns true if the queue is empty (only contains the sentinel).
func (q *QueueHead) IsEmpty() bool {
	return q.Link == q
}
