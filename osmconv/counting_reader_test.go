package osmconv

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// spyConvertProgress is a test spy that records calls to BytesRead.
type spyConvertProgress struct {
	bytesReadCalls []bytesReadCall
}

type bytesReadCall struct {
	fileIndex int
	n         int64
}

func (s *spyConvertProgress) SetFiles([]string, []int64) {}
func (s *spyConvertProgress) BytesRead(fileIndex int, n int64) {
	s.bytesReadCalls = append(s.bytesReadCalls, bytesReadCall{fileIndex: fileIndex, n: n})
}
func (s *spyConvertProgress) ObjectsWritten(int) {}
func (s *spyConvertProgress) Done()              {}

var _ ConvertProgress = &spyConvertProgress{}

func TestCountingReader_ReportsBytesRead(t *testing.T) {
	data := []byte("hello, world")
	spy := &spyConvertProgress{}
	cr := newCountingReader(bytes.NewReader(data), 0, spy)

	buf := make([]byte, len(data))
	n, err := cr.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(data) {
		t.Fatalf("read %d bytes, want %d", n, len(data))
	}

	if len(spy.bytesReadCalls) != 1 {
		t.Fatalf("BytesRead called %d times, want 1", len(spy.bytesReadCalls))
	}
	if spy.bytesReadCalls[0].n != int64(len(data)) {
		t.Errorf("BytesRead n = %d, want %d", spy.bytesReadCalls[0].n, len(data))
	}
	if spy.bytesReadCalls[0].fileIndex != 0 {
		t.Errorf("BytesRead fileIndex = %d, want 0", spy.bytesReadCalls[0].fileIndex)
	}
}

func TestCountingReader_MultipleReads(t *testing.T) {
	data := []byte("abcdefghij") // 10 bytes
	spy := &spyConvertProgress{}
	cr := newCountingReader(bytes.NewReader(data), 2, spy)

	// Read in chunks of 3
	buf := make([]byte, 3)
	totalRead := 0
	for {
		n, err := cr.Read(buf)
		totalRead += n
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if totalRead != len(data) {
		t.Errorf("total bytes read = %d, want %d", totalRead, len(data))
	}

	// Verify all calls used fileIndex 2
	totalReported := int64(0)
	for _, call := range spy.bytesReadCalls {
		if call.fileIndex != 2 {
			t.Errorf("BytesRead fileIndex = %d, want 2", call.fileIndex)
		}
		totalReported += call.n
	}
	if totalReported != int64(len(data)) {
		t.Errorf("total bytes reported = %d, want %d", totalReported, len(data))
	}
}

func TestCountingReader_BytesReadNotCalledWhenZeroBytes(t *testing.T) {
	// Use a reader that returns (0, nil) on the first call, then data on the second.
	spy := &spyConvertProgress{}
	r := &zeroThenDataReader{data: []byte("hi")}
	cr := newCountingReader(r, 0, spy)

	buf := make([]byte, 10)

	// First read: returns 0, nil — BytesRead must not be called.
	n, err := cr.Read(buf)
	if n != 0 || err != nil {
		t.Fatalf("first Read: got (%d, %v), want (0, nil)", n, err)
	}
	if len(spy.bytesReadCalls) != 0 {
		t.Errorf("BytesRead called %d times after zero-byte read, want 0", len(spy.bytesReadCalls))
	}

	// Second read: returns actual data — BytesRead must be called.
	n, err = cr.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("second Read error: %v", err)
	}
	if n != 2 {
		t.Errorf("second Read: got %d bytes, want 2", n)
	}
	if len(spy.bytesReadCalls) != 1 {
		t.Errorf("BytesRead called %d times, want 1", len(spy.bytesReadCalls))
	}
}

// zeroThenDataReader returns (0, nil) on the first call, then returns its data.
type zeroThenDataReader struct {
	data      []byte
	firstDone bool
}

func (r *zeroThenDataReader) Read(p []byte) (int, error) {
	if !r.firstDone {
		r.firstDone = true
		return 0, nil
	}
	n := copy(p, r.data)
	return n, io.EOF
}

func TestCountingReader_ErrorPassthrough(t *testing.T) {
	sentinel := errors.New("read error")
	spy := &spyConvertProgress{}
	cr := newCountingReader(&errorReader{err: sentinel}, 1, spy)

	buf := make([]byte, 10)
	_, err := cr.Read(buf)
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want %v", err, sentinel)
	}
}

// errorReader always returns an error with zero bytes read.
type errorReader struct {
	err error
}

func (r *errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestCountingReader_EOFPassthrough(t *testing.T) {
	spy := &spyConvertProgress{}
	cr := newCountingReader(bytes.NewReader([]byte{}), 0, spy)

	buf := make([]byte, 10)
	n, err := cr.Read(buf)
	if !errors.Is(err, io.EOF) {
		t.Errorf("error = %v, want io.EOF", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
	// BytesRead should not have been called since no bytes were read.
	if len(spy.bytesReadCalls) != 0 {
		t.Errorf("BytesRead called %d times, want 0", len(spy.bytesReadCalls))
	}
}

func TestCountingReader_FileIndexPropagated(t *testing.T) {
	data := []byte("test")
	spy := &spyConvertProgress{}
	cr := newCountingReader(bytes.NewReader(data), 5, spy)

	buf := make([]byte, len(data))
	_, err := cr.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(spy.bytesReadCalls) == 0 {
		t.Fatal("BytesRead was not called")
	}
	if spy.bytesReadCalls[0].fileIndex != 5 {
		t.Errorf("fileIndex = %d, want 5", spy.bytesReadCalls[0].fileIndex)
	}
}
