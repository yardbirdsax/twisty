# Fix: Region alias normalization for `twisty overpass start`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix `twisty overpass start --regions na/us/pennsylvania` failing with `unexpected EOF` when a valid `north-america_us_pennsylvania-latest.osm.pbf` already exists on disk.

**Root cause:** Geofabrik supports short aliases (`na/`, `eu/`, etc.) and canonical names (`north-america/`, `europe/`, etc.) for the same region. The code does no normalization, so `na/us/pennsylvania` and `north-america/us/pennsylvania` are treated as separate regions. Both end up in `allRegions` after merging with the stamp file. The new name `na/us/pennsylvania` doesn't match the existing file so the code downloads from `https://download.geofabrik.de/na/us/pennsylvania-latest.osm.pbf` — a URL that Geofabrik doesn't serve, returning a redirect or tiny HTML error page (9.4 KB on disk). Conversion then immediately fails with `reading blob header: unexpected EOF`.

**Evidence:** `~/.twisty/overpass/pbf/na_us_pennsylvania-latest.osm.pbf` is 9.4 KB. `north-america_us_pennsylvania-latest.osm.pbf` is 321.1 MB and valid.

**Fix:** Add a `normalizeRegion()` function that maps Geofabrik's short path prefixes to their canonical equivalents, and call it on every region string at parse time in `runOverpassStart()`.

**Architecture:** One new function in `overpass.go`, called at the region-parsing loop. No changes to `mergeRegions`, the stamp file, download logic, or conversion.

---

## File Map

| File | Action | Purpose |
|---|---|---|
| `overpass.go` | Modify | Add `normalizeRegion()` function; call it in `runOverpassStart()` |
| `overpass_test.go` | Modify | Add `TestNormalizeRegion` and a dedup test for `mergeRegions` |

---

### Task 1: Add `normalizeRegion()` with tests

**Files:**
- Modify: `overpass_test.go`
- Modify: `overpass.go`

- [ ] **Step 1: Write the failing test**

In `overpass_test.go`, add:

```go
func TestNormalizeRegion(t *testing.T) {
    cases := []struct {
        input string
        want  string
    }{
        {"na/us/pennsylvania", "north-america/us/pennsylvania"},
        {"na/us/maryland", "north-america/us/maryland"},
        {"eu/germany", "europe/germany"},
        {"sa/brazil", "south-america/brazil"},
        {"au/new-south-wales", "australia-oceania/new-south-wales"},
        {"as/japan", "asia/japan"},
        {"af/south-africa", "africa/south-africa"},
        {"an/antarctica", "antarctica/antarctica"},
        {"ce/mexico", "central-america/mexico"},
        // canonical names pass through unchanged
        {"north-america/us/virginia", "north-america/us/virginia"},
        {"europe/france", "europe/france"},
    }
    for _, c := range cases {
        got := normalizeRegion(c.input)
        if got != c.want {
            t.Errorf("normalizeRegion(%q) = %q; want %q", c.input, got, c.want)
        }
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test -run TestNormalizeRegion -v ./...
```

Expected: compile error — `normalizeRegion` undefined.

- [ ] **Step 3: Implement `normalizeRegion()` in `overpass.go`**

Add the function near `pbfFilename()` (around line 320):

```go
var regionAliases = map[string]string{
    "na":  "north-america",
    "sa":  "south-america",
    "au":  "australia-oceania",
    "as":  "asia",
    "af":  "africa",
    "eu":  "europe",
    "an":  "antarctica",
    "ce":  "central-america",
}

func normalizeRegion(region string) string {
    prefix, rest, ok := strings.Cut(region, "/")
    if !ok {
        return region
    }
    if canonical, found := regionAliases[prefix]; found {
        return canonical + "/" + rest
    }
    return region
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test -run TestNormalizeRegion -v ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add overpass.go overpass_test.go
git commit -m "fix(overpass): add normalizeRegion to resolve Geofabrik path aliases"
```

---

### Task 2: Apply normalization in `runOverpassStart()` and verify dedup

**Files:**
- Modify: `overpass.go` — call `normalizeRegion()` in the region-parsing loop
- Modify: `overpass_test.go` — add dedup test for `mergeRegions` after normalization

- [ ] **Step 1: Write the failing test**

Add a test to `overpass_test.go` that pins the dedup behavior after normalization:

```go
func TestMergeRegionsWithNormalization(t *testing.T) {
    // Simulates: stamp file has "north-america/us/pennsylvania",
    // user passes "--regions na/us/pennsylvania".
    // After normalization both are canonical — dedup works.
    existing := []string{"north-america/us/pennsylvania"}
    incoming := []string{"north-america/us/pennsylvania"} // normalized form
    got := mergeRegions(existing, incoming)
    if len(got) != 1 {
        t.Fatalf("expected 1 region after dedup, got %d: %v", len(got), got)
    }
}
```

- [ ] **Step 2: Run the full test suite to confirm baseline**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 3: Apply normalization in `runOverpassStart()`**

In `overpass.go`, inside the region-parsing loop (around line 154), add the normalization call:

```go
// Before:
r = strings.TrimSpace(r)
if r != "" {
    newRegions = append(newRegions, r)
}

// After:
r = strings.TrimSpace(r)
if r != "" {
    newRegions = append(newRegions, normalizeRegion(r))
}
```

- [ ] **Step 4: Run all tests**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add overpass.go overpass_test.go
git commit -m "fix(overpass): normalize region aliases before merging with stamp file"
```

---

### Task 3: Clean up the stale junk file and smoke test

- [ ] **Step 1: Delete the stale 9.4 KB file**

```bash
rm ~/.twisty/overpass/pbf/na_us_pennsylvania-latest.osm.pbf
```

- [ ] **Step 2: Smoke test**

```bash
twisty overpass start --regions na/us/pennsylvania
```

Expected stderr output:
```
PBF already exists: /Users/joshuafeierman/.twisty/overpass/pbf/north-america_us_pennsylvania-latest.osm.pbf
PBF already exists: ...maryland...
...
```

No download, no conversion error. The command proceeds to merging/starting the container using the existing valid PBF files.
