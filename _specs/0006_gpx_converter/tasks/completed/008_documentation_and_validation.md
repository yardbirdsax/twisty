# Task 008: Documentation and Manual Validation

## Summary

Complete the feature by updating help text, creating a validation procedure for testing with real Google Maps links, and ensuring the implementation meets the PRD requirements.

## Dependencies

Task 001 through Task 007 - all implementation and testing must be complete.

## Context: Project Structure

- All commands are in `main.go` (no `cmd/root.go`, no `cmd/gpx/` package)
- Build command: `go build -o twisty .` (main package is at the project root)
- The command is `twisty gpx --maps-url <url> --out <file>` (no `convert` subcommand)

## Detailed Directions

### 1. Update Command Help Text in main.go

Ensure the `Long` help text for `newGpxCmd()` in `main.go` is comprehensive:

```go
Long: `Export a driving route from a Google Maps shared link as a GPX file
for use in offline navigation applications like OSMAnd.

AUTHENTICATION:

The first time you run this command, you will be prompted to authenticate
with Google via a browser window. Your credentials will be securely stored
for future use (in the system keychain on macOS). You will not need to
re-authenticate for 24 hours under normal use.

FLAGS:

  --maps-url (required)
    Google Maps shared link. Supported formats:
      https://maps.app.goo.gl/abc123
      https://maps.google.com/maps/dir/origin/destination

  --out (required)
    Output file path for the GPX file.

EXAMPLES:

  # Convert a simple route
  twisty gpx --maps-url "https://maps.app.goo.gl/abc123" --out my-route.gpx

  # Convert a route with multiple stops
  twisty gpx \
    --maps-url "https://maps.google.com/maps/dir/Home/Stop1/Work" \
    --out commute.gpx

LIMITATIONS:

  Only driving routes are supported.
  If multiple route options exist in Google Maps, the primary route is used.`,
```

### 2. Create Validation Procedure Document

Create `_specs/0006_gpx_converter/VALIDATION.md` with manual test cases for verifying the feature with real Google Maps links and OSMAnd.

The document should include:
- Prerequisites (build, Google account, OSMAnd)
- Test 1: Authentication flow (first run triggers browser OAuth)
- Test 2: Simple 2-point route
- Test 3: Multi-stop route (3+ waypoints)
- Test 4: Error handling (invalid URL, missing flags, bad output path)
- Test 5: Token refresh (verify re-auth is not required within 24 hours)
- Test 6: OSMAnd import verification (GPX imports and route displays correctly)

### 3. Update Root Command Help in main.go

Ensure the `Short` description of `newGpxCmd()` is clear and consistent with the other commands in `newRootCmd()`.

### 4. Final Verification Steps

Run the validation script before marking complete:

```bash
# Build check
go build -o twisty . || exit 1

# Command recognition
./twisty gpx --help || exit 1

# Unit tests
go test ./... -v -race -timeout 60s || exit 1

# Code quality
go vet ./... || exit 1

# Cleanup
rm twisty
```

### 5. In-Code Documentation

Ensure key functions have clear comments:
- `runGpx` in `main.go` — explains the orchestration flow
- `auth.Authenticator.GetToken` — explains the token retrieval strategy
- `gpx.ConvertRouteToGPX` — explains input/output and validation
- `gpx.Service.ConvertToFile` — explains the end-to-end pipeline

## Acceptance Criteria

- [ ] `README.md` has a `## twisty gpx` section with flags, URL formats, authentication note, example, and output description
- [ ] `twisty gpx` added to the Subcommands bullet list in `README.md`
- [ ] `newGpxCmd()` long help text in `main.go` is comprehensive and accurate
- [ ] Help shows correct command syntax: `twisty gpx --maps-url <url> --out <file>` (no `convert` subcommand)
- [ ] `_specs/0006_gpx_converter/VALIDATION.md` exists with manual test cases
- [ ] `go build -o twisty .` succeeds (note: build at project root, not `./cmd/twisty`)
- [ ] `./twisty gpx --help` displays complete, accurate help
- [ ] `go test ./... -v -race` passes
- [ ] `go vet ./...` passes with no issues
- [ ] Manual testing with real Google Maps links succeeds:
  - [ ] Authentication flow works (browser opens, credentials stored)
  - [ ] Simple 2-point route converts correctly
  - [ ] Multi-stop route includes all waypoints
  - [ ] Error messages are clear and actionable for invalid inputs
  - [ ] Generated GPX imports into OSMAnd without errors
  - [ ] Route navigates correctly offline

## Notes

- Add a `## twisty gpx` section to `README.md` following the same format as the existing command sections. Include: required flags table, supported URL formats, authentication note, example, and output description. Also add `twisty gpx` to the Subcommands bullet list near the top of the file.
- There is no `cmd/twisty` package — the main package is at the project root, so `go build -o twisty .` is the correct build command.
- All references to `internal/gpx`, `internal/auth`, or `cmd/gpx` in any generated code are incorrect for this project.

---

# Task 008 Review: Documentation and Manual Validation

**Reviewer:** Principal Engineer
**Date:** 2026-04-11
**Verdict:** APPROVED

---

## Summary

Task 008 completes the `twisty gpx` feature by adding comprehensive help text, README documentation, in-code comments, and a manual validation procedure.

### Files Reviewed

| File | Status |
|------|--------|
| `README.md` | Reviewed |
| `main.go` | Reviewed |
| `_specs/0006_gpx_converter/VALIDATION.md` | Reviewed |
| `auth/oauth.go` | Reviewed |
| `gpx/converter.go` | Reviewed |
| `gpx/service.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `README.md` has `## twisty gpx` section with flags, URL formats, auth note, example, output description | PASS |
| `twisty gpx` added to Subcommands bullet list in `README.md` | PASS |
| `newGpxCmd()` long help text is comprehensive and accurate | PASS |
| Help shows correct syntax: `twisty gpx --maps-url <url> --out <file>` (no `convert` subcommand) | PASS |
| `_specs/0006_gpx_converter/VALIDATION.md` exists with manual test cases | PASS |
| `go build -o twisty .` succeeds | PASS |
| `go test ./... -v -race` passes | PASS |
| `go vet ./...` passes with no issues | PASS |
| `runGpx`, `GetToken`, `ConvertRouteToGPX`, `ConvertToFile` have clear comments | PASS |
| Manual testing with real Google Maps links | NOT VERIFIED (requires live credentials) |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go build -o .tmp/twisty .          # success
go test ./... -race -timeout 60s   # all packages pass
go vet ./...                        # no issues
```

---

## Final Verdict

**APPROVED**

All verifiable acceptance criteria pass. Manual testing items require live Google credentials and a physical device and cannot be verified in this review; they are gated by the operator before production use.
