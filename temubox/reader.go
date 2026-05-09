package temubox

import (
	"context"
	"io"
	"sync"
)

type dummyReader struct{}

func (dummyReader) Read(p []byte) (n int, err error) { return 0, io.EOF }

type dummyWriter struct{}

func (dummyWriter) Write(p []byte) (n int, err error) { return len(p), nil }

type nbReader struct {
	readBuf []byte
	newData chan []byte
	pool    sync.Pool
	mtx     sync.Mutex
	err     error
}

const nbReaderCap = 4096

func startNonblockingReader(ctx context.Context, reader io.Reader) *nbReader {
	r := &nbReader{
		readBuf: make([]byte, 0, nbReaderCap),
		newData: make(chan []byte),
	}
	r.pool.New = func() any { return make([]byte, 0, nbReaderCap) }

	go func() {
		defer close(r.newData)
		var rvErr error
		defer func() {
			r.mtx.Lock()
			defer r.mtx.Unlock()
			r.err = rvErr
		}()
		for {
			if ctx.Err() != nil {
				rvErr = ctx.Err()
				return
			}
			buf := r.pool.Get().([]byte)[:nbReaderCap]
			n, err := reader.Read(buf)
			if err != nil {
				rvErr = err
				return
			}
			select {
			case r.newData <- buf[:n]:
			case <-ctx.Done():
			}
		}
	}()

	return r
}

func (r *nbReader) Read(p []byte) (n int, err error) {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	if len(r.readBuf) == 0 {
		select {
		default:
			return 0, nil
		case newBuf, ok := <-r.newData:
			if !ok {
				return 0, r.err
			}
			oldBuf := r.readBuf
			r.readBuf = newBuf
			r.pool.Put(oldBuf)
		}
	}

	n = min(len(r.readBuf), len(p))
	copy(p[:n], r.readBuf[:n])
	r.readBuf = r.readBuf[:copy(r.readBuf, r.readBuf[n:])]
	return n, nil
}
