# Integration guide

What a product team does to integrate, end to end. Your integration bundle
(handed over by the platform team) contains the concrete values; this page
is the recipe.

## 1. Wire "Login with Central" into your IdP

Add an OIDC connection in your IdP (Clerk: *Custom OIDC provider*; Cognito:
*OIDC identity provider*) using the issuer, client id, and client secret
from your bundle:

- Authorization Code flow. PKCE where your IdP supports it (Clerk);
  client-secret only where it doesn't (Cognito).
- Scopes: `openid profile email`.
- **Link accounts by verified email** in your IdP's linking settings.
- Register the callback URL from your IdP into the bundle handover — the
  central side must know it.

Then disable native credential collection (passwords, MFA, verification
emails) in your IdP for delegated users — the central login owns those, and
duplicate emails from two systems is the #1 integration bug.

## 2. Wire the SDK into your backend

Install ([root README](../README.md) has the per-language table) and
configure with the four values + machine key from your bundle. Example (Go):

```go
client, err := sso.New(ctx, sso.Config{
    Endpoint:  cfg.SSOEndpoint,  // administration API base URL
    Product:   cfg.SSOProduct,   // your product id
    Issuer:    cfg.SSOIssuer,    // central login issuer
    ProjectID: cfg.SSOProjectID, // central API project id
    Key:       secretstore.Get("sso-machine-key"),
})
```

Note for GitHub Packages consumers (npm/Maven): the registry requires
authentication even for public packages — your bundle includes the two-line
registry setup.

## 3. Change your provisioning path

Wherever you create users today, call the administration API instead:

1. `create-user` with the email → store the returned `centralId` alongside
   your user record.
2. On 409, `onboard-user` with the existing id (see
   [operations.md](operations.md#create-vs-onboard)) — and store that id.
3. Keep your runtime login path unchanged — it goes through your IdP's
   OIDC connection, not through the API.

## 4. Route support tooling through the API

Password reset, MFA reset, lock/unlock, session termination — from your
backoffice, call the API (your support staff's product scope is enforced
server-side). Remember the global-effect semantics: you are operating on
the *person*, not just their account in your product.

## 5. Test in development

- Development emails are captured, not delivered — invitation codes come
  back via `inviteMode: "returnCode"` or from the platform team's capture
  tooling.
- Exercise at minimum: create → invitation → first login → linked account;
  the 409 → onboard path; one global operation (lock) and its effect on
  your login flow.
