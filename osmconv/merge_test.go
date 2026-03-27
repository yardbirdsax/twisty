package osmconv

import "testing"

// sliceScanner is a test helper that implements ObjectScanner over a slice.
type sliceScanner struct {
	objects []Object
	pos     int
}

func newSliceScanner(objects ...Object) *sliceScanner {
	return &sliceScanner{objects: objects, pos: -1}
}

func (s *sliceScanner) Next() bool {
	s.pos++
	return s.pos < len(s.objects)
}

func (s *sliceScanner) Object() Object {
	return s.objects[s.pos]
}

func (s *sliceScanner) Err() error { return nil }

func TestMergeScanners_SingleScanner(t *testing.T) {
	s := newSliceScanner(
		Object{Type: NodeType, ID: 1},
		Object{Type: NodeType, ID: 2},
		Object{Type: WayType, ID: 10},
	)

	merged := MergeScanners(s)
	var got []Object
	for merged.Next() {
		got = append(got, merged.Object())
	}
	if err := merged.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d objects, want 3", len(got))
	}
	assertObject(t, got[0], NodeType, 1)
	assertObject(t, got[1], NodeType, 2)
	assertObject(t, got[2], WayType, 10)
}

func TestMergeScanners_TwoScanners(t *testing.T) {
	s1 := newSliceScanner(
		Object{Type: NodeType, ID: 1},
		Object{Type: NodeType, ID: 3},
		Object{Type: WayType, ID: 10},
	)
	s2 := newSliceScanner(
		Object{Type: NodeType, ID: 2},
		Object{Type: NodeType, ID: 4},
		Object{Type: WayType, ID: 5},
	)

	merged := MergeScanners(s1, s2)
	var got []Object
	for merged.Next() {
		got = append(got, merged.Object())
	}
	if err := merged.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []struct {
		typ ObjectType
		id  int64
	}{
		{NodeType, 1},
		{NodeType, 2},
		{NodeType, 3},
		{NodeType, 4},
		{WayType, 5},
		{WayType, 10},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d objects, want %d", len(got), len(want))
	}
	for i, w := range want {
		assertObject(t, got[i], w.typ, w.id)
	}
}

func TestMergeScanners_Deduplication(t *testing.T) {
	s1 := newSliceScanner(
		Object{Type: NodeType, ID: 1},
		Object{Type: NodeType, ID: 2},
		Object{Type: WayType, ID: 10},
	)
	s2 := newSliceScanner(
		Object{Type: NodeType, ID: 2}, // duplicate
		Object{Type: NodeType, ID: 3},
		Object{Type: WayType, ID: 10}, // duplicate
	)

	merged := MergeScanners(s1, s2)
	var got []Object
	for merged.Next() {
		got = append(got, merged.Object())
	}
	if err := merged.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []struct {
		typ ObjectType
		id  int64
	}{
		{NodeType, 1},
		{NodeType, 2},
		{NodeType, 3},
		{WayType, 10},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d objects, want %d", len(got), len(want))
	}
	for i, w := range want {
		assertObject(t, got[i], w.typ, w.id)
	}
}

func TestMergeScanners_ThreeScanners(t *testing.T) {
	s1 := newSliceScanner(
		Object{Type: NodeType, ID: 1},
		Object{Type: RelationType, ID: 100},
	)
	s2 := newSliceScanner(
		Object{Type: WayType, ID: 50},
	)
	s3 := newSliceScanner(
		Object{Type: NodeType, ID: 2},
		Object{Type: WayType, ID: 51},
	)

	merged := MergeScanners(s1, s2, s3)
	var got []Object
	for merged.Next() {
		got = append(got, merged.Object())
	}

	want := []struct {
		typ ObjectType
		id  int64
	}{
		{NodeType, 1},
		{NodeType, 2},
		{WayType, 50},
		{WayType, 51},
		{RelationType, 100},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d objects, want %d", len(got), len(want))
	}
	for i, w := range want {
		assertObject(t, got[i], w.typ, w.id)
	}
}

func TestMergeScanners_EmptyScanners(t *testing.T) {
	s1 := newSliceScanner()
	s2 := newSliceScanner(Object{Type: NodeType, ID: 1})
	s3 := newSliceScanner()

	merged := MergeScanners(s1, s2, s3)
	var got []Object
	for merged.Next() {
		got = append(got, merged.Object())
	}

	if len(got) != 1 {
		t.Fatalf("got %d objects, want 1", len(got))
	}
	assertObject(t, got[0], NodeType, 1)
}

func assertObject(t *testing.T, o Object, typ ObjectType, id int64) {
	t.Helper()
	if o.Type != typ || o.ID != id {
		t.Errorf("got Object{Type: %d, ID: %d}, want Object{Type: %d, ID: %d}",
			o.Type, o.ID, typ, id)
	}
}
