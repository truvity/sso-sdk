# SSO SDKs

Client SDKs for the SSO administration API: manage your product's end-users
against the central identity provider.

| Language | Directory | Install |
|---|---|---|
| Go | [sso/](sso/) | `go get github.com/truvity/sso-sdk/sso` |
| TypeScript (Node.js) | [typescript/](typescript/) | `npm install @truvity/sso-sdk` |
| Python | [python/](python/) | `pip install truvity-sso-sdk` |
| Java | [java/](java/) | `com.truvity:sso-sdk` |

All configuration values (API endpoint, product id, issuer, project id,
machine key) are issued to your product during onboarding. Keep the machine
key in your secret store.

Documentation:

- [docs/architecture.md](docs/architecture.md) — how your product, your IdP, the
  central login, the administration API and the mail service fit together
- [docs/integration.md](docs/integration.md) — the end-to-end integration recipe
- [docs/operations.md](docs/operations.md) — semantics of all 14 operations
  (create-vs-onboard, global effects, scoping, idempotency)
- [openapi.yaml](openapi.yaml) — the API contract every SDK mirrors

Each SDK's README carries the language-specific quickstart.
