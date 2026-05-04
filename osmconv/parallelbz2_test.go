package osmconv

import (
	stdbzip2 "compress/bzip2"
	"bytes"
	"errors"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/dsnet/compress/bzip2"
)

// roundTrip compresses data with ParallelBZ2Writer using the given worker
// count and decompresses it with the standard library bzip2 reader, returning
// the decompressed bytes.
func roundTrip(t *testing.T, data []byte, workers int) []byte {
	t.Helper()

	var buf bytes.Buffer
	w, err := NewParallelBZ2Writer(&buf, workers)
	if err != nil {
		t.Fatalf("NewParallelBZ2Writer: %v", err)
	}

	if _, err := w.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r := stdbzip2.NewReader(&buf)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("decompressing: %v", err)
	}
	return got
}

// TestParallelBZ2_SmallData verifies a round-trip for data smaller than one block.
func TestParallelBZ2_SmallData(t *testing.T) {
	input := []byte("hello, parallel bzip2 world")
	got := roundTrip(t, input, 2)
	if !bytes.Equal(got, input) {
		t.Errorf("round-trip mismatch: got %q, want %q", got, input)
	}
}

// TestParallelBZ2_LargeData verifies a round-trip for ~5MB of data spanning
// multiple blocks.
func TestParallelBZ2_LargeData(t *testing.T) {
	const size = 5 * 1024 * 1024
	input := make([]byte, size)
	// Fill with a non-trivial repeating pattern so compression is meaningful.
	for i := range input {
		input[i] = byte(i*7+13) ^ byte(i>>8)
	}

	got := roundTrip(t, input, 4)
	if !bytes.Equal(got, input) {
		t.Errorf("large data round-trip mismatch: lengths got=%d want=%d", len(got), len(input))
	}
}

// TestParallelBZ2_SingleWorker verifies correctness with exactly one worker.
func TestParallelBZ2_SingleWorker(t *testing.T) {
	input := make([]byte, 2*bz2BlockSize+100)
	for i := range input {
		input[i] = byte(i % 251)
	}

	got := roundTrip(t, input, 1)
	if !bytes.Equal(got, input) {
		t.Errorf("single-worker round-trip mismatch: lengths got=%d want=%d", len(got), len(input))
	}
}

// TestParallelBZ2_ConcurrentCorrectness verifies that using 8 workers produces
// output that decompresses back to the original input in the correct order.
func TestParallelBZ2_ConcurrentCorrectness(t *testing.T) {
	const size = 10 * 1024 * 1024
	input := make([]byte, size)
	for i := range input {
		input[i] = byte(i*3+7) ^ byte(i>>4)
	}

	got := roundTrip(t, input, 8)
	if !bytes.Equal(got, input) {
		t.Errorf("concurrent round-trip mismatch: lengths got=%d want=%d", len(got), len(input))
	}
}

// TestParallelBZ2_CloseFlushesPartialBlock verifies that Close flushes any
// data that has not yet filled a complete block.
func TestParallelBZ2_CloseFlushesPartialBlock(t *testing.T) {
	// Write less than one block.
	input := bytes.Repeat([]byte("flush me"), 1000)

	var buf bytes.Buffer
	w, err := NewParallelBZ2Writer(&buf, 2)
	if err != nil {
		t.Fatalf("NewParallelBZ2Writer: %v", err)
	}
	if _, err := w.Write(input); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Close must flush the partial block.
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected compressed output after Close, got nothing")
	}

	r := stdbzip2.NewReader(&buf)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("decompressing: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Errorf("partial-block round-trip mismatch: lengths got=%d want=%d", len(got), len(input))
	}
}

// TestParallelBZ2_WriterError verifies that a failing underlying writer does
// not deadlock Close.
func TestParallelBZ2_WriterError(t *testing.T) {
	// errorWriter fails immediately on every Write.
	ew := &errorWriter{}
	w, err := NewParallelBZ2Writer(ew, 4)
	if err != nil {
		t.Fatalf("NewParallelBZ2Writer: %v", err)
	}

	// Write enough data to fill multiple blocks so workers are busy.
	input := make([]byte, 5*bz2BlockSize)
	for i := range input {
		input[i] = byte(i % 251)
	}

	// Write and Close must not deadlock.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = w.Write(input)
		_ = w.Close()
	}()

	select {
	case <-done:
		// success — did not deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("Close deadlocked when underlying writer failed")
	}
}

// errorWriter fails on every Write.
type errorWriter struct{}

func (e *errorWriter) Write(p []byte) (int, error) {
	return 0, errors.New("errorWriter: write failed")
}

// BenchmarkSerialBZ2 measures single-threaded compression using dsnet bzip2
// as a baseline.
func BenchmarkSerialBZ2(b *testing.B) {
	data := makeBenchData(5 * 1024 * 1024)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for b.Loop() {
		var buf bytes.Buffer
		w, err := bzip2.NewWriter(&buf, &bzip2.WriterConfig{Level: bzip2.DefaultCompression})
		if err != nil {
			b.Fatalf("NewWriter: %v", err)
		}
		if _, err := w.Write(data); err != nil {
			b.Fatalf("Write: %v", err)
		}
		if err := w.Close(); err != nil {
			b.Fatalf("Close: %v", err)
		}
	}
}

// BenchmarkParallelBZ2_1 measures ParallelBZ2Writer with 1 worker.
func BenchmarkParallelBZ2_1(b *testing.B) {
	benchmarkParallel(b, 1)
}

// BenchmarkParallelBZ2_4 measures ParallelBZ2Writer with 4 workers.
func BenchmarkParallelBZ2_4(b *testing.B) {
	benchmarkParallel(b, 4)
}

// BenchmarkParallelBZ2_N measures ParallelBZ2Writer with runtime.GOMAXPROCS(0) workers.
func BenchmarkParallelBZ2_N(b *testing.B) {
	benchmarkParallel(b, runtime.GOMAXPROCS(0))
}

func benchmarkParallel(b *testing.B, workers int) {
	b.Helper()
	data := makeBenchData(5 * 1024 * 1024)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for b.Loop() {
		var buf bytes.Buffer
		w, err := NewParallelBZ2Writer(&buf, workers)
		if err != nil {
			b.Fatalf("NewParallelBZ2Writer: %v", err)
		}
		if _, err := w.Write(data); err != nil {
			b.Fatalf("Write: %v", err)
		}
		if err := w.Close(); err != nil {
			b.Fatalf("Close: %v", err)
		}
	}
}

func makeBenchData(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*7+13) ^ byte(i>>8)
	}
	return data
}
