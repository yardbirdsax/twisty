package osmconv

import (
	"compress/gzip"
	"encoding/xml"
	"html"
	"io"
	"os"
	"strconv"
)

// XMLScanner reads OSM XML from an io.Reader token-by-token.
type XMLScanner struct {
	dec     *xml.Decoder
	current Object
	err     error
	done    bool
}

// NewXMLScanner creates an XMLScanner reading from r.
func NewXMLScanner(r io.Reader) *XMLScanner {
	return &XMLScanner{dec: xml.NewDecoder(r)}
}

func (s *XMLScanner) Next() bool {
	if s.done || s.err != nil {
		return false
	}
	for {
		tok, err := s.dec.Token()
		if err == io.EOF {
			s.done = true
			return false
		}
		if err != nil {
			s.err = err
			return false
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "node":
			obj, err := s.parseNode(start)
			if err != nil {
				s.err = err
				return false
			}
			s.current = obj
			return true
		case "way":
			obj, err := s.parseWay(start)
			if err != nil {
				s.err = err
				return false
			}
			s.current = obj
			return true
		case "relation":
			obj, err := s.parseRelation(start)
			if err != nil {
				s.err = err
				return false
			}
			s.current = obj
			return true
		}
	}
}

func (s *XMLScanner) Object() Object { return s.current }
func (s *XMLScanner) Err() error     { return s.err }

func findAttr(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func (s *XMLScanner) parseNode(start xml.StartElement) (Object, error) {
	id, err := strconv.ParseInt(findAttr(start.Attr, "id"), 10, 64)
	if err != nil {
		return Object{}, err
	}
	lat, err := strconv.ParseFloat(findAttr(start.Attr, "lat"), 64)
	if err != nil {
		return Object{}, err
	}
	lon, err := strconv.ParseFloat(findAttr(start.Attr, "lon"), 64)
	if err != nil {
		return Object{}, err
	}
	node := &Node{ID: id, Lat: lat, Lon: lon}

	tags, err := s.parseTags()
	if err != nil {
		return Object{}, err
	}
	if len(tags) > 0 {
		node.Tags = tags
	}
	return Object{Type: NodeType, ID: id, Node: node}, nil
}

func (s *XMLScanner) parseWay(start xml.StartElement) (Object, error) {
	id, err := strconv.ParseInt(findAttr(start.Attr, "id"), 10, 64)
	if err != nil {
		return Object{}, err
	}
	way := &Way{ID: id}

	for {
		tok, err := s.dec.Token()
		if err != nil {
			return Object{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "nd":
				ref, err := strconv.ParseInt(findAttr(t.Attr, "ref"), 10, 64)
				if err != nil {
					return Object{}, err
				}
				way.NodeIDs = append(way.NodeIDs, ref)
				if err := s.dec.Skip(); err != nil {
					return Object{}, err
				}
			case "tag":
				k := html.UnescapeString(findAttr(t.Attr, "k"))
				v := html.UnescapeString(findAttr(t.Attr, "v"))
				if way.Tags == nil {
					way.Tags = make(map[string]string)
				}
				way.Tags[k] = v
				if err := s.dec.Skip(); err != nil {
					return Object{}, err
				}
			default:
				if err := s.dec.Skip(); err != nil {
					return Object{}, err
				}
			}
		case xml.EndElement:
			if t.Name.Local == "way" {
				return Object{Type: WayType, ID: id, Way: way}, nil
			}
		}
	}
}

func (s *XMLScanner) parseRelation(start xml.StartElement) (Object, error) {
	id, err := strconv.ParseInt(findAttr(start.Attr, "id"), 10, 64)
	if err != nil {
		return Object{}, err
	}
	rel := &Relation{ID: id}

	for {
		tok, err := s.dec.Token()
		if err != nil {
			return Object{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "member":
				m, err := parseMember(t.Attr)
				if err != nil {
					return Object{}, err
				}
				rel.Members = append(rel.Members, m)
				if err := s.dec.Skip(); err != nil {
					return Object{}, err
				}
			case "tag":
				k := html.UnescapeString(findAttr(t.Attr, "k"))
				v := html.UnescapeString(findAttr(t.Attr, "v"))
				if rel.Tags == nil {
					rel.Tags = make(map[string]string)
				}
				rel.Tags[k] = v
				if err := s.dec.Skip(); err != nil {
					return Object{}, err
				}
			default:
				if err := s.dec.Skip(); err != nil {
					return Object{}, err
				}
			}
		case xml.EndElement:
			if t.Name.Local == "relation" {
				return Object{Type: RelationType, ID: id, Rel: rel}, nil
			}
		}
	}
}

func (s *XMLScanner) parseTags() (map[string]string, error) {
	var tags map[string]string
	for {
		tok, err := s.dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "tag" {
				k := html.UnescapeString(findAttr(t.Attr, "k"))
				v := html.UnescapeString(findAttr(t.Attr, "v"))
				if tags == nil {
					tags = make(map[string]string)
				}
				tags[k] = v
				if err := s.dec.Skip(); err != nil {
					return nil, err
				}
			} else {
				if err := s.dec.Skip(); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			return tags, nil
		}
	}
}

func parseMember(attrs []xml.Attr) (Member, error) {
	typeStr := findAttr(attrs, "type")
	ref, err := strconv.ParseInt(findAttr(attrs, "ref"), 10, 64)
	if err != nil {
		return Member{}, err
	}
	role := html.UnescapeString(findAttr(attrs, "role"))

	var objType ObjectType
	switch typeStr {
	case "node":
		objType = NodeType
	case "way":
		objType = WayType
	case "relation":
		objType = RelationType
	}
	return Member{Type: objType, ID: ref, Role: role}, nil
}

// GzipXMLScanner reads gzip-compressed OSM XML from a file.
type GzipXMLScanner struct {
	f   *os.File
	gz  *gzip.Reader
	xml *XMLScanner
}

// NewGzipXMLScanner opens path, wraps it in a gzip reader, and feeds it to an XMLScanner.
func NewGzipXMLScanner(path string) (*GzipXMLScanner, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &GzipXMLScanner{f: f, gz: gz, xml: NewXMLScanner(gz)}, nil
}

func (g *GzipXMLScanner) Next() bool    { return g.xml.Next() }
func (g *GzipXMLScanner) Object() Object { return g.xml.Object() }
func (g *GzipXMLScanner) Err() error    { return g.xml.Err() }

func (g *GzipXMLScanner) Close() error {
	gzErr := g.gz.Close()
	fErr := g.f.Close()
	if gzErr != nil {
		return gzErr
	}
	return fErr
}
