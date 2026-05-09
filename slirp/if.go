package slirp

// Interface queue functions for packet output.
// Reference: tinyemu-2019-12-21/slirp/if.c

// ifsInit initializes an mbuf as a session chain head.
// Reference: tinyemu-2019-12-21/slirp/if.c:10
func ifsInit(ifm *Mbuf) {
	ifm.NextPkt = ifm
	ifm.PrevPkt = ifm
}

// ifsInsque inserts ifm into the session chain after ifmhead.
// Reference: tinyemu-2019-12-21/slirp/if.c:13-19
func ifsInsque(ifm, ifmhead *Mbuf) {
	ifm.NextPkt = ifmhead.NextPkt
	ifmhead.NextPkt = ifm
	ifm.PrevPkt = ifmhead
	ifm.NextPkt.PrevPkt = ifm
}

// ifsRemque removes ifm from the session chain.
// Reference: tinyemu-2019-12-21/slirp/if.c:22-26
func ifsRemque(ifm *Mbuf) {
	ifm.PrevPkt.NextPkt = ifm.NextPkt
	ifm.NextPkt.PrevPkt = ifm.PrevPkt
}

// IfInit initializes the interface output queues.
// Reference: tinyemu-2019-12-21/slirp/if.c:29-34
func (s *Slirp) IfInit() {
	s.IfFastQ.Next = &s.IfFastQ
	s.IfFastQ.Prev = &s.IfFastQ
	s.IfBatchQ.Next = &s.IfBatchQ
	s.IfBatchQ.Prev = &s.IfBatchQ
	s.NextM = &s.IfBatchQ
}

// insque inserts element after head using Next/Prev pointers.
// This is for the interface queue (ifq_next/ifq_prev which are m_next/m_prev).
// Reference: tinyemu-2019-12-21/slirp/misc.c:19-29
func mbufIfqInsque(element, head *Mbuf) {
	element.Next = head.Next
	head.Next = element
	element.Prev = head
	element.Next.Prev = element
}

// remque removes element from the interface queue.
// Reference: tinyemu-2019-12-21/slirp/misc.c:31-38
func mbufIfqRemque(element *Mbuf) {
	element.Next.Prev = element.Prev
	element.Prev.Next = element.Next
	element.Prev = nil
}

// IfOutput queues a packet for output on the network interface.
// There are 2 output queues: if_fastq for interactive sessions and if_batchq
// for bulk data. Each queue is a doubly-linked list of doubly-linked lists
// of mbufs, with each inner list belonging to one session (socket).
// Reference: tinyemu-2019-12-21/slirp/if.c:50-139
func (s *Slirp) IfOutput(so *Socket, ifm *Mbuf) {
	var ifq *Mbuf
	onFastQ := true

	// First remove the mbuf from m_usedlist,
	// since we're going to use m_next and m_prev ourselves
	// Reference: tinyemu-2019-12-21/slirp/if.c:65-68
	if ifm.Flags&MBufUsedList != 0 {
		mbufRemque(ifm)
		ifm.Flags &= ^MBufUsedList
	}

	// See if there's already a batchq list for this session.
	// This can include an interactive session which should go on fastq,
	// but gets too greedy... hence it'll be downgraded from fastq to batchq.
	// We mustn't put this packet back on the fastq (or we'll send it out of order)
	// Reference: tinyemu-2019-12-21/slirp/if.c:77-85
	for ifq = s.IfBatchQ.Prev; ifq != &s.IfBatchQ; ifq = ifq.Prev {
		if so == ifq.So {
			// A match!
			ifm.So = so
			ifsInsque(ifm, ifq.PrevPkt)
			goto diddit
		}
	}

	// No match, check which queue to put it on
	// Reference: tinyemu-2019-12-21/slirp/if.c:88-101
	if so != nil && (so.SoIPTos&IPTOSLowDelay) != 0 {
		ifq = s.IfFastQ.Prev
		onFastQ = true
		// Check if this packet is a part of the last packet's session
		if ifq.So == so {
			ifm.So = so
			ifsInsque(ifm, ifq.PrevPkt)
			goto diddit
		}
	} else {
		ifq = s.IfBatchQ.Prev
	}

	// Create a new doubly linked list for this session
	// Reference: tinyemu-2019-12-21/slirp/if.c:103-106
	ifm.So = so
	ifsInit(ifm)
	mbufIfqInsque(ifm, ifq)

diddit:
	s.IfQueued++

	if so != nil {
		// Update *_queued counters
		// Reference: tinyemu-2019-12-21/slirp/if.c:111-114
		so.SoQueued++
		so.SoNQueued++

		// Check if the interactive session should be downgraded to
		// the batchq. A session is downgraded if it has queued 6
		// packets without pausing, and at least 3 of those packets
		// have been sent over the link.
		// Reference: tinyemu-2019-12-21/slirp/if.c:122-130
		if onFastQ && so.SoNQueued >= 6 && (so.SoNQueued-so.SoQueued) >= 3 {
			// Remove from current queue...
			mbufIfqRemque(ifm.NextPkt)
			// ...And insert in the new. That'll teach ya!
			mbufIfqInsque(ifm.NextPkt, &s.IfBatchQ)
		}
	}

	// This prevents us from malloc()ing too many mbufs
	// Reference: tinyemu-2019-12-21/slirp/if.c:137
	s.IfStart()
}

// IfStart sends packets from the output queues.
// Packets on if_fastq get absolute priority. For if_batchq, we send
// one packet from each session in round-robin fashion for fairness.
// Reference: tinyemu-2019-12-21/slirp/if.c:154-209
func (s *Slirp) IfStart() {
	var ifm, ifqt *Mbuf

	if s.IfQueued == 0 {
		return // Nothing to do
	}

again:
	// Check if we can really output
	// Reference: tinyemu-2019-12-21/slirp/if.c:165-166
	if s.CanOutput != nil && !s.CanOutput(s.Opaque) {
		return
	}

	// See which queue to get next packet from
	// If there's something in the fastq, select it immediately
	// Reference: tinyemu-2019-12-21/slirp/if.c:172-183
	if s.IfFastQ.Next != &s.IfFastQ {
		ifm = s.IfFastQ.Next
	} else {
		// Nothing on fastq, see if next_m is valid
		if s.NextM != &s.IfBatchQ {
			ifm = s.NextM
		} else {
			ifm = s.IfBatchQ.Next
		}
		// Set which packet to send on next iteration
		s.NextM = ifm.Next
	}

	// Remove it from the queue
	// Reference: tinyemu-2019-12-21/slirp/if.c:185-187
	ifqt = ifm.Prev
	mbufIfqRemque(ifm)
	s.IfQueued--

	// If there are more packets for this session, re-queue them
	// Reference: tinyemu-2019-12-21/slirp/if.c:190-193
	if ifm.NextPkt != ifm {
		mbufIfqInsque(ifm.NextPkt, ifqt)
		ifsRemque(ifm)
	}

	// Update so_queued
	// Reference: tinyemu-2019-12-21/slirp/if.c:196-200
	if ifm.So != nil {
		ifm.So.SoQueued--
		if ifm.So.SoQueued == 0 {
			// If there's no more queued, reset nqueued
			ifm.So.SoNQueued = 0
		}
	}

	// Encapsulate the packet for sending
	// Reference: tinyemu-2019-12-21/slirp/if.c:203
	s.IfEncap(ifm.Data[:ifm.Len])

	// Free the mbuf
	// Reference: tinyemu-2019-12-21/slirp/if.c:205
	ifm.Free()

	if s.IfQueued > 0 {
		goto again
	}
}
