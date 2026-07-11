# SSO SDKs

Client SDKs for the SSO administration API: manage your product's end-users
against the central identity provider.

| Language | Directory | Install |
|---|---|---|
| Go | [sso/](sso/) | `go get github.com/truvity/sso-sdk/sso` |
| TypeScript | _planned_ | |
| Python | _planned_ | |
| Java | _planned_ | |

All configuration values (API endpoint, product id, issuer, project id,
machine key) are issued to your product during onboarding. Keep the machine
key in your secret store.

See each SDK's README for usage; the semantics (product scoping,
idempotency, verified-email onboarding, global-effect operations) are shared
across languages.
