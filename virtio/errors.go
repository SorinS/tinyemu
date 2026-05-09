package virtio

import "errors"

// Error definitions for the VirtIO package
var (
	ErrNoRAM         = errors.New("no RAM at address")
	ErrDescChainEnd  = errors.New("descriptor chain ended unexpectedly")
	ErrDescWrongType = errors.New("descriptor has wrong type (read vs write)")
	ErrInvalidQueue  = errors.New("invalid queue index")
	ErrQueueNotReady = errors.New("queue is not ready")
)
