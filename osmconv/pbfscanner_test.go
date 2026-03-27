package osmconv

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"testing"

	"google.golang.org/protobuf/proto"

	pb "github.com/yardbirdsax/twisty/osmconv/internal/osmpbf"
)

// buildPBF builds a minimal .osm.pbf byte stream from a HeaderBlock and PrimitiveBlocks.
func buildPBF(t *testing.T, header *pb.HeaderBlock, blocks ...*pb.PrimitiveBlock) []byte {
	t.Helper()
	var buf bytes.Buffer

	// Write header block
	writeBlock(t, &buf, "OSMHeader", mustMarshal(t, header))

	// Write data blocks
	for _, block := range blocks {
		writeBlock(t, &buf, "OSMData", mustMarshal(t, block))
	}

	return buf.Bytes()
}

func writeBlock(t *testing.T, buf *bytes.Buffer, blobType string, data []byte) {
	t.Helper()

	// Compress data with zlib
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(data); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	rawSize := int32(len(data))
	blob := &pb.Blob{
		RawSize: &rawSize,
		Data:    &pb.Blob_ZlibData{ZlibData: compressed.Bytes()},
	}
	blobBytes := mustMarshal(t, blob)

	dataSize := int32(len(blobBytes))
	blobHeader := &pb.BlobHeader{
		Type:     &blobType,
		Datasize: &dataSize,
	}
	headerBytes := mustMarshal(t, blobHeader)

	// Write: 4-byte header length (big-endian), header, blob
	if err := binary.Write(buf, binary.BigEndian, int32(len(headerBytes))); err != nil {
		t.Fatalf("write header length: %v", err)
	}
	buf.Write(headerBytes)
	buf.Write(blobBytes)
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	return b
}

func TestPBFScanner_DenseNodes(t *testing.T) {
	granularity := int32(100)
	latOffset := int64(0)
	lonOffset := int64(0)

	// DenseNodes: two nodes with delta-coded IDs, lats, lons
	// Node 1: id=10, lat=51.5074° => 51507400000 nanodeg => raw = 51507400000/100 = 515074000
	// Node 2: id=20, lat=48.8566° => 48856600000 nanodeg => raw = 488566000
	// Delta lats: [515074000, 488566000-515074000] = [515074000, -26508000]
	// Node 1 lon: -0.1278° => -127800000 nanodeg => raw = -1278000
	// Node 2 lon: 0.1068° => 106800000 nanodeg => raw = 1068000
	// Delta lons: [-1278000, 1068000-(-1278000)] = [-1278000, 2346000]
	block := &pb.PrimitiveBlock{
		Stringtable: &pb.StringTable{
			S: [][]byte{[]byte("")}, // index 0 is always empty
		},
		Primitivegroup: []*pb.PrimitiveGroup{
			{
				Dense: &pb.DenseNodes{
					Id:  []int64{10, 10},                  // delta: 10, 20
					Lat: []int64{515074000, -26508000},    // delta: 51.5074, 48.8566
					Lon: []int64{-1278000, 2346000},       // delta: -0.1278, 0.1068
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

	data := buildPBF(t, header, block)
	scanner := NewPBFScanner(bytes.NewReader(data))

	var objects []Object
	for scanner.Next() {
		objects = append(objects, scanner.Object())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	if len(objects) != 2 {
		t.Fatalf("got %d objects, want 2", len(objects))
	}

	// Node 1
	assertObject(t, objects[0], NodeType, 10)
	if objects[0].Node == nil {
		t.Fatal("node 1: Node is nil")
	}
	assertCoord(t, "node1.Lat", objects[0].Node.Lat, 51.5074)
	assertCoord(t, "node1.Lon", objects[0].Node.Lon, -0.1278)

	// Node 2
	assertObject(t, objects[1], NodeType, 20)
	if objects[1].Node == nil {
		t.Fatal("node 2: Node is nil")
	}
	assertCoord(t, "node2.Lat", objects[1].Node.Lat, 48.8566)
	assertCoord(t, "node2.Lon", objects[1].Node.Lon, 0.1068)
}

func TestPBFScanner_DenseNodesWithTags(t *testing.T) {
	granularity := int32(100)
	latOffset := int64(0)
	lonOffset := int64(0)

	block := &pb.PrimitiveBlock{
		Stringtable: &pb.StringTable{
			S: [][]byte{
				[]byte(""),        // 0: delimiter
				[]byte("name"),    // 1
				[]byte("London"),  // 2
				[]byte("place"),   // 3
				[]byte("city"),    // 4
			},
		},
		Primitivegroup: []*pb.PrimitiveGroup{
			{
				Dense: &pb.DenseNodes{
					Id:  []int64{1, 1},        // IDs: 1, 2
					Lat: []int64{5150740, 0},
					Lon: []int64{-12780, 0},
					// keys_vals: node1 has name=London, place=city; node2 has no tags
					// Format: (key val)* 0 per node
					KeysVals: []int32{1, 2, 3, 4, 0, 0},
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

	data := buildPBF(t, header, block)
	scanner := NewPBFScanner(bytes.NewReader(data))

	var objects []Object
	for scanner.Next() {
		objects = append(objects, scanner.Object())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	if len(objects) != 2 {
		t.Fatalf("got %d objects, want 2", len(objects))
	}

	// Node 1 should have tags
	if objects[0].Node.Tags["name"] != "London" {
		t.Errorf("node1 name = %q, want London", objects[0].Node.Tags["name"])
	}
	if objects[0].Node.Tags["place"] != "city" {
		t.Errorf("node1 place = %q, want city", objects[0].Node.Tags["place"])
	}

	// Node 2 should have no tags
	if len(objects[1].Node.Tags) != 0 {
		t.Errorf("node2 should have no tags, got %v", objects[1].Node.Tags)
	}
}

func TestPBFScanner_Ways(t *testing.T) {
	granularity := int32(100)
	latOffset := int64(0)
	lonOffset := int64(0)

	id1 := int64(100)
	id2 := int64(200)

	block := &pb.PrimitiveBlock{
		Stringtable: &pb.StringTable{
			S: [][]byte{
				[]byte(""),          // 0
				[]byte("highway"),   // 1
				[]byte("primary"),   // 2
			},
		},
		Primitivegroup: []*pb.PrimitiveGroup{
			{
				Ways: []*pb.Way{
					{
						Id:   &id1,
						Keys: []uint32{1},
						Vals: []uint32{2},
						Refs: []int64{10, 10, 10}, // delta: 10, 20, 30
					},
					{
						Id:   &id2,
						Refs: []int64{5, 5}, // delta: 5, 10
					},
				},
			},
		},
		Granularity: &granularity,
		LatOffset:   &latOffset,
		LonOffset:   &lonOffset,
	}

	header := &pb.HeaderBlock{
		RequiredFeatures: []string{"OsmSchema-V0.6"},
	}

	data := buildPBF(t, header, block)
	scanner := NewPBFScanner(bytes.NewReader(data))

	var objects []Object
	for scanner.Next() {
		objects = append(objects, scanner.Object())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	if len(objects) != 2 {
		t.Fatalf("got %d objects, want 2", len(objects))
	}

	// Way 100
	assertObject(t, objects[0], WayType, 100)
	way := objects[0].Way
	if way == nil {
		t.Fatal("way 100: Way is nil")
	}
	wantRefs := []int64{10, 20, 30}
	if len(way.NodeIDs) != len(wantRefs) {
		t.Fatalf("way100 refs: got %v, want %v", way.NodeIDs, wantRefs)
	}
	for i, ref := range wantRefs {
		if way.NodeIDs[i] != ref {
			t.Errorf("way100.NodeIDs[%d] = %d, want %d", i, way.NodeIDs[i], ref)
		}
	}
	if way.Tags["highway"] != "primary" {
		t.Errorf("way100 highway = %q, want primary", way.Tags["highway"])
	}

	// Way 200 (no tags)
	assertObject(t, objects[1], WayType, 200)
	if len(objects[1].Way.Tags) != 0 {
		t.Errorf("way200 should have no tags, got %v", objects[1].Way.Tags)
	}
	wantRefs2 := []int64{5, 10}
	if len(objects[1].Way.NodeIDs) != len(wantRefs2) {
		t.Fatalf("way200 refs: got %v, want %v", objects[1].Way.NodeIDs, wantRefs2)
	}
}

func TestPBFScanner_Relations(t *testing.T) {
	granularity := int32(100)
	latOffset := int64(0)
	lonOffset := int64(0)

	relID := int64(500)

	block := &pb.PrimitiveBlock{
		Stringtable: &pb.StringTable{
			S: [][]byte{
				[]byte(""),              // 0
				[]byte("type"),          // 1
				[]byte("multipolygon"),  // 2
				[]byte("outer"),         // 3
				[]byte("inner"),         // 4
			},
		},
		Primitivegroup: []*pb.PrimitiveGroup{
			{
				Relations: []*pb.Relation{
					{
						Id:       &relID,
						Keys:     []uint32{1},
						Vals:     []uint32{2},
						RolesSid: []int32{3, 4},
						Memids:   []int64{100, 1}, // delta: 100, 101
						Types: []pb.Relation_MemberType{
							pb.Relation_WAY,
							pb.Relation_WAY,
						},
					},
				},
			},
		},
		Granularity: &granularity,
		LatOffset:   &latOffset,
		LonOffset:   &lonOffset,
	}

	header := &pb.HeaderBlock{
		RequiredFeatures: []string{"OsmSchema-V0.6"},
	}

	data := buildPBF(t, header, block)
	scanner := NewPBFScanner(bytes.NewReader(data))

	var objects []Object
	for scanner.Next() {
		objects = append(objects, scanner.Object())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	if len(objects) != 1 {
		t.Fatalf("got %d objects, want 1", len(objects))
	}

	assertObject(t, objects[0], RelationType, 500)
	rel := objects[0].Rel
	if rel == nil {
		t.Fatal("relation: Rel is nil")
	}
	if rel.Tags["type"] != "multipolygon" {
		t.Errorf("rel type = %q, want multipolygon", rel.Tags["type"])
	}
	if len(rel.Members) != 2 {
		t.Fatalf("got %d members, want 2", len(rel.Members))
	}
	if rel.Members[0].Type != WayType || rel.Members[0].ID != 100 || rel.Members[0].Role != "outer" {
		t.Errorf("member[0] = %+v, want {Way, 100, outer}", rel.Members[0])
	}
	if rel.Members[1].Type != WayType || rel.Members[1].ID != 101 || rel.Members[1].Role != "inner" {
		t.Errorf("member[1] = %+v, want {Way, 101, inner}", rel.Members[1])
	}
}

func TestPBFScanner_EmptyFile(t *testing.T) {
	header := &pb.HeaderBlock{
		RequiredFeatures: []string{"OsmSchema-V0.6"},
	}
	data := buildPBF(t, header) // no data blocks

	scanner := NewPBFScanner(bytes.NewReader(data))

	if scanner.Next() {
		t.Error("expected no objects from empty PBF")
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertCoord(t *testing.T, name string, got, want float64) {
	t.Helper()
	const epsilon = 1e-6
	if diff := got - want; diff > epsilon || diff < -epsilon {
		t.Errorf("%s = %f, want %f (diff=%e)", name, got, want, diff)
	}
}
