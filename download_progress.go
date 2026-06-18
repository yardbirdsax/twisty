package main

// FileDownloadProgress reports progress for a single in-flight PBF download.
// Each call to DownloadProgress.StartFile returns one of these.
type FileDownloadProgress interface {
	BytesDownloaded(n int64)
	FileComplete()
}

// DownloadProgress manages progress across all PBF file downloads.
type DownloadProgress interface {
	// StartFile begins tracking a new file download and returns a
	// FileDownloadProgress the caller uses to report bytes and completion.
	// totalBytes is Content-Length (-1 if unknown). fileIndex is 0-based.
	StartFile(name string, fileIndex int, totalFiles int, totalBytes int64) FileDownloadProgress

	// Done signals that all downloads are complete.
	Done()
}

// NoopFileDownloadProgress is a FileDownloadProgress that does nothing.
type NoopFileDownloadProgress struct{}

func (NoopFileDownloadProgress) BytesDownloaded(int64) {}
func (NoopFileDownloadProgress) FileComplete()         {}

var _ FileDownloadProgress = NoopFileDownloadProgress{}

// NoopDownloadProgress is a DownloadProgress that does nothing.
type NoopDownloadProgress struct{}

func (NoopDownloadProgress) StartFile(string, int, int, int64) FileDownloadProgress {
	return NoopFileDownloadProgress{}
}
func (NoopDownloadProgress) Done() {}

var _ DownloadProgress = NoopDownloadProgress{}
