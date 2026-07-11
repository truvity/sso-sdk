# SSO SDK for TypeScript (Node.js)

```
npm install @truvity/sso-sdk
```

```ts
import { Client, ApiError } from "@truvity/sso-sdk";

const client = new Client({
  endpoint: "https://sso-api.example.com",
  product: "acme",
  issuer: "https://login.example.com",
  projectId: "1234567890",
  key: keyJson, // machine key JSON (secret!)
});

const user = await client.createUser({ email: "jane@example.com" });
// Email already has an identity? Onboard it instead:
// catch (e) { if (e instanceof ApiError && e.status === 409) await client.onboard({ centralId: e.existingCentralId() }); }
```

Node.js ≥ 20. Semantics (product scoping, idempotency, verified-email
onboarding, global effects): see the repository README.
