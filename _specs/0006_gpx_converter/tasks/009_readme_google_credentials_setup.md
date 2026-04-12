# Task 009: Document Google Cloud Credential Provisioning in README

## Summary

Add a "Setup" subsection to the `## twisty gpx` section in `README.md` that explains how to create a Google Cloud project, obtain OAuth 2.0 credentials, and store them securely in the macOS Keychain for use at build time. The existing "Authentication" subsection describes the end-user login flow; this new section targets developers who are building the binary from source and need credentials baked in.

## Dependencies

None — this is a documentation-only change.

## Context: Existing README Structure

The `## twisty gpx` section currently contains:
- A usage synopsis
- Required flags table
- Supported URL formats table
- An "Authentication" subsection describing the browser login flow
- An example
- An "Output" subsection

The new "Setup" subsection belongs **before** "Authentication", since credentials must be provisioned before the binary can be built at all.

The Google API used by the Maps client (`gpx/maps_client.go`) is the **Directions API** (`https://maps.googleapis.com/maps/api/directions/json`). The OAuth scope (`auth/oauth.go`) is `https://www.googleapis.com/auth/maps-platform.routesPreferredApi`.

## Detailed Directions

### 1. Add a "Setup" Subsection to `README.md`

Insert the following subsection between the Supported URL formats table and the Authentication subsection (approximately after line 362 in the current file). All steps use the `gcloud` CLI; install it from [cloud.google.com/sdk](https://cloud.google.com/sdk) if you have not already.

````markdown
### Setup

`twisty gpx` requires a Google Cloud project with the Directions API enabled and an OAuth 2.0 client ID baked into the binary at build time. All steps below use the `gcloud` CLI.

#### 1. Create and configure a Google Cloud project

```bash
PROJECT_ID=twisty-maps   # must be globally unique; adjust if taken
gcloud projects create $PROJECT_ID --name="Twisty Maps"
gcloud config set project $PROJECT_ID
```

If you have a billing account, link it now (the Directions API requires billing to be enabled):

```bash
BILLING_ACCOUNT=$(gcloud billing accounts list --format='value(name)' --filter='open=true' | head -1)
gcloud billing projects link $PROJECT_ID --billing-account=$BILLING_ACCOUNT
```

#### 2. Enable the Directions API

```bash
gcloud services enable directions-backend.googleapis.com
```

#### 3. Configure the OAuth consent screen

```bash
gcloud alpha iap oauth-brands create \
  --application_title="Twisty" \
  --support_email="$(gcloud config get-value account)"
```

Note the brand resource name printed in the output (format: `projects/PROJECT_NUMBER/brands/PROJECT_NUMBER`).

#### 4. Create a Desktop app OAuth 2.0 client

```bash
BRAND=$(gcloud alpha iap oauth-brands list --format='value(name)' | head -1)
gcloud alpha iap oauth-clients create $BRAND --display_name="twisty-desktop"
```

The output includes `name` (the resource name) and `secret`. Extract the client ID and secret:

```bash
CLIENT_ID=$(gcloud alpha iap oauth-clients list $BRAND --format='value(name)' | head -1 | awk -F'/' '{print $NF}')
CLIENT_SECRET=$(gcloud alpha iap oauth-clients list $BRAND --format='value(secret)' | head -1)
```

#### 5. Store credentials in the macOS Keychain

```bash
security add-generic-password \
  -s twisty/build/google/client-id \
  -a twisty \
  -w "$CLIENT_ID"

security add-generic-password \
  -s twisty/build/google/client-secret \
  -a twisty \
  -w "$CLIENT_SECRET"
```

These values are read automatically by `make build` and injected into the binary at link time. They are never written to disk outside the Keychain.

#### 6. Build

```bash
make build
```
````

### 2. Verify Rendering

Read the modified README and confirm:
- The new subsection appears between "Supported URL formats" and "Authentication"
- All shell commands are in fenced `bash` code blocks
- The `make build` step is clear and self-contained

## Acceptance Criteria

- [ ] A `### Setup` subsection is added to `## twisty gpx` in `README.md`
- [ ] The subsection appears before `### Authentication`
- [ ] All provisioning steps use `gcloud` CLI commands, not console UI navigation
- [ ] Instructions cover: project creation, billing link, enabling the Directions API, OAuth consent screen, OAuth client creation, and Keychain storage
- [ ] The two `security add-generic-password` commands use service names `twisty/build/google/client-id` and `twisty/build/google/client-secret` and account name `twisty`
- [ ] `CLIENT_ID` and `CLIENT_SECRET` are extracted from `gcloud` output rather than copied manually
- [ ] The `make build` step is the final step
- [ ] No other sections in the README are modified

## Notes

- The keychain service names (`twisty/build/google/client-id`, `twisty/build/google/client-secret`) must match exactly what the Makefile reads in Task 010. Do not change them.
- The audience for this section is a developer building from source, not an end user running a pre-built binary.
- Do not add setup instructions to any other section of the README (e.g., do not add a top-level "Prerequisites" section).
- `gcloud alpha iap oauth-clients` creates OAuth 2.0 clients via the IAP API. Verify during implementation that the client type produced is compatible with the PKCE desktop flow used in `auth/oauth.go`. If it is not, the task should be updated to use whatever `gcloud` command produces a Desktop app OAuth client — or, if no stable CLI equivalent exists, fall back to documenting the console steps for that one sub-step only.
