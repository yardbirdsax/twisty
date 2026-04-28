package osmconv

import "io"

type countingReader struct {
	r         io.Reader
	fileIndex int
	progress  ConvertProgress
}

func newCountingReader(r io.Reader, fileIndex int, progress ConvertProgress) *countingReader {
	return &countingReader{r: r, fileIndex: fileIndex, progress: progress}
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 {
		cr.progress.BytesRead(cr.fileIndex, int64(n))
	}
	return n, err
}
