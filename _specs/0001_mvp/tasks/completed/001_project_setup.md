# Task 001: Project Setup and CLI Scaffold

## Summary

Initialize the Go module, create the package directory structure, implement flag parsing in `main.go`, and wire a stub pipeline so the binary compiles and exits correctly on missing flags.

## Dependencies

None — this is the foundational task.

## Detailed Directions

### 1. Initialize the Go Module

From the repository root, run:

```
go mod init github.com/yardbirdsax/twisty
```

This produces `go.mod`. No external dependencies will be added; the module uses standard library only.

### 2. Create the Package Directory Structure

Create the following directories with placeholder `.go` files so the packages compile:

```
twisty/
├── go.mod
├── main.go
├── geo/
│   └── geo.go
├── geocode/
│   └── nominatim.go
├── route/
│   ├── osrm.go
│   └── score.go
├── quality/
│   └── overpass.go
└── gpx/
    └── gpx.go
```

Each stub file (except `main.go`) should contain only the package declaration:

- `geo/geo.go` → `package geo`
- `geocode/nominatim.go` → `package geocode`
- `route/osrm.go` → `package route`
- `route/score.go` → `package route`
- `quality/overpass.go` → `package quality`
- `gpx/gpx.go` → `package gpx`

### 3. Implement Flag Parsing in main.go

`main.go` should declare and parse all five flags:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-origin` | string | `""` | Origin address or `lat,lon` |
| `-dest` | string | `""` | Destination address or `lat,lon` |
| `-twist` | float64 | `0.5` | 0.0 = fastest, 1.0 = twistiest |
| `-out` | string | `"route.gpx"` | Output file path |
| `-show-all` | bool | `false` | Print candidate comparison table |

After `flag.Parse()`:
1. If `-origin` or `-dest` is empty: call `flag.Usage()`, then print `Error: -origin and -dest are required` to stderr, then exit 1.
2. If `-twist` is outside `[0.0, 1.0]`: print `Error: -twist must be between 0.0 and 1.0` to stderr, then exit 1.

### 4. Stub the Pipeline in main.go

After flag validation, add placeholder comments for each pipeline stage:

```go
// Stage 1: Resolve origin
// Stage 2: Resolve destination
// Stage 3: Fetch routes from OSRM
// Stage 4: Decode geometries and score curvature
// Stage 5: Check road quality (soft failure)
// Stage 6: Select route by twist factor
// Stage 7: Write GPX output
// Stage 8: Print summary
```

## Acceptance Criteria

- [ ] `go.mod` exists with module path `github.com/yardbirdsax/twisty`
- [ ] `go build ./...` succeeds with no errors
- [ ] Running `./twisty` (no flags) prints usage and exits with code 1
- [ ] Running with only `-origin` or only `-dest` also exits 1 with usage
- [ ] `-twist 1.5` exits 1 with a meaningful error message
- [ ] All five package directories exist with valid stub `.go` files
- [ ] All five flags are registered and parseable

## Notes

- Use `fmt.Fprintln(os.Stderr, ...)` for user-facing errors, not `log.Fatal`.
- Keep `main.go` focused on orchestration; all business logic goes in sub-packages.

---

# Task 001 Review: Project Setup and CLI Scaffold

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-16
**Verdict:** APPROVED

---

## Summary

Initializes the Go module, creates the six stub packages, implements all five flags with validation, and wires the pipeline comment scaffold in `main.go`.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/go.mod` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/geo/geo.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/geocode/nominatim.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/route/osrm.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/route/score.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/overpass.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/gpx/gpx.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `go.mod` exists with module path `github.com/yardbirdsax/twisty` | PASS |
| `go build ./...` succeeds with no errors | PASS |
| Running `./twisty` (no flags) prints usage and exits with code 1 | PASS |
| Running with only `-origin` or only `-dest` also exits 1 with usage | PASS |
| `-twist 1.5` exits 1 with a meaningful error message | PASS |
| All five package directories exist with valid stub `.go` files | PASS |
| All five flags are registered and parseable | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Good Practices Observed

1. **Correct stderr usage:** All user-facing errors use `fmt.Fprintln(os.Stderr, ...)` as specified, avoiding `log.Fatal`.

---

## Verification Commands Run

```bash
go build ./...                                            # Exit: 0
./twisty                                                  # Usage printed, Exit: 1
./twisty -origin "foo"                                    # Usage printed, Exit: 1
./twisty -dest "bar"                                      # Usage printed, Exit: 1
./twisty -origin "foo" -dest "bar" -twist 1.5             # Error printed, Exit: 1
./twisty -origin "foo" -dest "bar"                        # Exit: 0
```

---

## Final Verdict

**APPROVED**

All acceptance criteria pass. No blocking or advisory issues found.
