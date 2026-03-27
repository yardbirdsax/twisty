package osmconv

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"

	pb "github.com/yardbirdsax/twisty/osmconv/internal/osmpbf"
)

func TestConvert_SinglePBF(t *testing.T) {
	granularity := int32(100)
	latOffset := int64(0)
	lonOffset := int64(0)

	wayID := int64(100)

	block := &pb.PrimitiveBlock{
		Stringtable: &pb.StringTable{
			S: [][]byte{
				[]byte(""),
				[]byte("highway"),
				[]byte("primary"),
			},
		},
		Primitivegroup: []*pb.PrimitiveGroup{
			{
				Dense: &pb.DenseNodes{
					Id:  []int64{1, 1},
					Lat: []int64{515074000, 0},
					Lon: []int64{-1278000, 0},
				},
			},
			{
				Ways: []*pb.Way{
					{
						Id:   &wayID,
						Keys: []uint32{1},
						Vals: []uint32{2},
						Refs: []int64{1, 1}, // delta: 1, 2
					},
				},
			},
		},
		Granularity: &granularity,
		LatOffset:   &latOffset,
		LonOffset:   &lonOffset,
	}

	header := &pb.HeaderBlock{
		RequiredFeatures: []string{"OsmSchema-V0.6", "DenseNodes"},
	}

	pbfData := buildPBF(t, header, block)

	scanner := NewPBFScanner(bytes.NewReader(pbfData))
	var out bytes.Buffer
	writer := NewXMLWriter(&out)

	err := Convert(ConvertOptions{
		Scanners: []ObjectScanner{scanner},
		Writer:   writer,
	})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	result := out.String()

	// Verify well-formed XML
	if err := xml.Unmarshal([]byte(result), new(any)); err != nil {
		t.Errorf("output is not well-formed XML: %v\n%s", err, result)
	}

	// Should contain nodes before ways (sorted order)
	nodeIdx := strings.Index(result, `<node id="1"`)
	wayIdx := strings.Index(result, `<way id="100"`)
	if nodeIdx < 0 {
		t.Error("missing node in output")
	}
	if wayIdx < 0 {
		t.Error("missing way in output")
	}
	if nodeIdx >= wayIdx {
		t.Error("nodes should appear before ways in output")
	}
}

func TestConvert_MultiplePBFs_Merged(t *testing.T) {
	granularity := int32(100)
	latOffset := int64(0)
	lonOffset := int64(0)

	// PBF 1: node 1, way 10
	wayID1 := int64(10)
	block1 := &pb.PrimitiveBlock{
		Stringtable: &pb.StringTable{S: [][]byte{[]byte("")}},
		Primitivegroup: []*pb.PrimitiveGroup{
			{Dense: &pb.DenseNodes{
				Id: []int64{1}, Lat: []int64{515074000}, Lon: []int64{-1278000},
			}},
			{Ways: []*pb.Way{{Id: &wayID1, Refs: []int64{1}}}},
		},
		Granularity: &granularity, LatOffset: &latOffset, LonOffset: &lonOffset,
	}

	// PBF 2: node 2 (shared border node 1 as duplicate), way 5
	wayID2 := int64(5)
	block2 := &pb.PrimitiveBlock{
		Stringtable: &pb.StringTable{S: [][]byte{[]byte("")}},
		Primitivegroup: []*pb.PrimitiveGroup{
			{Dense: &pb.DenseNodes{
				Id: []int64{1, 1}, Lat: []int64{515074000, 0}, Lon: []int64{-1278000, 0},
			}},
			{Ways: []*pb.Way{{Id: &wayID2, Refs: []int64{2}}}},
		},
		Granularity: &granularity, LatOffset: &latOffset, LonOffset: &lonOffset,
	}

	header := &pb.HeaderBlock{RequiredFeatures: []string{"OsmSchema-V0.6", "DenseNodes"}}

	pbf1 := buildPBF(t, header, block1)
	pbf2 := buildPBF(t, header, block2)

	s1 := NewPBFScanner(bytes.NewReader(pbf1))
	s2 := NewPBFScanner(bytes.NewReader(pbf2))

	var out bytes.Buffer
	writer := NewXMLWriter(&out)

	err := Convert(ConvertOptions{
		Scanners: []ObjectScanner{s1, s2},
		Writer:   writer,
	})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	result := out.String()

	// Node 1 should appear only once (deduplicated)
	if count := strings.Count(result, `<node id="1"`); count != 1 {
		t.Errorf("node 1 appears %d times, want 1 (dedup)", count)
	}

	// Node 2 should appear
	if !strings.Contains(result, `<node id="2"`) {
		t.Error("missing node 2")
	}

	// Both ways should appear, sorted: way 5 before way 10
	way5Idx := strings.Index(result, `<way id="5"`)
	way10Idx := strings.Index(result, `<way id="10"`)
	if way5Idx < 0 || way10Idx < 0 {
		t.Error("missing ways")
	}
	if way5Idx >= way10Idx {
		t.Error("way 5 should appear before way 10 (sorted)")
	}
}

func TestConvert_NoScanners(t *testing.T) {
	var out bytes.Buffer
	writer := NewXMLWriter(&out)

	err := Convert(ConvertOptions{
		Scanners: nil,
		Writer:   writer,
	})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	result := out.String()
	if !strings.Contains(result, "<osm") {
		t.Error("missing osm element")
	}
	if !strings.Contains(result, "</osm>") {
		t.Error("missing closing osm element")
	}
}
