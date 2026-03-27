package osmconv

import (
	"fmt"
	"io"
)

// ConvertOptions configures the conversion pipeline.
type ConvertOptions struct {
	Scanners []ObjectScanner
	Writer   ObjectWriter
	Progress io.Writer // optional; nil disables progress
}

// Convert reads from scanners, merges them in sorted order, and writes
// through the provided writer.
func Convert(opts ConvertOptions) error {
	merged := MergeScanners(opts.Scanners...)

	if err := opts.Writer.WriteHeader(); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}

	var count int64
	for merged.Next() {
		obj := merged.Object()
		if err := opts.Writer.WriteObject(obj); err != nil {
			return fmt.Errorf("writing object: %w", err)
		}
		count++
		if opts.Progress != nil && count%100000 == 0 {
			fmt.Fprintf(opts.Progress, "  %d objects written\n", count)
		}
	}
	if err := merged.Err(); err != nil {
		return fmt.Errorf("scanning: %w", err)
	}

	if err := opts.Writer.Close(); err != nil {
		return fmt.Errorf("closing writer: %w", err)
	}

	if opts.Progress != nil {
		fmt.Fprintf(opts.Progress, "  done: %d objects written\n", count)
	}

	return nil
}
