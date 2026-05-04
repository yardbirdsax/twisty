package osmconv

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"

	"google.golang.org/protobuf/proto"

	pb "github.com/yardbirdsax/twisty/osmconv/internal/osmpbf"
)

// BuildMinimalPBFBytes returns a minimal, valid PBF byte stream containing two
// nodes. It is intended for use in tests in external packages that need a real
// PBF without access to the internal protobuf types.
func BuildMinimalPBFBytes() []byte {
	granularity := int32(100)
	latOffset := int64(0)
	lonOffset := int64(0)

	block := &pb.PrimitiveBlock{
		Stringtable: &pb.StringTable{S: [][]byte{[]byte("")}},
		Primitivegroup: []*pb.PrimitiveGroup{
			{
				Dense: &pb.DenseNodes{
					Id:  []int64{1, 1},        // delta IDs: node 1, node 2
					Lat: []int64{515074000, 0}, // 51.5074° both
					Lon: []int64{-1278000, 0},  // -0.1278° both
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

	var buf bytes.Buffer
	writePBFBlock(&buf, "OSMHeader", mustMarshalMsg(header))
	writePBFBlock(&buf, "OSMData", mustMarshalMsg(block))
	return buf.Bytes()
}

func writePBFBlock(buf *bytes.Buffer, blobType string, data []byte) {
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, _ = zw.Write(data)
	_ = zw.Close()

	rawSize := int32(len(data))
	blob := &pb.Blob{
		RawSize: &rawSize,
		Data:    &pb.Blob_ZlibData{ZlibData: compressed.Bytes()},
	}
	blobBytes := mustMarshalMsg(blob)

	dataSize := int32(len(blobBytes))
	blobHeader := &pb.BlobHeader{
		Type:     &blobType,
		Datasize: &dataSize,
	}
	headerBytes := mustMarshalMsg(blobHeader)

	_ = binary.Write(buf, binary.BigEndian, int32(len(headerBytes)))
	buf.Write(headerBytes)
	buf.Write(blobBytes)
}

func mustMarshalMsg(m proto.Message) []byte {
	b, err := proto.Marshal(m)
	if err != nil {
		panic("osmconv: proto.Marshal: " + err.Error())
	}
	return b
}
