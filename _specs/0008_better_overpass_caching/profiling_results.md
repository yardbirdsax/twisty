# Profiling Results: Overpass Conversion Pipeline

## Dataset Description

- **Regions**: 5 US states (Delaware, Rhode Island, Connecticut, New Hampshire, Vermont)
- **Profile date**: 2026-05-03
- **Wall-clock duration**: 722.74s (~12 minutes)
- **CPU samples collected**: 256.71s (35.52% of wall time — typical for I/O-mixed workloads)

## Top 10 Functions by Cumulative CPU Time

| Rank | Function | Cumulative | Cum% |
|------|----------|-----------|------|
| 1 | `osmconv.Convert` | 250.92s | 97.74% |
| 2 | `osmconv.(*XMLWriter).WriteObject` | 241.48s | 94.07% |
| 3 | `fmt.Fprintf` | 231.26s | 90.09% |
| 4 | `bzip2.(*Writer).Write` | 229.46s | 89.38% |
| 5 | `bzip2.(*Writer).encodeBlock` | 224.79s | 87.57% |
| 6 | `bzip2.(*burrowsWheelerTransform).Encode` | 184.21s | 71.76% |
| 7 | `sais.computeSA_byte` | 175.67s | 68.43% |
| 8 | `osmconv.(*XMLWriter).writeNode` | 142.03s | 55.33% |
| 9 | `osmconv.(*XMLWriter).writeWay` | 97.35s | 37.92% |
| 10 | `sais.computeSA_int` | 50.84s | 19.80% |

## Percentage Breakdown by Pipeline Stage

| Stage | Key Functions | CPU Time | % of Total |
|-------|--------------|----------|-----------|
| **BZ2 compression** | `bzip2.(*Writer).encodeBlock`, BWT encode, SAIS, moveToFront, encodePrefix | ~224.79s | **87.6%** |
| **XML serialization** | `fmt.Fprintf`, `fmt.Sprintf`, `formatCoord`, `writeNode`, `writeWay`, `writeTags` (non-bz2 portion) | ~12.0s | **4.7%** |
| **Merge/dedup** | `mergeScanner.Next` | ~9.38s | **3.7%** |
| **PBF decoding** | `PBFScanner.Next`, `readBlock`, `decodePrimitiveBlock`, protobuf unmarshal | ~8.80s | **3.4%** |
| **I/O (syscalls)** | `syscall.rawsyscalln` (file read/write) | ~18.68s | **7.3%** |

> Note: `writeNode` and `writeWay` cumulative times (55% and 38%) are dominated by time
> spent blocked in `bufio.(*Writer).Flush` → `bzip2.(*Writer).Write`, not by XML generation
> itself. The actual XML formatting overhead is small.

## Dominant Bottleneck: BZ2 Compression (87.6%)

BZ2 compression is overwhelmingly the dominant bottleneck, consuming **87.6%** of all CPU
time. The breakdown within bzip2:

- **Burrows-Wheeler Transform (BWT)**: 71.76% — the core of bzip2's compression, implemented
  via suffix array construction (SAIS algorithm). The `computeSA_byte` function alone is 68.43%.
- **Move-to-Front encoding**: 8.99%
- **Prefix/Huffman encoding**: 6.25%

The pipeline is effectively **compression-bound**. XML serialization, PBF decoding, and
merge/dedup are all negligible by comparison.

## Recommended Optimization Strategy for Phase 2

### Primary: Parallelize BZ2 Compression

Since bzip2 is a block-based compressor, it is naturally parallelizable — each block can be
compressed independently. The recommended approach:

1. **Use `pgzip`-style parallel bzip2 compression**: Write a parallel bzip2 writer that
   compresses multiple blocks concurrently using a worker pool (`GOMAXPROCS` workers).
   Libraries like `dsnet/compress/bzip2` already operate on blocks; the parallelization
   wraps block dispatch and ordered reassembly.

2. **Alternative: Switch compression format**: If bzip2 compatibility is not strictly
   required by Overpass API, consider `zstd` or `gzip` which are dramatically faster.
   However, Overpass API expects `.osm.bz2` input, so this may not be viable.

3. **Alternative: Pipe to `pbzip2`**: Use the system `pbzip2` command (parallel bzip2) as
   an external process. This is the simplest approach but adds an external dependency.

### Secondary (diminishing returns)

- **XML serialization**: `fmt.Fprintf` accounts for ~4.7% of CPU. Switching to direct
  `strconv` + `bufio.WriteString` could reclaim some of this, but the payoff is small.
- **PBF decoding and merge** are already fast and not worth optimizing.

### Recommendation

**Implement parallel bzip2 block compression** as the Phase 2 optimization. This targets
87.6% of CPU time and should yield near-linear speedup with core count (e.g., 4-8x on
typical machines). This single optimization should reduce the 12-minute conversion to
under 2 minutes on modern hardware.
