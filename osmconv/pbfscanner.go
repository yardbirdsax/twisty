package osmconv

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"

	pb "github.com/yardbirdsax/twisty/osmconv/internal/osmpbf"
)

// PBFScanner reads OSM objects from a PBF-format stream.
// It implements ObjectScanner.
type PBFScanner struct {
	r       io.Reader
	pending []Object // buffered objects from current block
	pos     int
	current Object
	err     error
	started bool
}

// NewPBFScanner creates a PBFScanner that reads from r.
func NewPBFScanner(r io.Reader) *PBFScanner {
	return &PBFScanner{r: r}
}

func (s *PBFScanner) Next() bool {
	for {
		// Drain buffered objects from current block
		if s.pos < len(s.pending) {
			s.current = s.pending[s.pos]
			s.pos++
			return true
		}

		// Read next block
		s.pending = s.pending[:0]
		s.pos = 0

		blobType, data, err := s.readBlock()
		if err == io.EOF {
			return false
		}
		if err != nil {
			s.err = err
			return false
		}

		switch blobType {
		case "OSMHeader":
			// Skip header block — we don't need metadata
			continue
		case "OSMData":
			if err := s.decodePrimitiveBlock(data); err != nil {
				s.err = fmt.Errorf("decoding primitive block: %w", err)
				return false
			}
			// Loop back to drain pending
			continue
		default:
			// Unknown block type, skip
			continue
		}
	}
}

func (s *PBFScanner) Object() Object {
	return s.current
}

func (s *PBFScanner) Err() error {
	return s.err
}

// readBlock reads one blob header + blob from the stream.
// Returns the blob type, decompressed data, and any error.
func (s *PBFScanner) readBlock() (string, []byte, error) {
	// Read 4-byte header length (big-endian)
	var headerLen int32
	if err := binary.Read(s.r, binary.BigEndian, &headerLen); err != nil {
		// io.EOF means a clean end-of-stream (no bytes for the next block).
		// io.ErrUnexpectedEOF means we hit EOF mid-read of the 4-byte header-length
		// field — i.e., before any block data was consumed — which is also a clean
		// end-of-file rather than a real truncation error.
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return "", nil, io.EOF
		}
		return "", nil, fmt.Errorf("reading header length: %w", err)
	}

	// Read and parse BlobHeader
	headerBytes := make([]byte, headerLen)
	if _, err := io.ReadFull(s.r, headerBytes); err != nil {
		return "", nil, fmt.Errorf("reading blob header: %w", err)
	}
	var blobHeader pb.BlobHeader
	if err := proto.Unmarshal(headerBytes, &blobHeader); err != nil {
		return "", nil, fmt.Errorf("unmarshaling blob header: %w", err)
	}

	// Read and parse Blob
	blobBytes := make([]byte, blobHeader.GetDatasize())
	if _, err := io.ReadFull(s.r, blobBytes); err != nil {
		return "", nil, fmt.Errorf("reading blob: %w", err)
	}
	var blob pb.Blob
	if err := proto.Unmarshal(blobBytes, &blob); err != nil {
		return "", nil, fmt.Errorf("unmarshaling blob: %w", err)
	}

	// Decompress
	data, err := decompressBlob(&blob)
	if err != nil {
		return "", nil, fmt.Errorf("decompressing blob: %w", err)
	}

	return blobHeader.GetType(), data, nil
}

func decompressBlob(blob *pb.Blob) ([]byte, error) {
	switch d := blob.Data.(type) {
	case *pb.Blob_Raw:
		return d.Raw, nil
	case *pb.Blob_ZlibData:
		r, err := zlib.NewReader(bytes.NewReader(d.ZlibData))
		if err != nil {
			return nil, fmt.Errorf("zlib reader: %w", err)
		}
		defer r.Close()
		return io.ReadAll(r)
	default:
		return nil, fmt.Errorf("unsupported blob compression type: %T", d)
	}
}

func (s *PBFScanner) decodePrimitiveBlock(data []byte) error {
	var block pb.PrimitiveBlock
	if err := proto.Unmarshal(data, &block); err != nil {
		return fmt.Errorf("unmarshaling primitive block: %w", err)
	}

	st := block.GetStringtable().GetS()
	granularity := int64(block.GetGranularity())
	latOffset := block.GetLatOffset()
	lonOffset := block.GetLonOffset()

	for _, group := range block.GetPrimitivegroup() {
		// DenseNodes
		if dense := group.GetDense(); dense != nil {
			s.decodeDenseNodes(dense, st, granularity, latOffset, lonOffset)
		}

		// Regular nodes
		for _, n := range group.GetNodes() {
			s.decodeNode(n, st, granularity, latOffset, lonOffset)
		}

		// Ways
		for _, w := range group.GetWays() {
			s.decodeWay(w, st)
		}

		// Relations
		for _, r := range group.GetRelations() {
			s.decodeRelation(r, st)
		}
	}

	return nil
}

func (s *PBFScanner) decodeDenseNodes(
	dense *pb.DenseNodes,
	st [][]byte,
	granularity, latOffset, lonOffset int64,
) {
	ids := dense.GetId()
	lats := dense.GetLat()
	lons := dense.GetLon()
	keysVals := dense.GetKeysVals()

	var id, lat, lon int64
	kvIdx := 0

	for i := range ids {
		// Delta decode
		id += ids[i]
		lat += lats[i]
		lon += lons[i]

		node := &Node{
			ID:  id,
			Lat: toCoord(lat, granularity, latOffset),
			Lon: toCoord(lon, granularity, lonOffset),
		}

		// Decode tags from keys_vals
		if kvIdx < len(keysVals) {
			for kvIdx < len(keysVals) && keysVals[kvIdx] != 0 {
				keyIdx := keysVals[kvIdx]
				valIdx := keysVals[kvIdx+1]
				if node.Tags == nil {
					node.Tags = make(map[string]string)
				}
				node.Tags[string(st[keyIdx])] = string(st[valIdx])
				kvIdx += 2
			}
			kvIdx++ // skip the 0 delimiter
		}

		s.pending = append(s.pending, Object{
			Type: NodeType,
			ID:   id,
			Node: node,
		})
	}
}

func (s *PBFScanner) decodeNode(
	n *pb.Node,
	st [][]byte,
	granularity, latOffset, lonOffset int64,
) {
	node := &Node{
		ID:  n.GetId(),
		Lat: toCoord(n.GetLat(), granularity, latOffset),
		Lon: toCoord(n.GetLon(), granularity, lonOffset),
		Tags: decodeTags(n.GetKeys(), n.GetVals(), st),
	}
	s.pending = append(s.pending, Object{
		Type: NodeType,
		ID:   node.ID,
		Node: node,
	})
}

func (s *PBFScanner) decodeWay(w *pb.Way, st [][]byte) {
	// Delta-decode refs
	refs := make([]int64, len(w.GetRefs()))
	var ref int64
	for i, delta := range w.GetRefs() {
		ref += delta
		refs[i] = ref
	}

	way := &Way{
		ID:      w.GetId(),
		NodeIDs: refs,
		Tags:    decodeTags(w.GetKeys(), w.GetVals(), st),
	}
	s.pending = append(s.pending, Object{
		Type: WayType,
		ID:   way.ID,
		Way:  way,
	})
}

func (s *PBFScanner) decodeRelation(r *pb.Relation, st [][]byte) {
	memIDs := r.GetMemids()
	memTypes := r.GetTypes()
	rolesSID := r.GetRolesSid()

	members := make([]Member, len(memIDs))
	var memID int64
	for i := range memIDs {
		memID += memIDs[i] // delta decode
		members[i] = Member{
			ID:   memID,
			Type: pbMemberType(memTypes[i]),
			Role: string(st[rolesSID[i]]),
		}
	}

	rel := &Relation{
		ID:      r.GetId(),
		Members: members,
		Tags:    decodeTags(r.GetKeys(), r.GetVals(), st),
	}
	s.pending = append(s.pending, Object{
		Type: RelationType,
		ID:   rel.ID,
		Rel:  rel,
	})
}

func toCoord(raw, granularity, offset int64) float64 {
	return 1e-9 * float64(offset+granularity*raw)
}

func decodeTags(keys, vals []uint32, st [][]byte) map[string]string {
	if len(keys) == 0 {
		return nil
	}
	tags := make(map[string]string, len(keys))
	for i := range keys {
		tags[string(st[keys[i]])] = string(st[vals[i]])
	}
	return tags
}

func pbMemberType(mt pb.Relation_MemberType) ObjectType {
	switch mt {
	case pb.Relation_NODE:
		return NodeType
	case pb.Relation_WAY:
		return WayType
	case pb.Relation_RELATION:
		return RelationType
	default:
		return NodeType
	}
}
