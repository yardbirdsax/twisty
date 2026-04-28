package osmconv

import (
	"fmt"
)

// ConvertOptions configures the conversion pipeline.
type ConvertOptions struct {
	Scanners []ObjectScanner
	Writer   ObjectWriter
	Progress ConvertProgress // optional; nil defaults to NoopConvertProgress
}

// Convert reads from scanners, merges them in sorted order, and writes
// through the provided writer.
func Convert(opts ConvertOptions) error {
	if opts.Progress == nil {
		opts.Progress = NoopConvertProgress{}
	}

	merged := MergeScanners(opts.Scanners...)

	if err := opts.Writer.WriteHeader(); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}

	var count int64
	var batch int
	for merged.Next() {
		obj := merged.Object()
		if err := opts.Writer.WriteObject(obj); err != nil {
			return fmt.Errorf("writing object: %w", err)
		}
		count++
		batch++
		if count%100000 == 0 {
			opts.Progress.ObjectsWritten(batch)
			batch = 0
		}
	}
	if batch > 0 {
		opts.Progress.ObjectsWritten(batch)
	}
	if err := merged.Err(); err != nil {
		return fmt.Errorf("scanning: %w", err)
	}

	if err := opts.Writer.Close(); err != nil {
		return fmt.Errorf("closing writer: %w", err)
	}

	opts.Progress.Done()

	return nil
}
