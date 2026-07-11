# Operations reference

The API surface is 13 operations, all product-scoped under
`/v1/products/{product}/…` and specified in [openapi.yaml](../openapi.yaml).
This page explains the semantics the spec alone can't carry. Every SDK maps
these 1:1 (method names in your language's convention).

## Conventions

- **Authentication**: `Authorization: Bearer <token>` — the SDKs mint and
  refresh tokens from your machine key automatically.
- **Idempotency**: every mutation takes an `Idempotency-Key` header. SDKs
  generate one per call; pass your own (and reuse it) when you retry a
  failed call yourself — the retry becomes an exact replay.
- **Errors**: RFC 9457 problem JSON (`status`, `title`, `detail`,
  `errors[]`). SDKs raise a typed error carrying these fields.
- **Scoping**: users outside your product are `404`; another product's
  scope is `403`. This is enforced server-side on every operation.

## Create vs onboard

```mermaid
sequenceDiagram
    participant B as Your backend
    participant API as Administration API
    B->>API: POST /users {email}
    alt email is new
        API-->>B: 200 {centralId, created: true}
        Note over API: invitation email sent centrally
    else email already has an identity
        API-->>B: 409 + existing centralId in error details
        B->>API: POST /users/onboard {centralId}
        API-->>B: 200 {onboarded: true}
    end
```

- `create-user` creates the **identity** and sends the invitation
  (`inviteMode: "returnCode"` returns the verification code to you instead
  of emailing it — useful when you deliver invites through your own
  channel).
- `onboard-user` adds **your product's membership** to an existing
  identity — the everyday cross-product operation. By `centralId`, or by
  email: **only verified emails match** (an unverified address is 404 by
  design — never link an identity through an address nobody proved they
  own). Onboarding twice is a no-op (`onboarded: false`).

## Reading users

- `list-users` — cursor-paginated members of your product; optional email
  prefix filter.
- `get-user` — one member's detail; live directory state (a centrally
  locked user shows `status: "locked"`).
- `get-user-audit` — the user's credential-plane timeline within your
  scope: your product's events plus global-effect events from any product.
  You never see another product's local events.

## Support operations (all global-effect except verification)

| Operation | Effect | Notes |
|---|---|---|
| `send-verification-email` | resend invitation/verification | 409 if already verified |
| `reset-password` | password reset email | global |
| `reset-mfa` | removes second factors; re-enroll at next login | global; 409 if a factor needs operator support |
| `lock-user` / `unlock-user` | central deactivate / reactivate | global; issued product sessions live until expiry — cut those off product-side |

## Sessions

- `list-sessions` — the user's central sessions.
- `terminate-session` — one session; the session must belong to the user
  (404 otherwise).
- `terminate-all-sessions` — all of them. As with lock: product-side tokens
  already issued are unaffected until they expire.
