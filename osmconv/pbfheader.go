package osmconv

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/yardbirdsax/twisty/osmconv/internal/osmpbf"
)

// ReadPBFHeaderTimestamp reads the OSMHeader block of a PBF stream and returns
// its osmosis_replication_timestamp (the true age of the data in the file).
// ok is false when the header carries no such timestamp.
func ReadPBFHeaderTimestamp(r io.Reader) (ts time.Time, ok bool, err error) {
	s := &PBFScanner{r: r}
	typ, data, err := s.readBlock()
	if err != nil {
		return time.Time{}, false, fmt.Errorf("reading PBF header block: %w", err)
	}
	if typ != "OSMHeader" {
		return time.Time{}, false, fmt.Errorf("first PBF block is %q, want OSMHeader", typ)
	}
	var hb pb.HeaderBlock
	if err := proto.Unmarshal(data, &hb); err != nil {
		return time.Time{}, false, fmt.Errorf("unmarshaling header block: %w", err)
	}
	if hb.OsmosisReplicationTimestamp == nil {
		return time.Time{}, false, nil
	}
	return time.Unix(hb.GetOsmosisReplicationTimestamp(), 0).UTC(), true, nil
}

// BuildPBFBytesWithReplicationTimestamp returns a minimal PBF byte stream whose
// header carries the given osmosis_replication_timestamp (unix seconds). It is
// intended for tests in other packages.
func BuildPBFBytesWithReplicationTimestamp(unixSeconds int64) []byte {
	header := &pb.HeaderBlock{
		RequiredFeatures:            []string{"OsmSchema-V0.6", "DenseNodes"},
		OsmosisReplicationTimestamp: &unixSeconds,
	}
	var buf bytes.Buffer
	writePBFBlock(&buf, "OSMHeader", mustMarshalMsg(header))
	return buf.Bytes()
}
