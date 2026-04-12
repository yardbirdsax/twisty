# Task 015: Update Makefile and README for API Key Provisioning

## Summary

Update the `Makefile` build target and the `## twisty gpx` README section to reflect the switch from OAuth credentials to a single Google API key. The two-secret keychain pattern (client ID + secret) is replaced by a single `twisty/build/google/api-key` keychain entry. README setup instructions are rewritten to cover creating a restricted API key via `gcloud` instead of an OAuth client.

## Dependencies

Task 014 (API key wiring in code) must be complete first, since this task updates the ldflags to inject `builtInGoogleAPIKey`.

## Detailed Directions

### 1. Update `Makefile`

Replace the existing `# --- Build ---` section entirely:

```makefile
# --- Build ---

# Keychain key used to retrieve the Google Routes API key at build time.
# Store it once with:
#   security add-generic-password -s twisty/build/google/api-key -a twisty -w '<value>'
GOOGLE_API_KEY ?= $(shell security find-generic-password -s twisty/build/google/api-key -a twisty -w 2>/dev/null)

GOOGLE_LDFLAGS = -X main.builtInGoogleAPIKey=$(GOOGLE_API_KEY)

.PHONY: build
build: ## Build the twisty binary with the Google API key injected from the macOS keychain.
	@if [ -z "$(GOOGLE_API_KEY)" ]; then \
		echo "ERROR: twisty/build/google/api-key not found in keychain."; \
		echo "  security add-generic-password -s twisty/build/google/api-key -a twisty -w '<value>'"; \
		exit 1; \
	fi
	@mkdir -p bin
	go build -ldflags "$(GOOGLE_LDFLAGS)" -o bin/twisty .
```

### 2. Update the `### Setup` Subsection in `README.md`

Replace the existing `### Setup` subsection under `## twisty gpx` with the following. All steps use `gcloud`.

````markdown
### Setup

`twisty gpx` requires a Google Cloud project with the Routes API enabled and an API key baked into the binary at build time. All steps below use the `gcloud` CLI.

#### 1. Create and configure a Google Cloud project

```bash
PROJECT_ID=twisty-maps   # must be globally unique; adjust if taken
gcloud projects create $PROJECT_ID --name="Twisty Maps"
gcloud config set project $PROJECT_ID
```

Link a billing account (required for the Routes API):

```bash
BILLING_ACCOUNT=$(gcloud billing accounts list --format='value(name)' --filter='open=true' | head -1)
gcloud billing projects link $PROJECT_ID --billing-account=$BILLING_ACCOUNT
```

#### 2. Enable the Routes API

```bash
gcloud services enable routes.googleapis.com
```

#### 3. Create a restricted API key

```bash
API_KEY=$(gcloud alpha services api-keys create \
  --display-name="twisty-routes" \
  --api-target=service=routes.googleapis.com \
  --format='value(response.keyString)' 2>/dev/null)
echo "API key: $API_KEY"
```

This key is restricted to the Routes API only. Even if extracted from the binary, it cannot be used for any other Google service.

Set a daily quota cap to limit exposure from unauthorized use:

```bash
# Optional but recommended — adjust the limit to match your expected usage
gcloud alpha services api-keys update \
  $(gcloud alpha services api-keys list --filter='displayName=twisty-routes' --format='value(name)') \
  --api-target=service=routes.googleapis.com,methods=google.maps.routing.v2.Routes.ComputeRoutes \
  --quota-project=$PROJECT_ID
```

Also set a budget alert in the [Google Cloud Console Billing](https://console.cloud.google.com/billing) section so you are notified if usage spikes.

#### 4. Store the key in the macOS Keychain

```bash
security add-generic-password \
  -s twisty/build/google/api-key \
  -a twisty \
  -w "$API_KEY"
```

The key is read automatically by `make build` and injected into the binary at link time. It is never written to disk outside the Keychain.

#### 5. Build

```bash
make build
```
````

### 3. Update the `### Authentication` Subsection in `README.md`

The current `### Authentication` subsection describes the browser login flow, which no longer applies. Replace it with a brief note:

```markdown
### Authentication

`twisty gpx` uses a Google API key baked into the binary at build time. No browser login is required. See [Setup](#setup) for how to provision and store the key.
```

### 4. Verify

```bash
# With key present in keychain:
make build
./bin/twisty gpx --help   # should work

# Simulate missing key:
GOOGLE_API_KEY= make build
# Expected: error message with the security add-generic-password command, exit 1
```

## Acceptance Criteria

- [ ] Makefile `# --- Build ---` section uses `GOOGLE_API_KEY` / `twisty/build/google/api-key` instead of the two-secret OAuth pattern
- [ ] `GOOGLE_LDFLAGS` injects `-X main.builtInGoogleAPIKey`
- [ ] `make build` fails with a clear error and the exact `security add-generic-password` command when `GOOGLE_API_KEY` is empty
- [ ] README `### Setup` subsection uses `gcloud` to enable `routes.googleapis.com` (not `directions-backend.googleapis.com`) and create a key-restricted API key
- [ ] README `### Authentication` subsection no longer describes a browser login flow
- [ ] `make build` succeeds and produces `bin/twisty` when the key is present in the Keychain
- [ ] No references to `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, or OAuth credentials remain in the Makefile or README

## Notes

- The `gcloud alpha services api-keys create` command syntax should be verified during implementation — the `alpha` component is required as of early 2026 and the flags may differ slightly across gcloud versions.
- The Makefile `?=` assignment allows `GOOGLE_API_KEY=abc123 make build` to work in CI without touching the Keychain.
- The README warning about key extractability (it's baked into the binary) is intentionally omitted from user-facing docs — it's an accepted trade-off for a locally-distributed CLI, and the key restriction + quota cap mitigate the risk.
