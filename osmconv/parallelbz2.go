package osmconv

import (
	"bytes"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/dsnet/compress/bzip2"
)

const bz2BlockSize = 900 * 1024 // 900KB, matching BZ2 default block size

// compressedBlock holds a compressed BZ2 stream with its sequence number for
// in-order reassembly.
type compressedBlock struct {
	seq  int
	data []byte
}

// workItem is sent to worker goroutines for compression.
type workItem struct {
	seq  int
	data []byte
}

// ParallelBZ2Writer compresses data using multiple goroutines, each producing
// an independent BZ2 stream. BZ2 supports concatenated streams, so the result
// is a valid single BZ2 file that decompresses correctly.
type ParallelBZ2Writer struct {
	w       io.Writer
	workers int

	// buf accumulates incoming writes until a full block is ready
	buf []byte

	// pipeline channels
	workCh   chan workItem
	resultCh chan compressedBlock

	// next sequence number to assign
	nextSeq int

	// reassembly goroutine signals done via this channel
	reassemblyDone chan error

	// signal workers to stop (closed on Close)
	wg sync.WaitGroup

	// done is closed on the first error to unblock goroutines waiting on channels
	done     chan struct{}
	doneOnce sync.Once

	// any error from the reassembly or worker goroutines
	mu  sync.Mutex
	err error
}

// NewParallelBZ2Writer creates a ParallelBZ2Writer that writes compressed data
// to w using the specified number of worker goroutines. If workers <= 0,
// runtime.GOMAXPROCS(0) is used.
func NewParallelBZ2Writer(w io.Writer, workers int) (*ParallelBZ2Writer, error) {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}

	// Allow up to 2x workers in-flight to keep workers busy without unbounded
	// memory growth.
	inFlight := workers * 2

	p := &ParallelBZ2Writer{
		w:              w,
		workers:        workers,
		buf:            make([]byte, 0, bz2BlockSize),
		workCh:         make(chan workItem, inFlight),
		resultCh:       make(chan compressedBlock, inFlight),
		reassemblyDone: make(chan error, 1),
		done:           make(chan struct{}),
	}

	// Start worker goroutines.
	for range workers {
		p.wg.Add(1)
		go p.worker()
	}

	// Start reassembly goroutine.
	go p.reassemble()

	return p, nil
}

// Write buffers data and dispatches full blocks to workers.
func (p *ParallelBZ2Writer) Write(b []byte) (int, error) {
	if err := p.getErr(); err != nil {
		return 0, err
	}

	total := len(b)
	for len(b) > 0 {
		space := bz2BlockSize - len(p.buf)
		take := min(len(b), space)
		p.buf = append(p.buf, b[:take]...)
		b = b[take:]

		if len(p.buf) >= bz2BlockSize {
			if err := p.dispatchBlock(p.buf); err != nil {
				return 0, err
			}
			p.buf = p.buf[:0]
		}
	}
	return total, nil
}

// Close flushes any remaining buffered data, waits for all workers to finish,
// and closes the underlying pipeline.
func (p *ParallelBZ2Writer) Close() error {
	// Flush partial block if any. Ignore the error here — it is captured in
	// p.err and returned below. Teardown must always run regardless of whether
	// dispatchBlock succeeds, to avoid leaking worker goroutines.
	if len(p.buf) > 0 {
		_ = p.dispatchBlock(p.buf)
		p.buf = p.buf[:0]
	}

	// Close work channel to signal workers there is no more input.
	close(p.workCh)

	// Wait for all workers to finish, then close the result channel so the
	// reassembly goroutine can exit.
	p.wg.Wait()
	close(p.resultCh)

	// Wait for reassembly to finish writing.
	if err := <-p.reassemblyDone; err != nil {
		return err
	}

	return p.getErr()
}

// dispatchBlock sends a copy of data to a worker goroutine.
func (p *ParallelBZ2Writer) dispatchBlock(data []byte) error {
	if err := p.getErr(); err != nil {
		return err
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	select {
	case p.workCh <- workItem{seq: p.nextSeq, data: cp}:
		p.nextSeq++
		return nil
	case <-p.done:
		return p.getErr()
	}
}

// worker compresses blocks received from workCh and sends results to resultCh.
func (p *ParallelBZ2Writer) worker() {
	defer p.wg.Done()
	for item := range p.workCh {
		compressed, err := compressBlock(item.data)
		if err != nil {
			p.setErr(fmt.Errorf("compressing block %d: %w", item.seq, err))
			p.closeDone()
			return
		}
		select {
		case p.resultCh <- compressedBlock{seq: item.seq, data: compressed}:
		case <-p.done:
			return
		}
	}
}

// reassemble receives compressed blocks from resultCh and writes them to the
// underlying writer in input order.
func (p *ParallelBZ2Writer) reassemble() {
	pending := make(map[int][]byte)
	nextWrite := 0

	for block := range p.resultCh {
		pending[block.seq] = block.data

		// Write all consecutive blocks that are ready.
		for {
			data, ok := pending[nextWrite]
			if !ok {
				break
			}
			delete(pending, nextWrite)
			if _, err := p.w.Write(data); err != nil {
				p.setErr(fmt.Errorf("writing compressed block %d: %w", nextWrite, err))
				p.closeDone()
				// Drain resultCh so workers unblock and can exit.
				for range p.resultCh {
				}
				p.reassemblyDone <- p.getErr()
				return
			}
			nextWrite++
		}
	}

	p.reassemblyDone <- nil
}

// closeDone closes the done channel exactly once.
func (p *ParallelBZ2Writer) closeDone() {
	p.doneOnce.Do(func() { close(p.done) })
}

// compressBlock compresses data as a single BZ2 stream and returns the bytes.
func compressBlock(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	bw, err := bzip2.NewWriter(&buf, &bzip2.WriterConfig{Level: bzip2.DefaultCompression})
	if err != nil {
		return nil, fmt.Errorf("creating bzip2 writer: %w", err)
	}
	if _, err := bw.Write(data); err != nil {
		return nil, fmt.Errorf("writing to bzip2 writer: %w", err)
	}
	if err := bw.Close(); err != nil {
		return nil, fmt.Errorf("closing bzip2 writer: %w", err)
	}
	return buf.Bytes(), nil
}

func (p *ParallelBZ2Writer) setErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err == nil {
		p.err = err
	}
}

func (p *ParallelBZ2Writer) getErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}
