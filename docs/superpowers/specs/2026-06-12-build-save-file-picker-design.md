# Save Route: File System Access API

**Date:** 2026-06-12
**Branch:** feat/route-build

## Goal

When the user saves a route from `twisty build`, they should be able to choose both the filename and the destination folder via the browser's native Save As dialog, rather than the file always landing in the browser's default download folder with a hardcoded name.

## Scope

Single-function change: `saveRoute()` in the embedded JavaScript within `route_build.go`. No Go code changes, no HTML/UI changes, no new endpoints.

## Approach

Use the [File System Access API](https://developer.mozilla.org/en-US/docs/Web/API/File_System_Access_API) (`window.showSaveFilePicker`) where available, with a silent fallback to the existing `<a download>` behavior for browsers that don't support it.

## Updated `saveRoute()` Logic

1. Build the JSON blob exactly as today (state object with version, center, zoom, waypoints, legs).
2. Check for `window.showSaveFilePicker`:
   - **If present:** call it with `suggestedName: 'route.twisty.json'` and `types: [{ description: 'Twisty route', accept: { 'application/json': ['.json'] } }]`.
   - **If absent:** go directly to the `<a download>` fallback (existing behavior, no toast).
3. On user cancel (`AbortError`): do nothing, no toast.
4. On write failure: `showToast('Could not save route')`.
5. The `<a download>` fallback path is reached when: the API is absent, or (optionally) on non-abort API errors. If the fallback itself fails, show the toast.

## Error Handling

| Situation | Behavior |
|---|---|
| User cancels the picker | Silent — no toast |
| `showSaveFilePicker` not supported | Silent fallback to `<a download>` |
| Write fails | `showToast('Could not save route')` |
| `<a download>` fallback fails | `showToast('Could not save route')` |

## Testing

- Existing HTML structure tests in `route_build_test.go` remain valid — no UI changes.
- New unit test: mock `showSaveFilePicker` on the JS global, confirm it is called with the correct `suggestedName` and `types` when present.
- New unit test: confirm `<a download>` path is used when `showSaveFilePicker` is absent.
- New unit test: confirm no toast is shown on `AbortError`.

## Browser Support

| Browser | Behavior |
|---|---|
| Chrome / Edge | Native Save As dialog (name + folder) |
| Safari 17+ | Native Save As dialog (name + folder) |
| Firefox / Safari <17 | Silent fallback to `<a download>` |
