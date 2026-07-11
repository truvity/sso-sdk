# SSO SDK for Python

```
pip install truvity-sso-sdk
```

```python
from truvity_sso import Client, ApiError

client = Client(
    endpoint="https://sso-api.example.com",
    product="acme",
    issuer="https://login.example.com",
    project_id="1234567890",
    key=key_json,  # machine key JSON (secret!)
)

user = client.create_user(email="jane@example.com")
# Email already has an identity? Onboard it instead:
# except ApiError as e:
#     if e.status == 409: client.onboard(central_id=e.existing_central_id())
```

Python ≥ 3.10. Semantics (product scoping, idempotency, verified-email
onboarding, global effects): see the repository README.
