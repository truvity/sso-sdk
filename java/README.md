# SSO SDK for Java

Maven coordinates: `com.truvity:sso-sdk:0.1.0` (Java ≥ 17).

```java
var client = new SsoClient(new SsoClient.Config(
        "https://sso-api.example.com",  // administration API base URL
        "acme",                          // your registered product id
        "https://login.example.com",     // central identity issuer
        "1234567890",                    // central API project id
        keyJson));                       // machine key JSON (secret!)

var user = client.createUser(Map.of("email", "jane@example.com"));
// Email already has an identity? Onboard it instead:
// catch (SsoClient.ApiException e) {
//     if (e.status() == 409) client.onboard(Map.of("centralId", e.existingCentralId()));
// }
```

Semantics (product scoping, idempotency, verified-email onboarding, global
effects): see the repository README.
