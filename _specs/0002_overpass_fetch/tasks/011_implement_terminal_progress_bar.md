---
# Task 011: Implement Terminal Progress Bar and Wire into CLI

## Summary

Implement a terminal progress bar renderer that satisfies `quality.ProgressReporter` and wire it into the `fetch` subcommand. The bar renders to stderr using carriage-return overwriting, shows tile counts and a cache-vs-fetch breakdown, and suppresses itself when `-v` is active or when stderr is not a terminal.

## Dependencies

Task 009 and Task 010 must be complete.

## Detailed Directions

### 1. Implement the progress bar in main.go

Add a `termProgressBar` struct to `main.go` (no new file needed — keep it co-located with the CLI code that uses it):

```go
// termProgressBar renders a progress bar to w using carriage-return overwriting.
// It implements quality.ProgressReporter.
type termProgressBar struct {
    w       io.Writer
    total   int
    current int
    cached  int
    fetched int
}

func (b *termProgressBar) SetTotal(n int) {
    b.total = n
    b.render()
}

func (b *termProgressBar) Tick(cached bool) {
    b.current++
    if cached {
        b.cached++
    } else {
        b.fetched++
    }
    b.render()
}

func (b *termProgressBar) Done() {
    b.render()
    fmt.Fprintln(b.w)
}

func (b *termProgressBar) render() {
    const width = 30
    filled := 0
    if b.total > 0 {
        filled = width * b.current / b.total
    }
    bar := strings.Repeat("=", filled) + strings.Repeat(" ", width-filled)
    fmt.Fprintf(b.w, "\rFetching tiles: [%s] %d/%d (%d cached, %d fetched)",
        bar, b.current, b.total, b.cached, b.fetched)
}
```

The `strings` package is already imported in `main.go`; add `io` if not already present.

### 2. Add a helper to detect whether stderr is a terminal

Add a small helper to `main.go`:

```go
// isTerminal reports whether the given file is connected to a terminal.
func isTerminal(f *os.File) bool {
    fi, err := f.Stat()
    if err != nil {
        return false
    }
    return fi.Mode()&os.ModeCharDevice != 0
}
```

### 3. Wire into runFetch

In `runFetch`, after the logger is set up and before `quality.FetchTiledWays` is called, construct the appropriate `ProgressReporter`:

```go
var progress quality.ProgressReporter = quality.NoopProgressReporter{}
if !*verbose && isTerminal(os.Stderr) {
    progress = &termProgressBar{w: os.Stderr}
}
```

Then pass it into the config:

```go
cfg := quality.TileFetchConfig{
    TileSize: *tileSize,
    Cache:    cache,
    NoCache:  *noCache,
    Logger:   logger,
    Progress: progress,
}
```

(`*verbose` here is whatever the existing boolean flag variable is named in `runFetch` — check the actual variable name and use it.)

## Acceptance Criteria

- [ ] Running `twisty fetch --address "..." ` without `-v` displays a progress bar on stderr that updates per tile.
- [ ] The bar format is `Fetching tiles: [======>         ] N/TOTAL (C cached, F fetched)`.
- [ ] After all tiles are processed, the bar is followed by a newline so the shell prompt appears on a new line.
- [ ] Running with `-v` shows no progress bar (verbose log lines appear instead).
- [ ] When stderr is piped (not a terminal), no progress bar is rendered and no `\r` characters appear in the output.
- [ ] `go build ./...` and `go test ./...` pass with no errors.

## Notes

- `termProgressBar` must not be exported — it is an implementation detail of the CLI layer.
- The `\r` trick only works correctly on terminals; piped output would show garbled lines, which is why the `isTerminal` guard is important.
- The bar width of 30 characters is a reasonable fixed default. Detecting the actual terminal width (`ioctl TIOCGWINSZ`) would require `syscall` and is out of scope.
- No third-party libraries should be added for this feature.

---
