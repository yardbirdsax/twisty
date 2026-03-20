package quality

// ProgressReporter is implemented by types that want to receive progress
// events during a tiled fetch. All methods must be safe to call on a nil
// receiver or a no-op implementation.
type ProgressReporter interface {
	// SetTotal is called once, before the fetch loop begins, with the total
	// number of tiles to process.
	SetTotal(n int)

	// Tick is called once per tile after it has been processed (whether from
	// cache or from a live fetch). cached is true when the tile was served
	// from the local cache.
	Tick(cached bool)

	// Done is called after all tiles have been processed. Implementations
	// should use this to finalize any output (e.g., print a trailing newline).
	Done()
}

var _ ProgressReporter = NoopProgressReporter{}

// NoopProgressReporter is a ProgressReporter that discards all events.
// It is the safe default when no progress output is desired.
type NoopProgressReporter struct{}

func (NoopProgressReporter) SetTotal(_ int) {}
func (NoopProgressReporter) Tick(_ bool)    {}
func (NoopProgressReporter) Done()          {}
