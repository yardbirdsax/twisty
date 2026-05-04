package osmconv

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func writeGzipXMLFile(t *testing.T, dir string, name string, objects []Object) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	gz := gzip.NewWriter(f)
	w := NewXMLWriter(gz)
	if err := w.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	for _, o := range objects {
		if err := w.WriteObject(o); err != nil {
			t.Fatalf("WriteObject: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("XMLWriter.Close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip.Close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file.Close: %v", err)
	}
	return path
}

func TestXMLScanner_RoundTrip(t *testing.T) {
	objects := []Object{
		{
			Type: NodeType,
			ID:   123,
			Node: &Node{ID: 123, Lat: 51.5074, Lon: -0.1278, Tags: map[string]string{"name": "London", "place": "city"}},
		},
		{
			Type: NodeType,
			ID:   456,
			Node: &Node{ID: 456, Lat: 48.8566, Lon: 2.3522},
		},
		{
			Type: WayType,
			ID:   789,
			Way:  &Way{ID: 789, NodeIDs: []int64{123, 456}, Tags: map[string]string{"highway": "primary"}},
		},
		{
			Type: RelationType,
			ID:   999,
			Rel: &Relation{
				ID:      999,
				Members: []Member{{Type: WayType, ID: 789, Role: "outer"}},
				Tags:    map[string]string{"type": "multipolygon"},
			},
		},
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	w := NewXMLWriter(gz)
	if err := w.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	for _, o := range objects {
		if err := w.WriteObject(o); err != nil {
			t.Fatalf("WriteObject: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("XMLWriter.Close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip.Close: %v", err)
	}

	// Write to temp file and read back via GzipXMLScanner
	dir := t.TempDir()
	path := filepath.Join(dir, "test.osm.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	scanner, err := NewGzipXMLScanner(path)
	if err != nil {
		t.Fatalf("NewGzipXMLScanner: %v", err)
	}
	defer scanner.Close()

	var got []Object
	for scanner.Next() {
		got = append(got, scanner.Object())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	if len(got) != len(objects) {
		t.Fatalf("got %d objects, want %d", len(got), len(objects))
	}

	// Node 123
	assertObject(t, got[0], NodeType, 123)
	if got[0].Node == nil {
		t.Fatal("node 123: Node is nil")
	}
	assertCoord(t, "node123.Lat", got[0].Node.Lat, 51.5074)
	assertCoord(t, "node123.Lon", got[0].Node.Lon, -0.1278)
	if got[0].Node.Tags["name"] != "London" {
		t.Errorf("node123 name = %q, want London", got[0].Node.Tags["name"])
	}
	if got[0].Node.Tags["place"] != "city" {
		t.Errorf("node123 place = %q, want city", got[0].Node.Tags["place"])
	}

	// Node 456 (no tags)
	assertObject(t, got[1], NodeType, 456)
	if got[1].Node == nil {
		t.Fatal("node 456: Node is nil")
	}
	assertCoord(t, "node456.Lat", got[1].Node.Lat, 48.8566)
	assertCoord(t, "node456.Lon", got[1].Node.Lon, 2.3522)
	if len(got[1].Node.Tags) != 0 {
		t.Errorf("node 456 should have no tags, got %v", got[1].Node.Tags)
	}

	// Way 789
	assertObject(t, got[2], WayType, 789)
	if got[2].Way == nil {
		t.Fatal("way 789: Way is nil")
	}
	wantRefs := []int64{123, 456}
	if len(got[2].Way.NodeIDs) != len(wantRefs) {
		t.Fatalf("way789 refs: got %v, want %v", got[2].Way.NodeIDs, wantRefs)
	}
	for i, ref := range wantRefs {
		if got[2].Way.NodeIDs[i] != ref {
			t.Errorf("way789.NodeIDs[%d] = %d, want %d", i, got[2].Way.NodeIDs[i], ref)
		}
	}
	if got[2].Way.Tags["highway"] != "primary" {
		t.Errorf("way789 highway = %q, want primary", got[2].Way.Tags["highway"])
	}

	// Relation 999
	assertObject(t, got[3], RelationType, 999)
	if got[3].Rel == nil {
		t.Fatal("relation 999: Rel is nil")
	}
	if got[3].Rel.Tags["type"] != "multipolygon" {
		t.Errorf("rel999 type = %q, want multipolygon", got[3].Rel.Tags["type"])
	}
	if len(got[3].Rel.Members) != 1 {
		t.Fatalf("rel999 members: got %d, want 1", len(got[3].Rel.Members))
	}
	m := got[3].Rel.Members[0]
	if m.Type != WayType || m.ID != 789 || m.Role != "outer" {
		t.Errorf("rel999 member = %+v, want {WayType, 789, outer}", m)
	}
}

func TestXMLScanner_SortOrder(t *testing.T) {
	// Write objects in sorted order (nodes < ways < relations, ascending ID)
	objects := []Object{
		{Type: NodeType, ID: 1, Node: &Node{ID: 1}},
		{Type: NodeType, ID: 2, Node: &Node{ID: 2}},
		{Type: WayType, ID: 10, Way: &Way{ID: 10}},
		{Type: RelationType, ID: 100, Rel: &Relation{ID: 100}},
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	w := NewXMLWriter(gz)
	_ = w.WriteHeader()
	for _, o := range objects {
		_ = w.WriteObject(o)
	}
	_ = w.Close()
	_ = gz.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "sorted.osm.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	scanner, err := NewGzipXMLScanner(path)
	if err != nil {
		t.Fatalf("NewGzipXMLScanner: %v", err)
	}
	defer scanner.Close()

	want := []struct {
		typ ObjectType
		id  int64
	}{
		{NodeType, 1},
		{NodeType, 2},
		{WayType, 10},
		{RelationType, 100},
	}

	var got []Object
	for scanner.Next() {
		got = append(got, scanner.Object())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d objects, want %d", len(got), len(want))
	}
	for i, w := range want {
		assertObject(t, got[i], w.typ, w.id)
	}
}

func TestXMLScanner_EmptyFile(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	w := NewXMLWriter(gz)
	_ = w.WriteHeader()
	_ = w.Close()
	_ = gz.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.osm.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	scanner, err := NewGzipXMLScanner(path)
	if err != nil {
		t.Fatalf("NewGzipXMLScanner: %v", err)
	}
	defer scanner.Close()

	if scanner.Next() {
		t.Error("expected no objects from empty XML file")
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestXMLScanner_ErrNilOnCleanEOF(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	w := NewXMLWriter(gz)
	_ = w.WriteHeader()
	_ = w.WriteObject(Object{Type: NodeType, ID: 1, Node: &Node{ID: 1}})
	_ = w.Close()
	_ = gz.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "one.osm.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	scanner, err := NewGzipXMLScanner(path)
	if err != nil {
		t.Fatalf("NewGzipXMLScanner: %v", err)
	}
	defer scanner.Close()

	for scanner.Next() {
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Err() should be nil on clean EOF, got: %v", err)
	}
}

func TestXMLScanner_MergeScannersIntegration(t *testing.T) {
	dir := t.TempDir()

	file1Objects := []Object{
		{Type: NodeType, ID: 1, Node: &Node{ID: 1, Lat: 1.0, Lon: 1.0}},
		{Type: NodeType, ID: 3, Node: &Node{ID: 3, Lat: 3.0, Lon: 3.0}},
		{Type: WayType, ID: 10, Way: &Way{ID: 10, NodeIDs: []int64{1, 3}}},
	}
	file2Objects := []Object{
		{Type: NodeType, ID: 2, Node: &Node{ID: 2, Lat: 2.0, Lon: 2.0}},
		{Type: NodeType, ID: 3, Node: &Node{ID: 3, Lat: 3.0, Lon: 3.0}}, // duplicate
		{Type: WayType, ID: 10, Way: &Way{ID: 10, NodeIDs: []int64{1, 3}}}, // duplicate
		{Type: WayType, ID: 20, Way: &Way{ID: 20, NodeIDs: []int64{2}}},
	}

	path1 := writeGzipXMLFile(t, dir, "a.osm.gz", file1Objects)
	path2 := writeGzipXMLFile(t, dir, "b.osm.gz", file2Objects)

	s1, err := NewGzipXMLScanner(path1)
	if err != nil {
		t.Fatalf("open %s: %v", path1, err)
	}
	defer s1.Close()

	s2, err := NewGzipXMLScanner(path2)
	if err != nil {
		t.Fatalf("open %s: %v", path2, err)
	}
	defer s2.Close()

	merged := MergeScanners(s1, s2)
	var got []Object
	for merged.Next() {
		got = append(got, merged.Object())
	}
	if err := merged.Err(); err != nil {
		t.Fatalf("merge error: %v", err)
	}

	want := []struct {
		typ ObjectType
		id  int64
	}{
		{NodeType, 1},
		{NodeType, 2},
		{NodeType, 3},
		{WayType, 10},
		{WayType, 20},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d objects, want %d", len(got), len(want))
	}
	for i, w := range want {
		assertObject(t, got[i], w.typ, w.id)
	}
}

func TestXMLScanner_SpecialChars(t *testing.T) {
	objects := []Object{
		{
			Type: NodeType,
			ID:   1,
			Node: &Node{
				ID:   1,
				Tags: map[string]string{"name": `O'Brien & "Friends" <3`},
			},
		},
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	w := NewXMLWriter(gz)
	_ = w.WriteHeader()
	for _, o := range objects {
		_ = w.WriteObject(o)
	}
	_ = w.Close()
	_ = gz.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "special.osm.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	scanner, err := NewGzipXMLScanner(path)
	if err != nil {
		t.Fatalf("NewGzipXMLScanner: %v", err)
	}
	defer scanner.Close()

	if !scanner.Next() {
		t.Fatalf("expected one object, got none; err=%v", scanner.Err())
	}
	got := scanner.Object()
	if got.Node == nil {
		t.Fatal("Node is nil")
	}
	want := `O'Brien & "Friends" <3`
	if got.Node.Tags["name"] != want {
		t.Errorf("name = %q, want %q", got.Node.Tags["name"], want)
	}
}
