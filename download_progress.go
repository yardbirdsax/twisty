package main

// DownloadProgress reports progress during PBF file downloads.
type DownloadProgress interface {
	// StartFile begins progress tracking for a new file download.
	// totalBytes is the Content-Length (-1 if unknown). fileIndex is 0-based,
	// totalFiles is the total number of files to download.
	StartFile(name string, fileIndex int, totalFiles int, totalBytes int64)

	// BytesDownloaded reports that n additional bytes have been downloaded
	// for the current file.
	BytesDownloaded(n int64)

	// FileComplete signals that the current file download is finished.
	FileComplete()

	// Done signals that all downloads are complete.
	Done()
}

// NoopDownloadProgress is a DownloadProgress that does nothing.
// It is the default when no progress reporting is needed.
type NoopDownloadProgress struct{}

func (NoopDownloadProgress) StartFile(string, int, int, int64) {}
func (NoopDownloadProgress) BytesDownloaded(int64)             {}
func (NoopDownloadProgress) FileComplete()                     {}
func (NoopDownloadProgress) Done()                             {}

var _ DownloadProgress = NoopDownloadProgress{}
