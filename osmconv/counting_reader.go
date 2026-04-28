package osmconv

import "io"

// CountingReader wraps an io.Reader and reports bytes read to a ConvertProgress.
type CountingReader struct {
	r         io.Reader
	fileIndex int
	progress  ConvertProgress
}

// NewCountingReader wraps r so that each Read call reports the number of bytes
// read to progress.BytesRead(fileIndex, n).
func NewCountingReader(r io.Reader, fileIndex int, progress ConvertProgress) *CountingReader {
	return &CountingReader{r: r, fileIndex: fileIndex, progress: progress}
}

// newCountingReader is an unexported alias kept for internal tests.
func newCountingReader(r io.Reader, fileIndex int, progress ConvertProgress) *CountingReader {
	return NewCountingReader(r, fileIndex, progress)
}

func (cr *CountingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 {
		cr.progress.BytesRead(cr.fileIndex, int64(n))
	}
	return n, err
}
