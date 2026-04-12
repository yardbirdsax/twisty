# Task 011: Fix URL Parsing to Stop at Viewport Segment

## Summary

`parseSharedLink` currently collects every `/`-separated path segment after `/maps/dir/`, including the map viewport segment (`@lat,lng,zoom`) and the encoded state segment (`data=!...`). These are passed to the Directions API as waypoints, causing the request to fail. The fix is to stop collecting segments when the first one starting with `@` is encountered — this is a consistent structural rule in Google Maps `/maps/dir/` URLs.

## Dependencies

None — this is a self-contained bug fix in `gpx/maps_client.go`.

## Context

A Google Maps `/maps/dir/` URL has this structure:

```
/maps/dir/<waypoint1>/<waypoint2>/.../<waypointN>/@<lat>,<lng>,<zoom>/data=<encoded-state>
```

Everything before the `@` segment is a user-specified waypoint (coordinate or address). Everything from `@` onward is internal map display state and must be ignored. The current code does not make this distinction.

**Example URL that currently fails:**

```
https://www.google.com/maps/dir/40.238122,-75.526346/40.3625144,-75.6776312/40.445452,-75.802636/Speedway,+14233+Kutztown+Rd,+Fleetwood,+PA+19522/@40.2381255,-75.5323253,668m/data=!3m1!1e3!4m11!4m10!1m0!1m0!1m0!1m5!1m1!1s0x89c5d6cd37212bc1:0xd23839ad4c4b15f0!2m2!1d-75.8399983!2d40.4856114!3e0!5m1!1e4?entry=ttu&g_ep=EgoyMDI2MDQwOC4wIKXMDSoASAFQ
```

Expected waypoints (4):
1. `40.238122,-75.526346`
2. `40.3625144,-75.6776312`
3. `40.445452,-75.802636`
4. `Speedway, 14233 Kutztown Rd, Fleetwood, PA 19522`

Current behavior: also extracts `@40.2381255,-75.5323253,668m` and `data=!3m1!...` as waypoints, making the last "waypoint" the destination sent to the API.

## Detailed Directions

Follow red-green TDD: write the failing test first, verify it fails, then fix the code.

### 1. Add a Failing Test

Add a new test case to the `TestParseSharedLinkVariations` table in `gpx/url_parsing_test.go`:

```go
{
    name:      "full URL with viewport and data segments",
    url:       "https://www.google.com/maps/dir/40.238122,-75.526346/40.3625144,-75.6776312/40.445452,-75.802636/Speedway,+14233+Kutztown+Rd,+Fleetwood,+PA+19522/@40.2381255,-75.5323253,668m/data=!3m1!1e3!4m11!4m10!1m0!1m0!1m0!1m5!1m1!1s0x89c5d6cd37212bc1:0xd23839ad4c4b15f0!2m2!1d-75.8399983!2d40.4856114!3e0!5m1!1e4?entry=ttu&g_ep=EgoyMDI2MDQwOC4wIKXMDSoASAFQ",
    wantCount: 4,
},
```

Verify it fails:

```bash
go test ./gpx/... -run TestParseSharedLinkVariations -v
```

### 2. Fix `parseSharedLink` in `gpx/maps_client.go`

In the path-extraction loop, skip segments that are empty and break when a segment starts with `@`:

```go
for _, wp := range strings.Split(parts[1], "/") {
    if wp == "" {
        continue
    }
    if strings.HasPrefix(wp, "@") {
        break
    }
    if decoded, err := url.QueryUnescape(wp); err == nil {
        waypoints = append(waypoints, decoded)
    } else {
        waypoints = append(waypoints, wp)
    }
}
```

### 3. Verify the Test Passes

```bash
go test ./gpx/... -run TestParseSharedLinkVariations -v
go test ./gpx/... -v
```

All existing tests must continue to pass.

## Acceptance Criteria

- [ ] New test case for the full URL with viewport and data segments is added to `TestParseSharedLinkVariations` in `gpx/url_parsing_test.go`
- [ ] New test fails before the fix is applied
- [ ] `parseSharedLink` stops collecting waypoints at the first segment starting with `@`
- [ ] The example URL above produces exactly 4 waypoints in the correct order
- [ ] The 4th waypoint decodes `+` signs to spaces: `Speedway, 14233 Kutztown Rd, Fleetwood, PA 19522`
- [ ] All existing tests in `./gpx/...` continue to pass

## Notes

- Only `gpx/maps_client.go` and `gpx/url_parsing_test.go` should be modified.
- The `data=` segment does not need special handling — stopping at `@` is sufficient, since `data=` always follows `@` in this URL format.
