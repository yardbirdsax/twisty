# Task 004: File-Based Tile Cache

## Summary

Implement the file-based tile cache that stores raw Overpass JSON responses on disk, keyed by tile coordinates. Supports atomic writes, cache lookup, mtime-based access tracking, and cache management operations (clear, purge by age).

## Dependencies

Task 002 (for `Tile` struct and `TileCacheKey` function).

## Detailed Directions

### 1. Define the TileCache struct

- In `quality/tilefetch.go`, define:
  ```go
  type TileCache struct {
      Dir       string
      Precision int // decimal places for coordinate formatting
  }
  ```

### 2. Implement cache directory initialization

- Write `(c *TileCache) EnsureDir() error` that calls `os.MkdirAll(c.Dir, 0o755)`.
- This is called once at startup, not per-tile.

### 3. Implement cache path computation

- Write `(c *TileCache) Path(t Tile) string` that returns the full file path:
  ```go
  func (c *TileCache) Path(t Tile) string {
      return filepath.Join(c.Dir, TileCacheKey(t, c.Precision))
  }
  ```

### 4. Implement cache existence check

- Write `(c *TileCache) Has(t Tile) bool` that returns `true` if the cache file exists:
  ```go
  func (c *TileCache) Has(t Tile) bool {
      _, err := os.Stat(c.Path(t))
      return err == nil
  }
  ```

### 5. Implement cache read with mtime update

- Write `(c *TileCache) Read(t Tile) ([]byte, error)`:
  1. Read the file with `os.ReadFile`.
  2. On success, update the file's mtime to `time.Now()` using `os.Chtimes`.
  3. Return the raw bytes.

### 6. Implement atomic cache write

- Write `(c *TileCache) Write(t Tile, data []byte) error`:
  1. Write to a temporary file in the same directory (use `os.CreateTemp(c.Dir, "tile-*.tmp")`).
  2. Write `data` to the temp file and close it.
  3. Rename the temp file to the final cache path using `os.Rename`.
  4. If any step fails, clean up the temp file.
  ```go
  func (c *TileCache) Write(t Tile, data []byte) error {
      target := c.Path(t)
      tmp, err := os.CreateTemp(c.Dir, "tile-*.tmp")
      if err != nil {
          return fmt.Errorf("create temp file: %w", err)
      }
      tmpName := tmp.Name()
      defer os.Remove(tmpName) // cleanup on failure

      if _, err := tmp.Write(data); err != nil {
          tmp.Close()
          return fmt.Errorf("write temp file: %w", err)
      }
      if err := tmp.Close(); err != nil {
          return fmt.Errorf("close temp file: %w", err)
      }
      return os.Rename(tmpName, target)
  }
  ```

### 7. Implement `ClearAll`

- Write `(c *TileCache) ClearAll() error` that removes and recreates the cache directory:
  ```go
  func (c *TileCache) ClearAll() error {
      if err := os.RemoveAll(c.Dir); err != nil {
          return err
      }
      return os.MkdirAll(c.Dir, 0o755)
  }
  ```

### 8. Implement `PurgeOlderThan`

- Write `(c *TileCache) PurgeOlderThan(age time.Duration) (int, error)`:
  1. Read directory entries with `os.ReadDir`.
  2. For each `.json` file, stat it and check if `time.Since(info.ModTime()) > age`.
  3. If older, delete the file and increment a counter.
  4. Return the count of purged files.

### 9. Write unit tests

- In `quality/tilefetch_test.go`, write tests using `t.TempDir()` for isolated cache directories:
  - `TestTileCacheWriteAndRead`: Write data, read it back, verify contents match.
  - `TestTileCacheHas`: Verify `Has` returns false before write, true after.
  - `TestTileCacheReadUpdatesMtime`: Write a file, set its mtime to the past, read it, verify mtime is updated to approximately now.
  - `TestTileCacheAtomicWrite`: Verify that a partial write (simulated by checking no `.tmp` files remain) doesn't leave corrupt cache files.
  - `TestTileCacheClearAll`: Write some files, clear, verify directory is empty but exists.
  - `TestTileCachePurgeOlderThan`: Write files with different mtimes (set via `os.Chtimes`), purge, verify only old files are removed.
  - `TestTileCacheReadNonExistent`: Verify appropriate error on missing file.

## Acceptance Criteria

- [ ] `TileCache` struct with Dir and Precision fields
- [ ] Atomic write using temp-file-and-rename pattern
- [ ] Cache read updates file mtime for access tracking
- [ ] `ClearAll` removes and recreates cache directory
- [ ] `PurgeOlderThan` removes only files older than the threshold
- [ ] All unit tests pass using `t.TempDir()` for isolation
- [ ] No external dependencies (pure standard library)

## Notes

- The cache stores raw JSON bytes, not parsed objects. Parsing happens in the merge step (Task 006).
- The atomic write pattern is important for crash safety — if the process dies mid-write, no corrupt file is left behind.
- `t.TempDir()` automatically cleans up after the test, making tests hermetic.
- The precision field should default to 3 for the standard 0.05° tile size.
