package osmconv

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
)

func TestXMLWriter_Node(t *testing.T) {
	var buf bytes.Buffer
	w := NewXMLWriter(&buf)

	if err := w.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if err := w.WriteObject(Object{
		Type: NodeType,
		ID:   42,
		Node: &Node{
			ID:  42,
			Lat: 51.5074,
			Lon: -0.1278,
			Tags: map[string]string{
				"name":    "London",
				"place":   "city",
			},
		},
	}); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	out := buf.String()

	// Must start with XML declaration
	if !strings.HasPrefix(out, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("missing XML declaration, got prefix: %q", out[:min(len(out), 60)])
	}

	// Must contain <osm> root element
	if !strings.Contains(out, `<osm version="0.6"`) {
		t.Error("missing <osm> root element with version")
	}

	// Must contain the node
	if !strings.Contains(out, `<node id="42"`) {
		t.Error("missing node element")
	}
	if !strings.Contains(out, `lat="51.5074"`) {
		t.Error("missing lat attribute")
	}
	if !strings.Contains(out, `lon="-0.1278"`) {
		t.Error("missing lon attribute")
	}

	// Must end with </osm>
	if !strings.HasSuffix(strings.TrimSpace(out), "</osm>") {
		t.Errorf("missing closing </osm>, got suffix: %q", out[max(0, len(out)-30):])
	}

	// Verify well-formed XML
	if err := xml.Unmarshal([]byte(out), new(interface{})); err != nil {
		t.Errorf("output is not well-formed XML: %v", err)
	}
}

func TestXMLWriter_Way(t *testing.T) {
	var buf bytes.Buffer
	w := NewXMLWriter(&buf)
	_ = w.WriteHeader()
	if err := w.WriteObject(Object{
		Type: WayType,
		ID:   100,
		Way: &Way{
			ID:      100,
			NodeIDs: []int64{1, 2, 3},
			Tags:    map[string]string{"highway": "primary"},
		},
	}); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	_ = w.Close()

	out := buf.String()
	if !strings.Contains(out, `<way id="100"`) {
		t.Error("missing way element")
	}
	if !strings.Contains(out, `<nd ref="1"/>`) {
		t.Error("missing nd ref for node 1")
	}
	if !strings.Contains(out, `<nd ref="2"/>`) {
		t.Error("missing nd ref for node 2")
	}
	if !strings.Contains(out, `<nd ref="3"/>`) {
		t.Error("missing nd ref for node 3")
	}
	if !strings.Contains(out, `<tag k="highway" v="primary"/>`) {
		t.Error("missing highway tag")
	}
}

func TestXMLWriter_Relation(t *testing.T) {
	var buf bytes.Buffer
	w := NewXMLWriter(&buf)
	_ = w.WriteHeader()
	if err := w.WriteObject(Object{
		Type: RelationType,
		ID:   200,
		Rel: &Relation{
			ID: 200,
			Members: []Member{
				{Type: WayType, ID: 100, Role: "outer"},
				{Type: WayType, ID: 101, Role: "inner"},
			},
			Tags: map[string]string{"type": "multipolygon"},
		},
	}); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	_ = w.Close()

	out := buf.String()
	if !strings.Contains(out, `<relation id="200"`) {
		t.Error("missing relation element")
	}
	if !strings.Contains(out, `type="way"`) {
		t.Error("missing member type=way")
	}
	if !strings.Contains(out, `ref="100"`) {
		t.Error("missing member ref=100")
	}
	if !strings.Contains(out, `role="outer"`) {
		t.Error("missing member role=outer")
	}
}

func TestXMLWriter_NodeWithNoTags(t *testing.T) {
	var buf bytes.Buffer
	w := NewXMLWriter(&buf)
	_ = w.WriteHeader()
	if err := w.WriteObject(Object{
		Type: NodeType,
		ID:   1,
		Node: &Node{ID: 1, Lat: 0.0, Lon: 0.0},
	}); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	_ = w.Close()

	out := buf.String()
	// A node with no tags should be self-closing
	if !strings.Contains(out, `<node id="1"`) {
		t.Error("missing node element")
	}
	if strings.Contains(out, "<tag") {
		t.Error("should have no tag elements for tagless node")
	}
}

func TestXMLWriter_EscapesSpecialChars(t *testing.T) {
	var buf bytes.Buffer
	w := NewXMLWriter(&buf)
	_ = w.WriteHeader()
	if err := w.WriteObject(Object{
		Type: NodeType,
		ID:   1,
		Node: &Node{
			ID: 1, Lat: 0, Lon: 0,
			Tags: map[string]string{"name": `O'Brien & "Friends" <3`},
		},
	}); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	_ = w.Close()

	out := buf.String()
	// Must be valid XML despite special characters
	if err := xml.Unmarshal([]byte(out), new(interface{})); err != nil {
		t.Errorf("output with special chars is not well-formed XML: %v", err)
	}
}
