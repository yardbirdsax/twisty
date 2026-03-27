package osmconv

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
)

// XMLWriter writes OSM objects as Overpass-compatible OSM XML.
// It implements ObjectWriter.
type XMLWriter struct {
	w *bufio.Writer
}

// NewXMLWriter creates an XMLWriter that writes to w.
func NewXMLWriter(w io.Writer) *XMLWriter {
	return &XMLWriter{w: bufio.NewWriter(w)}
}

func (x *XMLWriter) WriteHeader() error {
	_, err := x.w.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<osm version="0.6" generator="twisty">` + "\n")
	return err
}

func (x *XMLWriter) WriteObject(o Object) error {
	switch o.Type {
	case NodeType:
		return x.writeNode(o.Node)
	case WayType:
		return x.writeWay(o.Way)
	case RelationType:
		return x.writeRelation(o.Rel)
	default:
		return fmt.Errorf("unknown object type: %d", o.Type)
	}
}

func (x *XMLWriter) Close() error {
	if _, err := x.w.WriteString("</osm>\n"); err != nil {
		return err
	}
	return x.w.Flush()
}

func (x *XMLWriter) writeNode(n *Node) error {
	if len(n.Tags) == 0 {
		_, err := fmt.Fprintf(x.w, "  <node id=\"%d\" lat=\"%s\" lon=\"%s\"/>\n",
			n.ID, formatCoord(n.Lat), formatCoord(n.Lon))
		return err
	}
	if _, err := fmt.Fprintf(x.w, "  <node id=\"%d\" lat=\"%s\" lon=\"%s\">\n",
		n.ID, formatCoord(n.Lat), formatCoord(n.Lon)); err != nil {
		return err
	}
	if err := x.writeTags(n.Tags); err != nil {
		return err
	}
	_, err := x.w.WriteString("  </node>\n")
	return err
}

func (x *XMLWriter) writeWay(w *Way) error {
	if _, err := fmt.Fprintf(x.w, "  <way id=\"%d\">\n", w.ID); err != nil {
		return err
	}
	for _, ref := range w.NodeIDs {
		if _, err := fmt.Fprintf(x.w, "    <nd ref=\"%d\"/>\n", ref); err != nil {
			return err
		}
	}
	if err := x.writeTags(w.Tags); err != nil {
		return err
	}
	_, err := x.w.WriteString("  </way>\n")
	return err
}

func (x *XMLWriter) writeRelation(r *Relation) error {
	if _, err := fmt.Fprintf(x.w, "  <relation id=\"%d\">\n", r.ID); err != nil {
		return err
	}
	for _, m := range r.Members {
		typeName := memberTypeName(m.Type)
		if _, err := fmt.Fprintf(x.w, "    <member type=\"%s\" ref=\"%d\" role=\"%s\"/>\n",
			typeName, m.ID, xmlEscape(m.Role)); err != nil {
			return err
		}
	}
	if err := x.writeTags(r.Tags); err != nil {
		return err
	}
	_, err := x.w.WriteString("  </relation>\n")
	return err
}

func (x *XMLWriter) writeTags(tags map[string]string) error {
	// Sort keys for deterministic output
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := fmt.Fprintf(x.w, "    <tag k=\"%s\" v=\"%s\"/>\n",
			xmlEscape(k), xmlEscape(tags[k])); err != nil {
			return err
		}
	}
	return nil
}

func memberTypeName(t ObjectType) string {
	switch t {
	case NodeType:
		return "node"
	case WayType:
		return "way"
	case RelationType:
		return "relation"
	default:
		return "unknown"
	}
}

func formatCoord(c float64) string {
	s := fmt.Sprintf("%.7f", c)
	// Trim trailing zeros but keep at least one decimal
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
