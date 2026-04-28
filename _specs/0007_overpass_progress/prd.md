# Product Requirements Document: Overpass Progress Reporting

## Overview

Twisty's `overpass start` command downloads PBF files from Geofabrik and converts them into bzip2-compressed OSM XML for import into a local Overpass API container. Both operations are long-running (minutes to tens of minutes depending on region size) and currently provide minimal or coarse feedback, leaving the user uncertain whether the process is stuck or progressing normally.

## Problem Statement

When a user runs `twisty overpass start`, two sequential operations dominate wall-clock time: downloading PBF files and converting them to BZ2. Both give the user insufficient feedback about progress.

1. **PBF download has coarse, non-visual progress.** The download step prints a line every 10 MB (e.g., `downloaded 30.0 MB / 250.0 MB (12%)`). These are discrete log lines that scroll, not an in-place progress bar. For large regions this produces dozens of lines of near-identical output, and the user cannot glance at the terminal to see how far along they are.

2. **PBF-to-BZ2 conversion has minimal progress.** The conversion step prints `N objects written` every 100,000 objects. There is no percentage, no time estimate, and no indication of how many objects remain. The user has no way to judge whether the conversion is 10% or 90% complete.

## Users

Motorcycle riders using Twisty who want to run a local Overpass API for offline or faster road-quality scoring. These users run `twisty overpass start` infrequently (when setting up or refreshing regional data) and need clear terminal feedback that the multi-minute startup process is progressing normally.

## Design Principles

### 1. Percentage Over Count

Users want to know "how far along am I" and "how much longer." A percentage bar with elapsed time answers both questions at a glance. Raw object counts do not. All long-running operations should report progress as a percentage when the total is knowable.

### 2. In-Place Rendering

Progress output should overwrite itself in-place using carriage returns, not scroll the terminal with repeated log lines. This matches the existing `termProgressBar` pattern used by tile fetching and scoring. When stderr is not a terminal, fall back to periodic log lines.

### 3. Composable Interfaces

The progress reporting contract for conversion is fundamentally different from tile fetching (bytes/objects vs. cached/fetched tiles). Rather than bloating the existing `quality.ProgressReporter` interface, introduce a new, focused interface for conversion progress. Concrete terminal renderers can implement multiple interfaces when needed.

### 4. Non-Intrusive to Core Logic

The `osmconv` package is a general-purpose PBF-to-XML converter. Progress hooks should be injected via interfaces or `io.Reader` wrappers, not hard-wired into the scanner or writer. The package should remain usable without progress reporting.

## Feature 1: PBF Download Progress Bar

### Behavioral / Functional Goal

When downloading PBF files during `overpass start`, the user sees an in-place percentage progress bar that shows download progress for each file and overall across all files.

### Trigger / Entry Point

User runs `twisty overpass start` and PBF files need to be downloaded (not already cached or up-to-date).

### Core Behavior

For each PBF file being downloaded, render an in-place progress bar on stderr:

```
Downloading us-northeast (2/3): [============>          ] 52% | 128.3 / 246.8 MB | 1m12s
```

When the HTTP response includes a `Content-Length` header (Geofabrik always does), use it to compute percentage. The bar updates in-place using carriage-return overwriting.

When all files are downloaded, print a final summary line:

```
Downloads complete: 3 files, 512.4 MB, 3m45s
```

### Rules and Constraints

- When stderr is not a terminal (`isTerminal` returns false), fall back to printing a line every 10 MB (current behavior) to avoid garbled output in log files or CI.
- The progress bar must not buffer excessively; update at most once per 100ms to avoid terminal flicker.
- If `Content-Length` is absent, fall back to a counter-style display showing bytes downloaded without percentage.
- Per-file progress is shown while downloading each file. Overall file count (e.g., `2/3`) is shown in the label.

### Data Model

No persistent data model. Progress state is transient and held in the progress bar struct.

## Feature 2: PBF-to-BZ2 Conversion Progress Bar

### Behavioral / Functional Goal

During PBF-to-BZ2 conversion, the user sees an in-place percentage progress bar based on bytes read from the input PBF file(s), with per-file and overall indicators.

### Trigger / Entry Point

The conversion step begins after PBF downloads complete during `twisty overpass start`.

### Core Behavior

Use the total size of input PBF files (known from the filesystem) and track bytes read from each file to compute progress as a percentage. Render an in-place progress bar on stderr:

```
Converting us-northeast.pbf (2/3): [=========>             ] 38% | 1,245,000 objects | 2m31s
```

The byte-tracking is done by wrapping each input `io.Reader` (the PBF file) in a counting reader that reports bytes read to the progress interface. This avoids modifying the `PBFScanner` internals.

When all files are converted, print a summary:

```
Conversion complete: 3,412,500 objects written, 8m12s
```

### Rules and Constraints

- Percentage is based on compressed input bytes read vs. total input file size, not object count. This gives a smooth, accurate progress signal since PBF files are read sequentially.
- When multiple PBF files are merged, overall percentage is computed as total bytes read across all files divided by total size of all files.
- The per-file label updates when the conversion moves to reading predominantly from the next file. Since the merge reads interleaved blocks, per-file progress reflects the file currently yielding the most reads.
- When stderr is not a terminal, fall back to printing a line every 100,000 objects (current behavior).
- Object count is displayed alongside the percentage bar as supplementary info.
- Update rate capped at 100ms to prevent flicker.

### Data Model

```json
{
  "files": [
    { "name": "us-northeast.pbf", "size_bytes": 258000000, "bytes_read": 98000000 }
  ],
  "total_objects_written": 1245000,
  "start_time": "2026-04-27T10:00:00Z"
}
```

## Technical Direction

### Progress Interface

Introduce a new `ConvertProgress` interface in the `osmconv` package:

```go
type ConvertProgress interface {
    SetFiles(names []string, sizes []int64)
    BytesRead(fileIndex int, n int64)
    ObjectsWritten(n int)
    Done()
}
```

A `NoopConvertProgress` implementation is provided as the default. The terminal implementation lives in `main.go` alongside the existing `termProgressBar`.

### Counting Reader

Implement a small `countingReader` wrapper (`io.Reader`) that intercepts `Read()` calls and reports byte counts to the `ConvertProgress` interface. This is injected between the file handle and `PBFScanner`, keeping `osmconv` internals unchanged.

```go
type countingReader struct {
    r         io.Reader
    fileIndex int
    progress  ConvertProgress
}
```

### Download Progress

Refactor `downloadPBF()` in `overpass.go` to accept a progress reporter. The existing chunk-read loop already tracks bytes; replace the `fmt.Fprintf` calls with progress interface calls. Reuse the same `ConvertProgress` interface or a simpler `DownloadProgress` interface if the contracts diverge significantly.

### Integration with Existing System

The `osmconv` package gains the `ConvertProgress` interface and `NoopConvertProgress` type. The `ConvertOptions` struct replaces its `Progress io.Writer` field with `Progress ConvertProgress`. The counting reader wraps input files before they are passed to `NewPBFScanner`.

New code lives in:
- `osmconv/progress.go` -- `ConvertProgress` interface and noop implementation
- `osmconv/counting_reader.go` -- `countingReader` wrapper
- `main.go` or `progress.go` -- terminal progress bar implementation for conversion
- `overpass.go` -- wiring progress into download and conversion calls

## Phasing

### Phase 1: Conversion Progress Bar

**Hypothesis:** Wrapping input readers with a counting reader and displaying byte-based percentage progress gives users sufficient visibility into the PBF-to-BZ2 conversion step.

**Scope:**
- Define `ConvertProgress` interface in `osmconv`
- Implement `countingReader`
- Update `ConvertOptions` to use `ConvertProgress` instead of `io.Writer`
- Implement terminal progress bar for conversion in `main.go`
- Wire into `convertPBFsToBZ2()` in `overpass.go`
- Fall back to line-based output when not a terminal

**Success measurement:**
- User sees a percentage progress bar during conversion that advances smoothly
- No regression in conversion correctness (existing tests pass)
- Progress bar renders correctly for single-file and multi-file conversions

**Exit criteria for Phase 2:** Phase 1 is merged and working correctly for at least one real-world region conversion.

### Phase 2: Download Progress Bar

**Hypothesis:** Replacing the line-based download progress with an in-place percentage bar improves the user experience during the download step.

**Scope:**
- Refactor `downloadPBF()` to accept a progress interface
- Implement terminal progress bar for downloads (may share implementation with Phase 1)
- Show per-file label with overall file count
- Fall back to line-based output when not a terminal

**Success measurement:**
- User sees an in-place progress bar during PBF download
- No regression in download reliability or correctness

**Exit criteria:** Phase 2 is merged and working.

## Non-Goals

- Changing the `quality.ProgressReporter` interface used by tile fetching and scoring.
- Adding ETA (estimated time remaining) calculations. Elapsed time is sufficient; ETAs based on byte throughput are unreliable due to variable decompression costs.
- Adding progress to Docker image build (already streams raw Docker output).
- Adding progress to the Overpass import step inside the Docker container (not controlled by Twisty).
- Making the progress bar interactive (e.g., cancel on keypress).

## Success Criteria

1. During `twisty overpass start`, both PBF download and PBF-to-BZ2 conversion display in-place percentage progress bars when stderr is a terminal.
2. Progress bars show per-file context (file name, file N of M) and overall percentage.
3. When stderr is not a terminal, output falls back to periodic log lines (no garbled escape sequences).
4. The `osmconv` package remains usable without progress reporting (noop default).
5. Existing tests continue to pass with no modification to test assertions.

## Future Considerations

- **ETA calculation:** If users request time-remaining estimates, byte throughput averaging could be added to the progress bar. Deferred because decompression cost varies significantly across PBF blocks, making byte-rate extrapolation unreliable.
- **Parallel PBF downloads:** If multiple regions are downloaded concurrently in the future, the progress bar would need to show multiple concurrent downloads. The interface supports this via file indexing but the terminal renderer would need multi-line support.
- **Unified progress framework:** If more commands need progress bars (beyond tile fetching, scoring, and conversion), a shared progress rendering library could be extracted. Currently three separate implementations is acceptable given their different data models.
