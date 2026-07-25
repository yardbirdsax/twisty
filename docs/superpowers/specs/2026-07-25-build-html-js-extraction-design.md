# Design: Extract `buildHTML` JS/HTML into Separate Files

**Date:** 2026-07-25
**Status:** Approved

## Problem

`route_build.go` is 2,593 lines, of which roughly 1,470 are a raw Go string constant (`buildHTML`) containing HTML, CSS, and JavaScript. This makes the JS impossible to test independently, difficult to read, and hard to change without risking Go-side breakage.

## Goal

Extract the HTML and JavaScript from `route_build.go` into separate files in `static/`, embed them into the binary via `//go:embed`, and add a Node test suite for the JS — following the existing pattern established by `static/statusManager.js`.

## Non-goals

- Splitting the Go code in `route_build.go` (handlers, SSE broker, etc.) — deferred.
- Introducing a static file server or serving JS from disk at runtime.
- Changing any user-visible behavior.

## Approach

### Runtime config injection

The current `buildHTML` uses `fmt.Sprintf` to inject five values:

| Arg | Value | Where used in JS |
|---|---|---|
| `%s` | debug snippet HTML | injected into HTML body |
| `%f, %f` | center lat, lon | `L.map(...).setView([lat, lon], 13)` |
| `%g` | `quality.DefaultMaxCurvaturePerKm` | `var scorePerKmMax = ...` |
| `%s` | `statusManagerJS` (embedded) | inline `<script>` block |
| `%g` | `quality.DefaultMaxCurvature` | `var SLIDER_MAX_SCORE = ...` |

To allow `build.js` to be valid, standalone JavaScript (no format verbs), Go will inject a tiny inline config script block. `handleIndex` renders:

```html
<script>window.TWISTY_CONFIG={lat:%.6f,lon:%.6f,scorePerKmMax:%g,scoreMax:%g};</script>
```

The JS reads these from `window.TWISTY_CONFIG` at startup instead of having them baked in as format-verb literals.

### Files

**`static/build.html`** — the full HTML shell embedded via `//go:embed`. Contains four `%s` placeholders injected by `fmt.Sprintf` in `handleIndex`, in order:
1. The inline config `<script>` block (lat/lon/constants).
2. The `statusManagerJS` content (inline `<script>` block).
3. The `buildJS` content (inline `<script>` block).
4. The optional debug snippet (`buildHTMLDebugSnippet`, unchanged Go constant).

All JS is injected inline — no `<script src>` tags, no file serving.

**`static/build.js`** — all JavaScript currently in `buildHTML`. References `window.TWISTY_CONFIG` instead of format-verb literals. No format verbs; valid, loadable JS.

**`static/build.test.js`** — Node test file using `node:test` + `node:assert/strict`, loading `build.js` via `node:vm` (same pattern as `statusManager.test.js`). Stubs `window.TWISTY_CONFIG`, Leaflet globals, and DOM as needed. Tests all functions with independently verifiable logic.

### Go side changes

- Remove `const buildHTML` and `const buildHTMLDebugSnippet` from `route_build.go`.
- Add `//go:embed static/build.html` and `//go:embed static/build.js` vars alongside the existing `statusManagerJS` embed.
- `handleIndex` renders the config script string and calls `fmt.Sprintf(buildHTML, configScript, statusManagerJS, buildJS, debugSnippet)`.
- `buildHTMLDebugSnippet` moves to `static/build_debug_snippet.html` and is embedded separately, or stays as a Go constant — whichever is simpler. (Prefer keeping it as a Go constant since it's small and conditionally used.)

### Testing

- `static/build.test.js` runs via `node --test static/build.test.js`.
- `Makefile` `test` target extended to include `node --test static/build.test.js`.
- Go tests in `route_build_test.go` continue to pass unchanged (the rendered HTML output is the same).

## Acceptance criteria

1. `route_build.go` no longer contains any HTML or inline JavaScript.
2. `static/build.js` is valid JavaScript with no `%f`/`%g`/`%s` format verbs.
3. `static/build.test.js` covers all JS functions with testable logic.
4. `make test` passes (Go tests + both Node test files).
5. The served page is byte-for-byte equivalent in rendered output.
