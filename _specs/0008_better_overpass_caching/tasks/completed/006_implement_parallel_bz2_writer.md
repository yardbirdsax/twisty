# Task 006: Implement Parallel BZ2 Block Compression Writer

## Summary

Implement a parallel BZ2 writer that compresses multiple blocks concurrently using a goroutine worker pool. This targets the dominant bottleneck: BZ2 compression accounts for 87.6% of CPU time (profiling_results.md), and the Burrows-Wheeler Transform within each block is independently compressible.

## Dependencies

Task 003

## Detailed Directions

### 1. Understand BZ2 Block Structure

BZ2 compresses data in independent blocks (default 900KB uncompressed). Each block is:
1. Run-length encoded
2. Burrows-Wheeler transformed
3. Move-to-front encoded
4. Huffman encoded

Blocks are independent — block N's compression does not depend on block N-1. This makes BZ2 naturally parallelizable.

### 2. Implement ParallelBZ2Writer

Create `osmconv/parallelbz2.go` implementing `io.WriteCloser`:

```go
type ParallelBZ2Writer struct { ... }

func NewParallelBZ2Writer(w io.Writer, workers int) (*ParallelBZ2Writer, error)
```

Design:
- Buffer incoming `Write()` calls into block-sized chunks (900KB by default, matching the standard BZ2 block size).
- When a block is full, dispatch it to a worker goroutine for compression.
- Workers compress blocks independently using the `dsnet/compress/bzip2` package (or the standard library's `compress/bzip2` writer on a per-block basis).
- A reassembly goroutine writes compressed blocks to the underlying writer **in input order** (not completion order).
- Use a bounded channel or semaphore to limit in-flight blocks (prevent unbounded memory growth).
- `Close()` flushes the final partial block and waits for all workers to finish.

### 3. Ordered Reassembly Strategy

Use a sequenced channel approach:
1. Each dispatched block gets a sequence number.
2. Compressed results are sent to a result channel tagged with their sequence number.
3. The reassembly goroutine pulls results and writes them in sequence order, buffering out-of-order completions.

Alternatively, use a simpler ring buffer of futures (one slot per in-flight block, read sequentially).

### 4. BZ2 Stream Format Compliance

A valid BZ2 stream is:
- Stream header: magic bytes `BZ` + version `h` + block size digit (`9` for 900KB)
- One or more compressed blocks, each starting with the block magic `0x314159265359`
- Stream footer with a CRC

**Important**: The parallel writer must produce a valid BZ2 stream. The simplest approach is to write each block as an independent BZ2 stream (BZ2 supports concatenated streams — `bzip2 -d` and most libraries handle this transparently). This avoids needing to reassemble raw block bitstreams.

### 5. Write Tests

- **Correctness test**: Compress data with `ParallelBZ2Writer`, decompress with standard `bzip2.NewReader`, verify decompressed output matches input byte-for-byte.
- **Large data test**: Compress data spanning multiple blocks (e.g., 5MB of XML-like text), verify round-trip correctness.
- **Single worker test**: With `workers=1`, behavior matches serial BZ2 writer.
- **Concurrent correctness**: With `workers=8`, output is still correctly ordered and decompresses to the original input.
- **Close semantics**: Verify `Close()` flushes partial blocks and blocks until all workers complete.

### 6. Benchmark

Add a benchmark comparing `ParallelBZ2Writer` to serial `bzip2.NewWriter` on a ~10MB input:
- `BenchmarkSerialBZ2` — single-threaded baseline
- `BenchmarkParallelBZ2_1` — 1 worker (overhead check)
- `BenchmarkParallelBZ2_4` — 4 workers
- `BenchmarkParallelBZ2_N` — `runtime.GOMAXPROCS(0)` workers

## Acceptance Criteria

- [ ] `ParallelBZ2Writer` implements `io.WriteCloser`
- [ ] Compressed output is a valid BZ2 stream (decompresses correctly with standard tools)
- [ ] Output is byte-for-byte identical when decompressed, regardless of worker count
- [ ] Blocks are written in input order (not completion order)
- [ ] Memory usage is bounded (limited in-flight blocks)
- [ ] Benchmark shows meaningful speedup with multiple workers vs. serial BZ2
- [ ] Tests pass

## Notes

- The concatenated-streams approach (each block is an independent BZ2 stream) is strongly recommended over raw block reassembly. It avoids bit-level manipulation of the BZ2 format and leverages existing BZ2 writer implementations.
- The `dsnet/compress/bzip2` package provides a pure-Go BZ2 writer that can be used per-block. The standard library's `compress/bzip2` only provides a reader.
- Worker count should default to `runtime.GOMAXPROCS(0)` in production, but be configurable in the constructor for testing.
- Block size should match the `dsnet/compress/bzip2` default (900KB) to avoid compatibility surprises.

---

# Task 006 Review: Implement Parallel BZ2 Block Compression Writer

**Reviewer:** Claude (principal-engineer)
**Date:** 2026-05-03
**Verdict:** APPROVED

---

## Summary

All previously identified issues have been resolved. The goroutine leak in `Close()` is fixed — teardown is now unconditional regardless of whether `dispatchBlock` fails.

### Files Reviewed

| File | Status |
|------|--------|
| `osmconv/parallelbz2.go` | Reviewed |
| `osmconv/parallelbz2_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `ParallelBZ2Writer` implements `io.WriteCloser` | PASS |
| Compressed output is a valid BZ2 stream | PASS |
| Output is byte-for-byte identical when decompressed, regardless of worker count | PASS |
| Blocks are written in input order (not completion order) | PASS |
| Memory usage is bounded (limited in-flight blocks) | PASS |
| Benchmark shows meaningful speedup with multiple workers vs. serial BZ2 | PASS |
| Tests pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go test ./osmconv/... -v -run TestParallel -timeout 60s  # all 6 tests PASS
make lint  # clean
make test  # all packages PASS
```

---

## Final Verdict

**APPROVED**

The goroutine leak fix is correct: `Close()` now discards the error from `dispatchBlock` with `_ =` (the error is captured internally via `setErr`) and falls through unconditionally to `close(p.workCh)` / `p.wg.Wait()` / `close(p.resultCh)`. Workers always observe the channel close and exit cleanly. All acceptance criteria met, tests pass, linter clean.
