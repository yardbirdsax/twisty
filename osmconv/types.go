package osmconv

import "io"

// ObjectType distinguishes OSM element kinds.
type ObjectType int

const (
	NodeType     ObjectType = iota
	WayType
	RelationType
)

// Object is a single OSM element (node, way, or relation).
type Object struct {
	Type ObjectType
	ID   int64
	Node *Node
	Way  *Way
	Rel  *Relation
}

// Node is an OSM node with coordinates and optional tags.
type Node struct {
	ID   int64
	Lat  float64
	Lon  float64
	Tags map[string]string
}

// Way is an OSM way referencing an ordered list of node IDs.
type Way struct {
	ID      int64
	NodeIDs []int64
	Tags    map[string]string
}

// Relation is an OSM relation with typed members.
type Relation struct {
	ID      int64
	Members []Member
	Tags    map[string]string
}

// Member is a single member of a relation.
type Member struct {
	Type ObjectType
	ID   int64
	Role string
}

// ObjectLess reports whether a should sort before b.
// Objects are ordered by type (node < way < relation), then by ID ascending.
func ObjectLess(a, b Object) bool {
	if a.Type != b.Type {
		return a.Type < b.Type
	}
	return a.ID < b.ID
}

// ObjectScanner yields OSM objects one at a time from any source.
type ObjectScanner interface {
	Next() bool
	Object() Object
	Err() error
}

// ObjectWriter serializes a stream of OSM objects to a destination.
type ObjectWriter interface {
	WriteHeader() error
	WriteObject(Object) error
	Close() error
}

// CompressedWriterFunc wraps an io.Writer with compression.
type CompressedWriterFunc func(w io.Writer) (io.WriteCloser, error)
