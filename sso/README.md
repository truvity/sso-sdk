# SSO SDK for Go

Go client for the SSO administration API: manage your product's end-users
against the central identity provider — create, onboard, list, and support
operations (verification email, password reset, lock/unlock, MFA reset,
sessions).

```
go get github.com/truvity/sso-sdk/sso
```

## Usage

All configuration values are issued to your product during onboarding.
The machine key JSON stays in your secret store — never in code.

```go
client, err := sso.New(ctx, sso.Config{
	Endpoint:  "https://sso-api.example.com",   // administration API base URL
	Product:   "acme",                          // your registered product id
	Issuer:    "https://login.example.com",     // central identity issuer
	ProjectID: "1234567890",                    // central API project id
	Key:       keyJSON,                         // machine key JSON (secret!)
})
if err != nil {
	// invalid configuration
}

// Create a new end-user + send the invitation email.
user, err := client.CreateUser(ctx, &sso.CreateUserRequest{
	Email:     "jane@example.com",
	FirstName: "Jane",
	LastName:  "Doe",
})

// The email may already have an identity (created by another product):
var apiErr *sso.APIError
if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
	// onboard the existing identity into your product instead
	_, err = client.Onboard(ctx, &sso.OnboardRequest{CentralID: apiErr.ExistingCentralID()})
}
```

## Semantics worth knowing

- **Product scope.** Every call is scoped to your product; users of other
  products are invisible (404) and other scopes are forbidden (403).
- **Idempotency.** Mutations carry an `Idempotency-Key` (auto-generated per
  call). When you retry a failed call yourself, pass
  `sso.WithIdempotencyKey(k)` with the same key to make the retry safe.
- **Onboard links by VERIFIED email only.** An unverified address never
  matches (404) — invite flows verify the email first.
- **Global effects.** One person has one central identity: MFA reset,
  password reset, lock and unlock affect the user across *all* products.
  `ActionResult.GlobalEffect` reflects this.

## Authentication

The SDK mints and refreshes tokens automatically from your machine key
(JWT profile) with the API project audience. No token handling is needed
on your side.
