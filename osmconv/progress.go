package osmconv

// ConvertProgress reports progress during PBF-to-BZ2 conversion.
type ConvertProgress interface {
	// SetFiles configures the progress reporter with the list of input files
	// and their sizes in bytes.
	SetFiles(names []string, sizes []int64)

	// BytesRead reports that n bytes have been read from the file at fileIndex.
	BytesRead(fileIndex int, n int64)

	// ObjectsWritten reports that n objects have been written to the output.
	ObjectsWritten(n int)

	// Done signals that conversion is complete.
	Done()
}

// NoopConvertProgress is a ConvertProgress that does nothing.
// It is the default when no progress reporting is needed.
type NoopConvertProgress struct{}

func (NoopConvertProgress) SetFiles([]string, []int64) {}
func (NoopConvertProgress) BytesRead(int, int64)       {}
func (NoopConvertProgress) ObjectsWritten(int)         {}
func (NoopConvertProgress) Done()                      {}

var _ ConvertProgress = NoopConvertProgress{}
