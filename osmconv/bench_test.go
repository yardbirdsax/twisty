package osmconv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dsnet/compress/bzip2"
)

// BenchmarkConvert measures end-to-end conversion of a real PBF file.
//
// Set BENCHMARK_PBF to the path of the input PBF file before running.
// Set BENCHMARK_OUTPUT to override the output path (default: $TMPDIR/benchmark-output.osm.bz2).
//
//	BENCHMARK_PBF=benchmark-data/north-america_us_delaware-latest.osm.pbf \
//	  go test -bench=BenchmarkConvert -benchtime=1x -v ./osmconv/
func BenchmarkConvert(b *testing.B) {
	pbfPath := os.Getenv("BENCHMARK_PBF")
	if pbfPath == "" {
		b.Skip("BENCHMARK_PBF not set")
	}

	outPath := os.Getenv("BENCHMARK_OUTPUT")
	if outPath == "" {
		outPath = filepath.Join(os.TempDir(), "benchmark-output.osm.bz2")
	}

	for b.Loop() {
		runConvert(b, pbfPath, outPath)
	}
}

func runConvert(b *testing.B, pbfPath, outPath string) {
	b.Helper()

	pbfFile, err := os.Open(pbfPath)
	if err != nil {
		b.Fatalf("opening PBF: %v", err)
	}
	defer pbfFile.Close()

	outFile, err := os.Create(outPath)
	if err != nil {
		b.Fatalf("creating output file: %v", err)
	}

	bz2w, err := bzip2.NewWriter(outFile, nil)
	if err != nil {
		outFile.Close()
		b.Fatalf("creating bzip2 writer: %v", err)
	}

	scanner := NewPBFScanner(pbfFile)
	writer := NewXMLWriter(bz2w)

	if err := Convert(ConvertOptions{
		Scanners: []ObjectScanner{scanner},
		Writer:   writer,
	}); err != nil {
		bz2w.Close()
		outFile.Close()
		b.Fatalf("Convert: %v", err)
	}

	if err := bz2w.Close(); err != nil {
		outFile.Close()
		b.Fatalf("closing bzip2 writer: %v", err)
	}
	if err := outFile.Close(); err != nil {
		b.Fatalf("closing output file: %v", err)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		b.Fatalf("stat output: %v", err)
	}
	b.SetBytes(info.Size())
}
