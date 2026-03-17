# Task 009: GPX File Writer

## Summary

Implement the gpx package to write a valid GPX 1.1 file from the selected route's decoded coordinate points.

## Dependencies

Task 002 (geo.Coord), Task 005 (route.Route)

## Detailed Directions

### 1. Define the GPX XML Types

In gpx/gpx.go, define Go structs matching the GPX 1.1 schema for marshaling:

```go
package gpx

import (
    "encoding/xml"
    "github.com/yardbirdsax/twisty/geo"
)

type GPX struct {
    XMLName xml.Name `xml:"gpx"`
    Version string   `xml:"version,attr"`
    Creator string   `xml:"creator,attr"`
    Xmlns   string   `xml:"xmlns,attr"`
    Trk     Track    `xml:"trk"`
}

type Track struct {
    Name   string      `xml:"name"`
    TrkSeg TrackSeg    `xml:"trkseg"`
}

type TrackSeg struct {
    Points []TrackPoint `xml:"trkpt"`
}

type TrackPoint struct {
    Lat float64 `xml:"lat,attr"`
    Lon float64 `xml:"lon,attr"`
}
```

### 2. Implement WriteGPX

```go
// WriteGPX writes a GPX 1.1 file to the given path containing the route points.
// trackName is used as the trk name value.
func WriteGPX(path string, points []geo.Coord, trackName string) error
```

Steps:
1. Create the output file with os.Create(path). Return any error.
2. Write the XML declaration manually: XML version 1.0 encoding UTF-8
3. Build the GPX struct with Version 1.1, Creator twistrouter, proper xmlns
4. Use encoding/xml.NewEncoder with Indent to write the XML.
5. Call encoder.Encode and return any error.

The track name format (to be passed in from main.go):
```go
trackName := fmt.Sprintf("Twist Route (factor=%.1f, score=%.0f)", twistFactor, adjustedScore)
```

### 3. Write Unit Tests

Create gpx/gpx_test.go:

- Call WriteGPX with 3 known coordinate points to a temp file.
- Read back and parse the XML; verify:
  - Root element is gpx with version 1.1
  - Track name matches the provided string
  - Three trkpt elements with correct lat/lon attributes
  - File starts with the XML declaration

## Acceptance Criteria

- [ ] go test ./gpx/ passes
- [ ] Written GPX files begin with XML version 1.0 encoding UTF-8
- [ ] Root element is gpx with version 1.1, creator twistrouter, and correct xmlns
- [ ] Each coordinate appears as trkpt with lat and lon attributes
- [ ] File write errors are returned (not panicked)
- [ ] go build ./... succeeds

## Notes

- encoding/xml does not emit the XML declaration by default. Write it manually with file.WriteString.
- GPX track points encode lat/lon as attributes, not child elements.
- The lat/lon values should be formatted with enough decimal places for GPS accuracy. Use strconv.FormatFloat or let encoding/xml handle float marshaling via the attr tag.

---
# Task 009 Review: GPX File Writer

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-16
**Verdict:** APPROVED

---

## Summary

Implements the `gpx` package with XML struct types matching the GPX 1.1 schema and a `WriteGPX` function that creates a GPX file from a slice of `geo.Coord` points. Tests cover both the happy path (3 points, XML validation) and file-write error handling.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/gpx/gpx.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/gpx/gpx_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `go test ./gpx/` passes | PASS |
| Written GPX files begin with XML version 1.0 encoding UTF-8 | PASS |
| Root element is gpx with version 1.1, creator twistrouter, and correct xmlns | PASS |
| Each coordinate appears as trkpt with lat and lon attributes | PASS |
| File write errors are returned (not panicked) | PASS |
| `go build ./...` succeeds | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Good Practices Observed

1. **enc.Flush called explicitly:** `enc.Flush()` is called after `enc.Encode`, which ensures any buffered XML bytes are written to the file before the deferred `f.Close()` runs.

---

## Verification Commands Run

```bash
go test ./gpx/        # ok  github.com/yardbirdsax/twisty/gpx  0.176s
go build ./...        # success (no output)
go vet ./...          # no issues in gpx package
go test ./...         # gpx: PASS; route integration test fails due to network sandbox restriction, unrelated to this task
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. The implementation matches the specification exactly and tests are thorough. The only test failure in the repository is an OSRM integration test blocked by network restrictions, which is unrelated to this task.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
