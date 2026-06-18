package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// minimalPBF builds a minimal valid .osm.pbf byte stream with just a header block.
// This produces a valid PBF that the scanner can read (yielding zero objects).
func minimalPBF(t *testing.T) []byte {
	t.Helper()

	// Build a minimal HeaderBlock protobuf (just required_features field 4).
	// Field 4, wire type 2 (length-delimited): tag = (4<<3)|2 = 34
	hbData := []byte{}
	for _, feat := range []string{"OsmSchema-V0.6", "DenseNodes"} {
		hbData = append(hbData, 0x22) // field 4, wire type 2
		hbData = append(hbData, byte(len(feat)))
		hbData = append(hbData, []byte(feat)...)
	}

	// Compress the header block data with zlib.
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(hbData); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	// Build Blob protobuf:
	// field 2 (raw_size, varint): tag = (2<<3)|0 = 16
	// field 3 (zlib_data, bytes): tag = (3<<3)|2 = 26
	rawSize := len(hbData)
	blobData := []byte{0x10} // field 2 tag
	blobData = append(blobData, protowire.AppendVarint(nil, uint64(rawSize))...)
	blobData = append(blobData, 0x1a) // field 3 tag
	blobData = append(blobData, protowire.AppendVarint(nil, uint64(compressed.Len()))...)
	blobData = append(blobData, compressed.Bytes()...)

	// Build BlobHeader protobuf:
	// field 1 (type, string): tag = (1<<3)|2 = 10
	// field 3 (datasize, int32): tag = (3<<3)|0 = 24
	blobType := "OSMHeader"
	bhData := []byte{0x0a} // field 1 tag
	bhData = append(bhData, byte(len(blobType)))
	bhData = append(bhData, []byte(blobType)...)
	bhData = append(bhData, 0x18) // field 3 tag
	bhData = append(bhData, protowire.AppendVarint(nil, uint64(len(blobData)))...)

	// Frame: 4 bytes big-endian BlobHeader length, BlobHeader, Blob
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.BigEndian, int32(len(bhData))); err != nil {
		t.Fatalf("write header length: %v", err)
	}
	buf.Write(bhData)
	buf.Write(blobData)

	return buf.Bytes()
}

// setupTestDataDir creates a temp directory structured for runOverpassStart with
// a pre-existing PBF file for the given region, so no download is attempted.
func setupTestDataDir(t *testing.T, region string) string {
	t.Helper()
	dir := t.TempDir()
	pbfDir := filepath.Join(dir, "pbf")
	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		t.Fatalf("creating pbf dir: %v", err)
	}
	// Write a minimal PBF using the same naming convention as runOverpassStart.
	filename := strings.ReplaceAll(region, "/", "_") + "-latest.osm.pbf"
	pbfPath := filepath.Join(pbfDir, filename)
	if err := os.WriteFile(pbfPath, minimalPBF(t), 0o644); err != nil {
		t.Fatalf("writing test PBF: %v", err)
	}
	return dir
}

func TestCPUProfile_WrittenDuringConversion(t *testing.T) {
	region := "test/region"
	dataDir := setupTestDataDir(t, region)
	profilePath := filepath.Join(dataDir, "cpu.prof")

	var stderr bytes.Buffer
	err := convertRegionsWithProfile([]string{region}, dataDir, profilePath, &stderr)
	if err != nil {
		t.Fatalf("convertRegionsWithProfile returned unexpected error: %v", err)
	}

	// Verify the profile file exists and is non-empty.
	info, err := os.Stat(profilePath)
	if err != nil {
		t.Fatalf("profile file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("profile file is empty")
	}

	// Verify stderr mentions the profile path.
	if !strings.Contains(stderr.String(), "CPU profile written to") {
		t.Errorf("stderr should mention profile path, got: %s", stderr.String())
	}
}

func TestCPUProfile_NotCreatedWhenOmitted(t *testing.T) {
	region := "test/region"
	dataDir := setupTestDataDir(t, region)
	profilePath := filepath.Join(dataDir, "cpu.prof")

	var stderr bytes.Buffer
	_ = convertRegionsWithProfile([]string{region}, dataDir, "", &stderr)

	// Verify no profile file was created.
	if _, err := os.Stat(profilePath); !os.IsNotExist(err) {
		t.Errorf("profile file should not exist when cpuprofile is omitted, but got err=%v", err)
	}
}

func TestCPUProfile_FlushedOnConversionError(t *testing.T) {
	// Set up a data dir with NO valid PBF file so conversion will fail.
	dir := t.TempDir()
	pbfDir := filepath.Join(dir, "pbf")
	if err := os.MkdirAll(pbfDir, 0o755); err != nil {
		t.Fatalf("creating pbf dir: %v", err)
	}
	// Write an invalid (empty) PBF file so download is skipped but conversion fails.
	region := "test/region"
	filename := strings.ReplaceAll(region, "/", "_") + "-latest.osm.pbf"
	pbfPath := filepath.Join(pbfDir, filename)
	if err := os.WriteFile(pbfPath, []byte("not a valid pbf"), 0o644); err != nil {
		t.Fatalf("writing invalid PBF: %v", err)
	}

	profilePath := filepath.Join(dir, "cpu.prof")

	var stderr bytes.Buffer
	err := convertRegionsWithProfile([]string{region}, dir, profilePath, &stderr)
	if err == nil {
		t.Fatal("expected conversion error but got nil")
	}

	// Partial profile should still be written (defer flushes it).
	info, statErr := os.Stat(profilePath)
	if statErr != nil {
		t.Fatalf("profile file not created on error: %v", statErr)
	}
	if info.Size() == 0 {
		t.Error("profile file is empty after conversion error")
	}
}
